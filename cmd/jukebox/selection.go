package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ryjen/jukebox/internal/musicassistant"
)

// Catalog choices are encrypted, authenticated, short-lived claims bound to
// a room and session. They attest to an upstream search, not playability.
type selectionClaim struct {
	Room     string `json:"room"`
	Owner    string `json:"owner"`
	URI      string `json:"uri"`
	Provider string `json:"provider"`
	Name     string `json:"name"`
	Artist   string `json:"artist"`
	Expires  int64  `json:"exp"`
}

type catalogChoice struct {
	SelectionID string `json:"selectionId"`
	Name        string `json:"name"`
	Artist      string `json:"artist"`
	Provider    string `json:"provider"`
}

var errInvalidSelection = errors.New("invalid or expired catalogue selection")

func sessionFingerprint(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func validCatalogClaim(c selectionClaim) bool {
	return validID(c.Room) &&
		len(c.URI) > 0 && len(c.URI) <= 2048 && !strings.ContainsRune(c.URI, 0) &&
		len(c.Provider) > 0 && len(c.Provider) <= 128 &&
		c.Name != "" && utf8.ValidString(c.Name) && utf8.RuneCountInString(c.Name) <= 120 &&
		utf8.ValidString(c.Artist) && utf8.RuneCountInString(c.Artist) <= 120
}

func (s *server) selectionAEAD() (cipher.AEAD, error) {
	block, err := aes.NewCipher(s.selectionKey)
	if err != nil {
		return nil, errInvalidSelection
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errInvalidSelection
	}
	return aead, nil
}

func (s *server) signCatalogChoice(roomID, guestToken string, item musicassistant.Track) (catalogChoice, error) {
	c := selectionClaim{
		Room: roomID, Owner: sessionFingerprint(guestToken),
		URI: item.URI, Provider: item.Provider, Name: item.Name, Artist: item.Artist,
		Expires: s.now().Add(10 * time.Minute).Unix(),
	}
	if !validCatalogClaim(c) {
		return catalogChoice{}, errInvalidSelection
	}
	payload, err := json.Marshal(c)
	if err != nil {
		return catalogChoice{}, errInvalidSelection
	}
	aead, err := s.selectionAEAD()
	if err != nil {
		return catalogChoice{}, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return catalogChoice{}, errInvalidSelection
	}
	ciphertext := aead.Seal(nil, nonce, payload, nil)
	token := base64.RawURLEncoding.EncodeToString(append(nonce, ciphertext...))
	if len(token) > 4096 {
		return catalogChoice{}, errInvalidSelection
	}
	return catalogChoice{SelectionID: token, Name: c.Name, Artist: c.Artist, Provider: c.Provider}, nil
}

func (s *server) verifyCatalogChoice(id, roomID, guestToken string) (selectionClaim, error) {
	var empty selectionClaim
	if len(id) == 0 || len(id) > 4096 {
		return empty, errInvalidSelection
	}
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return empty, errInvalidSelection
	}
	aead, err := s.selectionAEAD()
	if err != nil {
		return empty, err
	}
	if len(raw) < aead.NonceSize()+aead.Overhead() {
		return empty, errInvalidSelection
	}
	nonce, ciphertext := raw[:aead.NonceSize()], raw[aead.NonceSize():]
	payload, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil || len(payload) > 3072 {
		return empty, errInvalidSelection
	}
	var claim selectionClaim
	if err := json.Unmarshal(payload, &claim); err != nil || !validCatalogClaim(claim) {
		return empty, errInvalidSelection
	}
	expected := sessionFingerprint(guestToken)
	if claim.Room != roomID || subtle.ConstantTimeCompare([]byte(claim.Owner), []byte(expected)) != 1 ||
		s.now().Unix() >= claim.Expires {
		return empty, errInvalidSelection
	}
	return claim, nil
}
