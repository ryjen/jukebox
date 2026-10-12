package musicassistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestFederatedSearchIsReadOnlyAndSanitized(t *testing.T) {
	client, closeServer := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		var cmd struct {
			Command string         `json:"command"`
			Args    map[string]any `json:"args"`
		}
		if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
			t.Error(err)
		}
		if cmd.Command != "music/search" || cmd.Args["search_query"] != "dreams" || cmd.Args["limit"] != float64(15) {
			t.Errorf("unexpected search command or parameters: %+v", cmd)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tracks":[{"uri":"filesystem://track/1","name":"Dreams","artists":[{"name":"Fleetwood Mac"}],"provider":"filesystem","secrets":"omit"},{"uri":"spotify://track/2","name":"Dreams (Live)","artists":[{"name":"Other"}],"provider":"spotify"}]}`)
	})
	defer closeServer()
	tracks, err := client.SearchTracks(context.Background(), "  dreams  ")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 2 || tracks[0].Artist != "Fleetwood Mac" || tracks[1].Provider != "spotify" {
		t.Fatalf("bad results: %+v", tracks)
	}
	data, _ := json.Marshal(tracks)
	if strings.Contains(string(data), "secrets") {
		t.Fatal("untrusted upstream field leaked")
	}
}

func TestSearchRejectsBadInputAndMalformedResponses(t *testing.T) {
	client, closeServer := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"unexpected":"not-a-search-result"}`)
	})
	defer closeServer()
	for _, query := range []string{"", "x", strings.Repeat("中", 81)} {
		if _, err := client.SearchTracks(context.Background(), query); err == nil {
			t.Fatalf("invalid query accepted: %q", query)
		}
	}
	if _, err := client.SearchTracks(context.Background(), "valid search"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unexpected shape accepted: %v", err)
	}
}
