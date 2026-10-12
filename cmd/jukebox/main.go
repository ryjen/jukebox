package main

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ryjen/jukebox/internal/musicassistant"
)

//go:embed web/*
var webAssets embed.FS

type room struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	PlayerID string `json:"-"`
}
type roomConfig struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	PlayerID string `json:"playerId"`
}
type config struct {
	Rooms []roomConfig `json:"rooms"`
}
type invite struct {
	RoomID  string
	Expires time.Time
}
type session struct {
	ID      string
	RoomID  string
	Expires time.Time
	Count   int
}
type request struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Artist      string    `json:"artist"`
	Source      string    `json:"source"`
	RequestedAt time.Time `json:"requestedAt"`
}
type server struct {
	mu         sync.Mutex
	rooms      map[string]room
	invites    map[string]invite
	sessions   map[string]*session
	requests   map[string][]request
	adminToken string
	ma         *musicassistant.Client
	now        func() time.Time
}

func newServer(cfg config, adminToken string, now func() time.Time) (*server, error) {
	if len(adminToken) < 24 {
		return nil, errors.New("JUKEBOX_ADMIN_TOKEN must contain at least 24 characters")
	}
	if len(cfg.Rooms) == 0 {
		return nil, errors.New("at least one room is required")
	}
	s := &server{rooms: map[string]room{}, invites: map[string]invite{}, sessions: map[string]*session{}, requests: map[string][]request{}, adminToken: adminToken, now: now}
	players := map[string]bool{}
	for _, r := range cfg.Rooms {
		if !validID(r.ID) || strings.TrimSpace(r.Name) == "" || strings.TrimSpace(r.PlayerID) == "" {
			return nil, errors.New("room needs valid id, name and playerId")
		}
		if _, ok := s.rooms[r.ID]; ok {
			return nil, errors.New("duplicate room id")
		}
		if players[r.PlayerID] {
			return nil, errors.New("player assigned to multiple independent rooms")
		}
		players[r.PlayerID] = true
		s.rooms[r.ID] = room{r.ID, r.Name, r.PlayerID}
	}
	return s, nil
}
func validID(v string) bool {
	if len(v) < 1 || len(v) > 48 {
		return false
	}
	for _, c := range v {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return true
}
func secret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func send(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}
func fail(w http.ResponseWriter, code int, message string) {
	send(w, code, map[string]string{"error": message})
}
func decode(r *http.Request, v any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("application/json required")
	}
	payload, err := io.ReadAll(io.LimitReader(r.Body, 4097))
	if err != nil || len(payload) > 4096 {
		return errors.New("request body too large")
	}
	d := json.NewDecoder(bytes.NewReader(payload))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("unexpected trailing data")
	}
	return nil
}
func (s *server) host(r *http.Request) bool {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return false
	}
	candidate := strings.TrimPrefix(header, "Bearer ")
	return len(candidate) == len(s.adminToken) && subtle.ConstantTimeCompare([]byte(candidate), []byte(s.adminToken)) == 1
}
func (s *server) auth(r *http.Request, roomID string) bool {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return false
	}
	token := strings.TrimPrefix(header, "Bearer ")
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[token]
	return ok && s.now().Before(sess.Expires) && sess.RoomID == roomID
}
func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		send(w, 200, map[string]string{"status": "ok", "playback": "not-connected"})
	})
	mux.HandleFunc("GET /api/rooms", func(w http.ResponseWriter, r *http.Request) {
		list := make([]room, 0, len(s.rooms))
		for _, v := range s.rooms {
			list = append(list, v)
		}
		// Stable ordering for clients.
		for i := 0; i < len(list); i++ {
			for j := i + 1; j < len(list); j++ {
				if list[i].ID > list[j].ID {
					list[i], list[j] = list[j], list[i]
				}
			}
		}
		send(w, 200, map[string]any{"rooms": list, "playback": "not-connected"})
	})
	mux.HandleFunc("POST /api/admin/invites", func(w http.ResponseWriter, r *http.Request) {
		if !s.host(r) {
			fail(w, 401, "unauthorized")
			return
		}
		var body struct {
			RoomID string `json:"roomId"`
		}
		if err := decode(r, &body); err != nil {
			fail(w, 400, "invalid request")
			return
		}
		if _, ok := s.rooms[body.RoomID]; !ok {
			fail(w, 404, "room not found")
			return
		}
		code, err := secret()
		if err != nil {
			fail(w, 503, "randomness unavailable")
			return
		}
		expiry := s.now().Add(10 * time.Minute)
		s.mu.Lock()
		s.invites[code] = invite{body.RoomID, expiry}
		s.mu.Unlock()
		send(w, 201, map[string]any{"inviteCode": code, "roomId": body.RoomID, "expiresAt": expiry})
	})
	mux.HandleFunc("POST /api/sessions", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			InviteCode string `json:"inviteCode"`
		}
		if err := decode(r, &body); err != nil {
			fail(w, 400, "invalid request")
			return
		}
		s.mu.Lock()
		inv, ok := s.invites[body.InviteCode]
		if !ok || !s.now().Before(inv.Expires) {
			s.mu.Unlock()
			fail(w, 401, "invalid or expired invitation")
			return
		}
		token, err := secret()
		if err != nil {
			s.mu.Unlock()
			fail(w, 503, "randomness unavailable")
			return
		}
		id, err := secret() // Non-secret host-visible revocation handle.
		if err != nil {
			s.mu.Unlock()
			fail(w, 503, "randomness unavailable")
			return
		}
		delete(s.invites, body.InviteCode) // one-time consumption
		expiry := s.now().Add(8 * time.Hour)
		s.sessions[token] = &session{ID: id, RoomID: inv.RoomID, Expires: expiry}
		s.mu.Unlock()
		send(w, 201, map[string]any{"token": token, "roomId": inv.RoomID, "expiresAt": expiry})
	})

	mux.HandleFunc("GET /api/admin/sessions", func(w http.ResponseWriter, r *http.Request) {
		if !s.host(r) {
			fail(w, 401, "unauthorized")
			return
		}
		type summary struct {
			ID       string    `json:"id"`
			RoomID   string    `json:"roomId"`
			Expires  time.Time `json:"expiresAt"`
			Requests int       `json:"requests"`
		}
		items := []summary{}
		s.mu.Lock()
		for token, sess := range s.sessions {
			if !s.now().Before(sess.Expires) {
				delete(s.sessions, token)
				continue
			}
			items = append(items, summary{sess.ID, sess.RoomID, sess.Expires, sess.Count})
		}
		s.mu.Unlock()
		// The host can revoke by the opaque ID; guest bearer tokens are never enumerated.
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
		send(w, 200, map[string]any{"sessions": items})
	})
	mux.HandleFunc("POST /api/admin/revoke", func(w http.ResponseWriter, r *http.Request) {
		if !s.host(r) {
			fail(w, 401, "unauthorized")
			return
		}
		var body struct {
			SessionID string `json:"sessionId"`
		}
		if err := decode(r, &body); err != nil || len(body.SessionID) != 64 {
			fail(w, 400, "invalid request")
			return
		}
		s.mu.Lock()
		for token, sess := range s.sessions {
			if sess.ID == body.SessionID {
				delete(s.sessions, token)
				break
			}
		}
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	// The Music Assistant token stays server-side. Room scopes select a trusted,
	// configured player; a group queue is intentionally denied until group
	// membership and authorization are implemented.
	mux.HandleFunc("GET /api/rooms/{room}/queue", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("room")
		if !s.auth(r, id) {
			fail(w, 403, "room access denied")
			return
		}
		if s.ma == nil {
			fail(w, 503, "music assistant not configured")
			return
		}
		player := s.rooms[id]
		snapshot, err := s.ma.ReadQueue(r.Context(), player.PlayerID)
		if err != nil {
			if errors.Is(err, musicassistant.ErrGrouped) {
				fail(w, 409, "group queue not authorized")
			} else {
				fail(w, 502, "music assistant unavailable")
			}
			return
		}
		// A session revoked during a slow upstream response must not disclose it.
		if !s.auth(r, id) {
			fail(w, 403, "room access denied")
			return
		}
		send(w, 200, snapshot)
	})
	mux.HandleFunc("GET /api/rooms/{room}/requests", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("room")
		if !s.auth(r, id) {
			fail(w, 403, "room access denied")
			return
		}
		s.mu.Lock()
		items := append([]request{}, s.requests[id]...)
		s.mu.Unlock()
		send(w, 200, map[string]any{"requests": items, "playback": "not-connected"})
	})
	mux.HandleFunc("POST /api/rooms/{room}/requests", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("room")
		if !s.auth(r, id) {
			fail(w, 403, "room access denied")
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		var body struct {
			Title  string `json:"title"`
			Artist string `json:"artist"`
			Source string `json:"source"`
		}
		if err := decode(r, &body); err != nil {
			fail(w, 400, "invalid request")
			return
		}
		body.Title = strings.TrimSpace(body.Title)
		body.Artist = strings.TrimSpace(body.Artist)
		if body.Title == "" || utf8.RuneCountInString(body.Title) > 120 || utf8.RuneCountInString(body.Artist) > 120 || (body.Source != "local" && body.Source != "cloud") {
			fail(w, 400, "invalid track metadata")
			return
		}
		s.mu.Lock()
		sess, ok := s.sessions[token]
		if !ok || !s.now().Before(sess.Expires) || sess.RoomID != id {
			s.mu.Unlock()
			fail(w, 403, "room access denied")
			return
		}
		if sess.Count >= 5 || len(s.requests[id]) >= 50 {
			s.mu.Unlock()
			fail(w, 429, "request limit reached")
			return
		}
		key, err := secret()
		if err != nil {
			s.mu.Unlock()
			fail(w, 503, "randomness unavailable")
			return
		}
		item := request{key, body.Title, body.Artist, body.Source, s.now()}
		s.requests[id] = append(s.requests[id], item)
		sess.Count++
		s.mu.Unlock()
		send(w, 201, map[string]any{"request": item, "playback": "not-connected"})
	})
	assets, _ := fs.Sub(webAssets, "web")
	mux.Handle("/", http.FileServer(http.FS(assets)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}
func main() {
	path := os.Getenv("JUKEBOX_CONFIG")
	if path == "" {
		path = "config.example.json"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	var cfg config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		log.Fatal(err)
	}
	s, err := newServer(cfg, os.Getenv("JUKEBOX_ADMIN_TOKEN"), time.Now)
	if err != nil {
		log.Fatal(err)
	}
	// Read-only Music Assistant connection is explicitly opt-in. Reject
	// incomplete configuration instead of running with ambiguous authority.
	maURL, maToken := os.Getenv("JUKEBOX_MA_URL"), os.Getenv("JUKEBOX_MA_TOKEN")
	if maURL != "" || maToken != "" {
		s.ma, err = musicassistant.New(maURL, maToken)
		if err != nil {
			log.Fatal(err)
		}
	}
	addr := os.Getenv("JUKEBOX_LISTEN_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8097"
	}
	if !strings.HasPrefix(addr, "127.0.0.1:") && !strings.HasPrefix(addr, "localhost:") && os.Getenv("JUKEBOX_ALLOW_REMOTE") != "1" {
		log.Fatal("non-loopback bind requires JUKEBOX_ALLOW_REMOTE=1 and an authenticated TLS ingress")
	}
	log.Printf("experimental request board on %s (NO MUSIC PLAYBACK)", addr)
	srv := &http.Server{Addr: addr, Handler: s.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
