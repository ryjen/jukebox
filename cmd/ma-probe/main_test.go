package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func environment(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestReadOnlyProbeReportsCountsNotMusicOrSecrets(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/api" || r.Header.Get("Authorization") != "Bearer sample-secret" {
			t.Errorf("bad MA request: %s %s", r.Method, r.URL)
		}
		var command struct {
			Command string         `json:"command"`
			Args    map[string]any `json:"args"`
		}
		if err := json.NewDecoder(r.Body).Decode(&command); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch command.Command {
		case "players/get":
			if command.Args["player_id"] != "living-player" {
				t.Error("wrong player mapping")
			}
			fmt.Fprint(w, `{"player_id":"living-player","type":"player","available":true,"enabled":true,"group_members":[]}`)
		case "player_queues/get_active_queue":
			fmt.Fprint(w, `{"queue_id":"living-player","state":"playing","current_index":37}`)
		case "player_queues/items":
			if command.Args["offset"] != float64(37) || command.Args["limit"] != float64(25) {
				t.Errorf("wrong bounded window: %v", command.Args)
			}
			fmt.Fprint(w, `[{"queue_item_id":"abc","name":"Do not expose personal title"}]`)
		case "music/search":
			if command.Args["search_query"] != "demo song" {
				t.Errorf("wrong query %v", command.Args)
			}
			fmt.Fprint(w, `{"tracks":[{"uri":"filesystem://track/private","name":"private track","provider":"filesystem","artists":[{"name":"Private Artist"}]},{"uri":"qobuz://track/private","name":"cloud track","provider":"qobuz","artists":[{"name":"Another"}]}]}`)
		default:
			t.Errorf("unexpected potentially mutating API call: %s", command.Command)
		}
	}))
	defer server.Close()
	config := environment(map[string]string{"JUKEBOX_MA_URL": server.URL, "JUKEBOX_MA_TOKEN": "sample-secret", "JUKEBOX_MA_PLAYER_ID": "living-player", "JUKEBOX_MA_SEARCH_QUERY": "demo song"})
	var output bytes.Buffer
	code := run(context.Background(), &output, config)
	if code != 0 || calls.Load() != 5 {
		t.Fatalf("code=%d calls=%d report=%s", code, calls.Load(), output.String())
	}
	var report result
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "pass" || report.Queue != "ok" || report.QueueItems != 1 || report.Search != "ok" || report.SearchMatches != 2 {
		t.Fatalf("unexpected report: %+v", report)
	}
	for _, s := range []string{"sample-secret", "private track", "Private Artist", "living-player", "demo song", "filesystem://"} {
		if strings.Contains(output.String(), s) {
			t.Fatalf("sensitive detail leaked in report: %q", s)
		}
	}
}

func TestProbeMissingConfigurationAndGroupFailClosed(t *testing.T) {
	var output bytes.Buffer
	if code := run(context.Background(), &output, environment(map[string]string{})); code != 2 || !strings.Contains(output.String(), "missing_configuration") {
		t.Fatalf("unsafe empty config: code=%d report=%s", code, output.String())
	}
	output.Reset()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var command struct {
			Command string `json:"command"`
		}
		if err := json.NewDecoder(r.Body).Decode(&command); err != nil {
			t.Error(err)
		}
		if command.Command != "players/get" {
			t.Fatalf("unexpected call after grouped denial: %s", command.Command)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"player_id":"living-player","type":"group","available":true,"enabled":true,"group_members":["member"]}`)
	}))
	defer server.Close()
	config := environment(map[string]string{"JUKEBOX_MA_URL": server.URL, "JUKEBOX_MA_TOKEN": "secret", "JUKEBOX_MA_PLAYER_ID": "living-player", "JUKEBOX_MA_SEARCH_QUERY": "demo song"})
	if code := run(context.Background(), &output, config); code != 1 {
		t.Fatalf("expected grouped failure: %d %s", code, output.String())
	}
	if calls.Load() != 1 || !strings.Contains(output.String(), "group_membership") || strings.Contains(output.String(), "secret") {
		t.Fatalf("invalid denied probe: calls=%d result=%s", calls.Load(), output.String())
	}
}

func TestProbePermissionDeniedIsSanitized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "private upstream token error")
	}))
	defer server.Close()
	var out bytes.Buffer
	config := environment(map[string]string{"JUKEBOX_MA_URL": server.URL, "JUKEBOX_MA_TOKEN": "secret", "JUKEBOX_MA_PLAYER_ID": "living-player", "JUKEBOX_MA_SEARCH_QUERY": "demo song"})
	if code := run(context.Background(), &out, config); code != 1 || !strings.Contains(out.String(), "queue_read_failed") || strings.Contains(out.String(), "private upstream") {
		t.Fatalf("permission failure not sanitized: %d %s", code, out.String())
	}
}
