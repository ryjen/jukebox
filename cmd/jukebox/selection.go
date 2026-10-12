package main

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    "errors"
    "strings"
    "time"
    "unicode/utf8"

    "github.com/ryjen/jukebox/internal/musicassistant"
)

// Catalog choices are signed, short-lived claims bound to one guest session
// and room. They attest to an upstream search result, never to playability.
type selectionClaim struct {
    Room string `json:"room"`
    Owner string `json:"owner"`
    URI string `json:"uri"`
    Provider string `json:"provider"`
    Name string `json:"name"`
    Artist string `json:"artist"`
    Expires int64 `json:"exp"`
}

type catalogChoice struct {
    SelectionID string `json:"selectionId"`
    Name string `json:"name"`
    Artist string `json:"artist"`
    Provider string `json:"provider"`
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

func (s *server) signCatalogChoice(roomID, guestToken string, item musicassistant.Track) (catalogChoice, error) {
    c := selectionClaim{
        Room: roomID, Owner: sessionFingerprint(guestToken),
        URI: item.URI, Provider: item.Provider, Name: item.Name, Artist: item.Artist,
        Expires: s.now().Add(10*time.Minute).Unix(),
    }
    if !validCatalogClaim(c) { return catalogChoice{}, errInvalidSelection }
    payload, err := json.Marshal(c)
    if err != nil { return catalogChoice{}, errInvalidSelection }
    encoded := base64.RawURLEncoding.EncodeToString(payload)
    mac := hmac.New(sha256.New, s.selectionKey)
    _, _ = mac.Write([]byte(encoded))
    id := encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
    if len(id) > 4096 { return catalogChoice{}, errInvalidSelection }
    return catalogChoice{SelectionID:id, Name:c.Name, Artist:c.Artist, Provider:c.Provider}, nil
}

func (s *server) verifyCatalogChoice(id, roomID, guestToken string) (selectionClaim, error) {
    var empty selectionClaim
    if len(id) == 0 || len(id) > 4096 { return empty, errInvalidSelection }
    parts := strings.Split(id, ".")
    if len(parts) != 2 { return empty, errInvalidSelection }
    supplied, err := base64.RawURLEncoding.DecodeString(parts[1])
    if err != nil || len(supplied) != sha256.Size { return empty, errInvalidSelection }
    mac := hmac.New(sha256.New, s.selectionKey)
    _, _ = mac.Write([]byte(parts[0]))
    if !hmac.Equal(supplied, mac.Sum(nil)) { return empty, errInvalidSelection }
    payload, err := base64.RawURLEncoding.DecodeString(parts[0])
    if err != nil || len(payload) > 3072 { return empty, errInvalidSelection }
    var claim selectionClaim
    if err := json.Unmarshal(payload, &claim); err != nil || !validCatalogClaim(claim) {
        return empty, errInvalidSelection
    }
    expected := sessionFingerprint(guestToken)
    if claim.Room != roomID || !hmac.Equal([]byte(claim.Owner), []byte(expected)) ||
        s.now().Unix() >= claim.Expires {
        return empty, errInvalidSelection
    }
    return claim, nil
}
