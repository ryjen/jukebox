package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ryjen/jukebox/internal/musicassistant"
)

func TestQueueReadIsRoomScoped(t *testing.T) {
	var count atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		var c struct {
			Command string `json:"command"`
		}
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		if c.Command == "player_queues/get_active_queue" {
			fmt.Fprint(w, `{"queue_id":"p1","state":"playing"}`)
		} else if c.Command == "player_queues/items" {
			fmt.Fprint(w, `[{"queue_item_id":"x1","name":"A song"}]`)
		} else {
			t.Errorf("unexpected command %s", c.Command)
		}
	}))
	defer upstream.Close()
	s, _ := fixture(t)
	c, err := musicassistant.New(upstream.URL, "example-token")
	if err != nil {
		t.Fatal(err)
	}
	s.ma = c
	guest := join(t, s, "living")
	other := join(t, s, "office")
	if got := call(s, "GET", "/api/rooms/living/queue", nil, other).Code; got != 403 {
		t.Fatal(got)
	}
	if count.Load() != 0 {
		t.Fatal("cross-room request hit upstream")
	}
	r := call(s, "GET", "/api/rooms/living/queue", nil, guest)
	if r.Code != 200 || !strings.Contains(r.Body.String(), "A song") {
		t.Fatalf("queue: %d %s", r.Code, r.Body.String())
	}
	if strings.Contains(r.Body.String(), "example-token") || count.Load() != 2 {
		t.Fatal("exposure or wrong command count")
	}
}

func TestQueueDisabledAndGroupsDenied(t *testing.T) {
	s, _ := fixture(t)
	guest := join(t, s, "living")
	if got := call(s, "GET", "/api/rooms/living/queue", nil, guest).Code; got != 503 {
		t.Fatal(got)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"queue_id":"other-group"}`)
	}))
	defer upstream.Close()
	client, err := musicassistant.New(upstream.URL, "example-token")
	if err != nil {
		t.Fatal(err)
	}
	s.ma = client
	if got := call(s, "GET", "/api/rooms/living/queue", nil, guest).Code; got != 409 {
		t.Fatal(got)
	}
}

func TestRoomBoundFederatedSearchAndQuota(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var command struct {
			Command string `json:"command"`
		}
		if err := json.NewDecoder(r.Body).Decode(&command); err != nil {
			t.Error(err)
		}
		if command.Command != "music/search" {
			t.Errorf("unexpected upstream command %q", command.Command)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tracks":[{"uri":"library://track/1","name":"Dreams","artists":[{"name":"Fleetwood Mac"}],"provider":"library"}]}`)
	}))
	defer upstream.Close()
	s, _ := fixture(t)
	ma, err := musicassistant.New(upstream.URL, "example-token")
	if err != nil {
		t.Fatal(err)
	}
	s.ma = ma
	guest := join(t, s, "living")
	other := join(t, s, "office")
	if got := call(s, "GET", "/api/rooms/living/search?q=dreams", nil, other).Code; got != 403 {
		t.Fatalf("cross-room search access: %d", got)
	}
	if got := call(s, "GET", "/api/rooms/living/search?q=x", nil, guest).Code; got != 400 {
		t.Fatalf("invalid search accepted: %d", got)
	}
	if calls.Load() != 0 {
		t.Fatal("unauthorized or malformed search contacted upstream")
	}
	for i := 0; i < 40; i++ {
		r := call(s, "GET", "/api/rooms/living/search?q=dreams", nil, guest)
		if r.Code != 200 || !strings.Contains(r.Body.String(), "Fleetwood Mac") {
			t.Fatalf("search %d failed: %d %s", i, r.Code, r.Body.String())
		}
	}
	if got := call(s, "GET", "/api/rooms/living/search?q=dreams", nil, guest).Code; got != 429 {
		t.Fatalf("search rate limit bypass: %d", got)
	}
	if calls.Load() != 40 {
		t.Fatalf("unexpected call count: %d", calls.Load())
	}
	if got := call(s, "GET", "/api/rooms/office/search?q=dreams", nil, other).Code; got != 200 {
		t.Fatalf("unrelated room wrongly throttled: %d", got)
	}
}
