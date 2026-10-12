package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ryjen/jukebox/internal/musicassistant"
)

func TestQueueReadIsRoomScoped(t *testing.T) {
	count := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
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
	if count != 0 {
		t.Fatal("cross-room request hit upstream")
	}
	r := call(s, "GET", "/api/rooms/living/queue", nil, guest)
	if r.Code != 200 || !strings.Contains(r.Body.String(), "A song") {
		t.Fatalf("queue: %d %s", r.Code, r.Body.String())
	}
	if strings.Contains(r.Body.String(), "example-token") || count != 2 {
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
