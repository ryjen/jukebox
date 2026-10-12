// ma-probe is a read-only compatibility check for an explicitly configured
// Music Assistant instance. It never changes queues, playback or providers.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ryjen/jukebox/internal/musicassistant"
)

type result struct {
	Status        string `json:"status"`
	Queue         string `json:"queue"`
	QueueItems    int    `json:"queueItems"`
	Search        string `json:"search"`
	SearchMatches int    `json:"searchMatches"`
	Failure       string `json:"failure,omitempty"`
}

func probe(ctx context.Context, env func(string) string) (result, int) {
	r := result{Status: "failed", Queue: "not_checked", Search: "not_checked"}
	url := strings.TrimSpace(env("JUKEBOX_MA_URL"))
	token := env("JUKEBOX_MA_TOKEN")
	player := strings.TrimSpace(env("JUKEBOX_MA_PLAYER_ID"))
	query := strings.TrimSpace(env("JUKEBOX_MA_SEARCH_QUERY"))
	if url == "" || strings.TrimSpace(token) == "" || player == "" || query == "" {
		r.Failure = "missing_configuration"
		return r, 2
	}
	client, err := musicassistant.New(url, token)
	if err != nil {
		r.Failure = "invalid_connection_configuration"
		return r, 2
	}
	snap, err := client.ReadQueue(ctx, player)
	if err != nil {
		if errors.Is(err, musicassistant.ErrGrouped) {
			r.Queue = "grouped_denied"
			r.Failure = "group_membership"
		} else {
			r.Queue = "unavailable"
			r.Failure = "queue_read_failed"
		}
		return r, 1
	}
	r.Queue = "ok"
	r.QueueItems = len(snap.Items)
	matches, err := client.SearchTracks(ctx, query)
	if err != nil {
		r.Search = "unavailable"
		r.Failure = "search_failed"
		return r, 1
	}
	r.Search = "ok"
	r.SearchMatches = len(matches)
	r.Status = "pass"
	return r, 0
}

func run(ctx context.Context, output io.Writer, env func(string) string) int {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	report, exit := probe(ctx, env)
	if err := json.NewEncoder(output).Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, "probe output unavailable")
		return 1
	}
	return exit
}

func main() {
	os.Exit(run(context.Background(), os.Stdout, os.Getenv))
}
