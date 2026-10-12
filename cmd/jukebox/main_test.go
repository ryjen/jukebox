package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T) (*server, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	cfg := config{Rooms: []roomConfig{{"living", "Living", "p1"}, {"office", "Office", "p2"}}}
	s, err := newServer(cfg, strings.Repeat("a", 32), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return s, &now
}
func call(s *server, method, path string, body any, token string) *httptest.ResponseRecorder {
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	return rec
}
func getJSON(t *testing.T, r *httptest.ResponseRecorder, key string) string {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &obj); err != nil {
		t.Fatal(err)
	}
	v, _ := obj[key].(string)
	return v
}
func join(t *testing.T, s *server, room string) string {
	t.Helper()
	inv := call(s, "POST", "/api/admin/invites", map[string]string{"roomId": room}, strings.Repeat("a", 32))
	if inv.Code != 201 {
		t.Fatalf("invite %d %s", inv.Code, inv.Body)
	}
	session := call(s, "POST", "/api/sessions", map[string]string{"inviteCode": getJSON(t, inv, "inviteCode")}, "")
	if session.Code != 201 {
		t.Fatalf("session %d", session.Code)
	}
	return getJSON(t, session, "token")
}
func TestRoomIsolationAndHostAuthority(t *testing.T) {
	s, _ := fixture(t)
	living := join(t, s, "living")
	office := join(t, s, "office")
	if got := call(s, "POST", "/api/admin/invites", map[string]string{"roomId": "living"}, living).Code; got != 401 {
		t.Fatalf("guest created invite: %d", got)
	}
	item := map[string]string{"title": "A", "artist": "B", "source": "local"}
	if got := call(s, "POST", "/api/rooms/living/requests", item, living).Code; got != 201 {
		t.Fatalf("post: %d", got)
	}
	for _, p := range []string{"/api/rooms/office/requests", "/api/rooms/living/requests"} {
		tok := living
		if strings.Contains(p, "living") {
			tok = office
		}
		if got := call(s, "GET", p, nil, tok).Code; got != 403 {
			t.Fatalf("cross room read %s: %d", p, got)
		}
		if got := call(s, "POST", p, item, tok).Code; got != 403 {
			t.Fatalf("cross room write %s: %d", p, got)
		}
	}
	rec := call(s, "GET", "/api/rooms/office/requests", nil, office)
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "\"title\":\"A\"") {
		t.Fatal("cross-room disclosure")
	}
}
func TestInvitesExpireOneTimeAndRevoke(t *testing.T) {
	s, now := fixture(t)
	inv := call(s, "POST", "/api/admin/invites", map[string]string{"roomId": "living"}, strings.Repeat("a", 32))
	code := getJSON(t, inv, "inviteCode")
	tok := join(t, s, "living")
	call(s, "POST", "/api/admin/revoke", map[string]string{"sessionToken": tok}, strings.Repeat("a", 32))
	if got := call(s, "GET", "/api/rooms/living/requests", nil, tok).Code; got != 403 {
		t.Fatalf("revoked session: %d", got)
	}
	if got := call(s, "POST", "/api/sessions", map[string]string{"inviteCode": code}, "").Code; got != 201 {
		t.Fatalf("first exchange: %d", got)
	}
	if got := call(s, "POST", "/api/sessions", map[string]string{"inviteCode": code}, "").Code; got != 401 {
		t.Fatalf("invite reused: %d", got)
	}
	later := call(s, "POST", "/api/admin/invites", map[string]string{"roomId": "living"}, strings.Repeat("a", 32))
	laterCode := getJSON(t, later, "inviteCode")
	*now = now.Add(11 * time.Minute)
	if got := call(s, "POST", "/api/sessions", map[string]string{"inviteCode": laterCode}, "").Code; got != 401 {
		t.Fatalf("expired invite: %d", got)
	}
}
func TestLimitsAndMalformedInput(t *testing.T) {
	s, _ := fixture(t)
	tok := join(t, s, "office")
	for i := 0; i < 5; i++ {
		if got := call(s, "POST", "/api/rooms/office/requests", map[string]string{"title": "Track", "source": "cloud"}, tok).Code; got != 201 {
			t.Fatalf("request %d: %d", i, got)
		}
	}
	if got := call(s, "POST", "/api/rooms/office/requests", map[string]string{"title": "Track", "source": "cloud"}, tok).Code; got != 429 {
		t.Fatalf("limit: %d", got)
	}
	if got := call(s, "POST", "/api/rooms/office/requests", map[string]string{"title": "X", "source": "web"}, join(t, s, "office")).Code; got != 400 {
		t.Fatalf("invalid source: %d", got)
	}
	if _, err := newServer(config{Rooms: []roomConfig{{"a", "A", "x"}, {"b", "B", "x"}}}, strings.Repeat("a", 32), time.Now); err == nil {
		t.Fatal("duplicate player accepted")
	}
	if _, err := newServer(config{Rooms: []roomConfig{{"a", "A", "x"}}}, "short", time.Now); err == nil {
		t.Fatal("weak host token accepted")
	}
}
func TestConcurrentRequestsBounded(t *testing.T) {
	s, _ := fixture(t)
	tok := join(t, s, "living")
	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			call(s, "POST", "/api/rooms/living/requests", map[string]string{"title": "Track", "source": "local"}, tok)
		}()
	}
	wg.Wait()
	rec := call(s, "GET", "/api/rooms/living/requests", nil, tok)
	var body struct {
		Requests []request `json:"requests"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Requests) != 5 {
		t.Fatalf("expected 5 got %d", len(body.Requests))
	}
}
func TestStaticSecurityHeaders(t *testing.T) {
	s, _ := fixture(t)
	r := call(s, "GET", "/", nil, "")
	if r.Code != 200 || r.Header().Get("Content-Security-Policy") == "" || r.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("missing security headers: %d", r.Code)
	}
}
