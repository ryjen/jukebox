package musicassistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestNewRequiresEncryptedOrLoopbackTransport(t *testing.T) {
	for _, address := range []string{"http://example.com:8095", "http://localhost:8095", "https://user:pass@example.com", "https://example.com/api", "http://192.168.1.9:8095", "https://example.com?secret=yes", "file:///etc/passwd"} {
		if _, err := New(address, "read-token"); err == nil {
			t.Fatalf("unsafe Music Assistant target accepted: %q", address)
		}
	}
	if _, err := New("https://example.com:8095", ""); err == nil {
		t.Fatal("missing bearer token accepted")
	}
	if _, err := New("https://example.com:8095", "read-token"); err != nil {
		t.Fatal(err)
	}
	if _, err := New("http://127.0.0.1:8095", "read-token"); err != nil {
		t.Fatal(err)
	}
}

func localClient(t *testing.T, handler http.HandlerFunc) (*Client, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	client, err := New(server.URL, "read-secret")
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	return client, server.Close
}

func TestReadQueueNeverMutatesPlaybackAndRedactsUpstreamData(t *testing.T) {
	var mu sync.Mutex
	commands := []string{}
	client, closeServer := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api" || r.Header.Get("Authorization") != "Bearer read-secret" {
			t.Errorf("invalid upstream request: %s %s", r.Method, r.URL)
			w.WriteHeader(403)
			return
		}
		var cmd struct {
			Command   string         `json:"command"`
			MessageID string         `json:"message_id"`
			Args      map[string]any `json:"args"`
		}
		if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
			t.Error(err)
		}
		mu.Lock()
		commands = append(commands, cmd.Command)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch cmd.Command {
		case "player_queues/get_active_queue":
			if cmd.Args["player_id"] != "room-1" {
				t.Error("wrong player", cmd.Args)
			}
			fmt.Fprint(w, `{"queue_id":"room-1","state":"playing","super_secret":"never-expose"}`)
		case "player_queues/items":
			if cmd.Args["queue_id"] != "room-1" || cmd.Args["limit"] != float64(25) {
				t.Errorf("invalid bounded query: %+v", cmd.Args)
			}
			fmt.Fprint(w, `[{"queue_item_id":"q1","name":"曲 ♫","credentials":"private"}]`)
		default:
			t.Errorf("unexpected (possibly mutating) command: %s", cmd.Command)
			w.WriteHeader(400)
		}
	})
	defer closeServer()
	result, err := client.ReadQueue(context.Background(), "room-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.QueueID != "room-1" || result.State != "playing" || len(result.Items) != 1 || result.Items[0].Name != "曲 ♫" {
		t.Fatalf("invalid snapshot: %+v", result)
	}
	raw, _ := json.Marshal(result)
	if strings.Contains(string(raw), "read-secret") || strings.Contains(string(raw), "super_secret") || strings.Contains(string(raw), "credentials") {
		t.Fatalf("sensitive fields leaked: %s", raw)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(commands) != 2 || commands[0] != "player_queues/get_active_queue" || commands[1] != "player_queues/items" {
		t.Errorf("unexpected upstream commands: %v", commands)
	}
}

func TestGroupedQueueFailsClosedBeforeReadingItems(t *testing.T) {
	var calls atomic.Int64
	client, closeServer := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"queue_id":"group-different","state":"playing"}`)
	})
	defer closeServer()
	_, err := client.ReadQueue(context.Background(), "room-1")
	if !errors.Is(err, ErrGrouped) || calls.Load() != 1 {
		t.Fatalf("expected grouped rejection before item read, got %v (%d calls)", err, calls.Load())
	}
}

func TestFailClosedOnBadResponseAndDeniedScope(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		contentType string
		payload     string
	}{
		{"denied", 403, "text/plain", "Missing queues.read"},
		{"missing-auth", 401, "text/plain", "Auth required"},
		{"missing", 200, "application/json", "null"},
		{"invalid", 200, "application/json", "not-json"},
		{"oversized", 200, "application/json", strings.Repeat("x", maxResponseBytes+1)},
		{"bad-type", 200, "text/html", "<html>problem</html>"},
		{"wrong-shape", 200, "application/json", `{"unknown":"queue"}`},
		{"items-wrong-shape", 200, "application/json", `{"queue_id":"room-1"}`},
	}
	for _, testcase := range cases {
		t.Run(testcase.name, func(t *testing.T) {
			count := 0
			client, closeServer := localClient(t, func(w http.ResponseWriter, r *http.Request) {
				count++
				w.Header().Set("Content-Type", testcase.contentType)
				w.WriteHeader(testcase.status)
				if testcase.name == "items-wrong-shape" && count == 2 {
					fmt.Fprint(w, `{"items":"no"}`)
				} else {
					fmt.Fprint(w, testcase.payload)
				}
			})
			defer closeServer()
			_, err := client.ReadQueue(context.Background(), "room-1")
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("expected unavailable error, got %v", err)
			}
		})
	}
}

func TestDoesNotFollowRedirectWithCredential(t *testing.T) {
	var destinationHits atomic.Int64
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationHits.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("credential leaked")
		}
	}))
	defer destination.Close()
	client, closeServer := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/steal", http.StatusTemporaryRedirect)
	})
	defer closeServer()
	_, err := client.ReadQueue(context.Background(), "room-1")
	if !errors.Is(err, ErrUnavailable) || destinationHits.Load() != 0 {
		t.Fatalf("followed redirect: %v (%d)", err, destinationHits.Load())
	}
}
