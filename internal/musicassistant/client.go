// Package musicassistant implements a constrained, read-only HTTP adapter for
// Music Assistant's 2.10.6 POST /api commands. No arbitrary command execution.
package musicassistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	ErrUnavailable = errors.New("music assistant unavailable")
	ErrGrouped     = errors.New("room is currently using a different queue")
)

const maxResponseBytes = 1 << 20

type Client struct {
	endpoint string
	token    string
	http     *http.Client
}

type Item struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Snapshot struct {
	QueueID  string `json:"queueId"`
	State    string `json:"state"`
	Items    []Item `json:"items"`
	Playback string `json:"playback"`
}

// New requires HTTPS for remote hosts. Loopback HTTP is useful for local
// deployments, but insecure remote bearer transport is always rejected.
func New(rawURL, token string) (*Client, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("invalid Music Assistant base URL")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return nil, errors.New("Music Assistant requires HTTPS except for literal loopback IPs")
		}
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("Music Assistant read-only token required")
	}
	return &Client{
		endpoint: strings.TrimRight(u.String(), "/") + "/api",
		token:    token,
		http: &http.Client{
			Timeout: 5 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

// command sends only hardcoded queue-read commands; callers cannot supply
// command names. The 2.10.6 HTTP API returns the result directly as JSON.
func (c *Client) command(ctx context.Context, name string, args any) (json.RawMessage, error) {
	if name != "player_queues/get_active_queue" && name != "player_queues/items" && name != "players/get" && name != "music/search" {
		return nil, ErrUnavailable
	}
	payload, err := json.Marshal(struct {
		MessageID string `json:"message_id"`
		Command   string `json:"command"`
		Args      any    `json:"args"`
	}{"jukebox-read", name, args})
	if err != nil {
		return nil, ErrUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, ErrUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: transport", ErrUnavailable)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: upstream status %d", ErrUnavailable, response.StatusCode)
	}
	contentType := response.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(contentType), "application/json") {
		return nil, fmt.Errorf("%w: unexpected content type", ErrUnavailable)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(raw) > maxResponseBytes || !json.Valid(raw) {
		return nil, fmt.Errorf("%w: invalid response", ErrUnavailable)
	}
	return raw, nil
}

// requireStandalonePlayer checks group membership explicitly: a native group
// coordinator may still own a queue with the same ID as its player. A
// read-only player scope is required in addition to QUEUES_READ.
func (c *Client) requireStandalonePlayer(ctx context.Context, playerID string) error {
	data, err := c.command(ctx, "players/get", map[string]any{"player_id": playerID})
	if err != nil {
		return err
	}
	var state struct {
		PlayerID string   `json:"player_id"`
		Type string       `json:"type"`
		Available *bool   `json:"available"`
		Enabled *bool     `json:"enabled"`
		GroupMembers *[]string `json:"group_members"`
		StaticGroupMembers []string `json:"static_group_members"`
		SyncedTo *string  `json:"synced_to"`
		ActiveGroup *string `json:"active_group"`
	}
	if err := json.Unmarshal(data, &state); err != nil ||
		state.PlayerID != playerID || state.Type == "" ||
		state.Available == nil || !*state.Available || state.Enabled == nil || !*state.Enabled ||
		state.GroupMembers == nil {
		return fmt.Errorf("%w: invalid player state", ErrUnavailable)
	}
	if state.Type == "group" || len(state.StaticGroupMembers) != 0 ||
		(state.SyncedTo != nil && *state.SyncedTo != "") ||
		(state.ActiveGroup != nil && *state.ActiveGroup != "") {
		return ErrGrouped
	}
	for _, memberID := range *state.GroupMembers {
		if memberID != playerID {
			return ErrGrouped
		}
	}
	return nil
}

// ReadQueue refuses group queues, including native coordinator-owned queues,
// until explicit group membership authorization exists. No write is possible.
func (c *Client) ReadQueue(ctx context.Context, playerID string) (Snapshot, error) {
	var empty Snapshot
	if playerID == "" {
		return empty, ErrUnavailable
	}
	if err := c.requireStandalonePlayer(ctx, playerID); err != nil {
		return empty, err
	}
	raw, err := c.command(ctx, "player_queues/get_active_queue", map[string]any{"player_id": playerID})
	if err != nil {
		return empty, err
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return empty, ErrUnavailable
	}
	var active struct {
		QueueID string `json:"queue_id"`
		State   string `json:"state"`
		CurrentIndex *int `json:"current_index"`
	}
	if err := json.Unmarshal(raw, &active); err != nil || active.QueueID == "" {
		return empty, fmt.Errorf("%w: unknown queue response", ErrUnavailable)
	}
	if active.QueueID != playerID {
		return empty, ErrGrouped
	}
	// Prefer the current item and upcoming tracks, not already-played entries.
	offset := 0
	if active.CurrentIndex != nil {
		if *active.CurrentIndex < 0 || *active.CurrentIndex > 1000000 {
			return empty, fmt.Errorf("%w: invalid playhead", ErrUnavailable)
		}
		offset = *active.CurrentIndex
	}
	raw, err = c.command(ctx, "player_queues/items", map[string]any{"queue_id": active.QueueID, "limit": 25, "offset": offset})
	if err != nil {
		return empty, err
	}
	var entries []struct {
		QueueItemID string `json:"queue_item_id"`
		Name        string `json:"name"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil || entries == nil {
		return empty, fmt.Errorf("%w: unknown item response", ErrUnavailable)
	}
	// Recheck membership after the I/O boundary to prevent returning another
	// room's queue when the player was grouped during the request.
	if err := c.requireStandalonePlayer(ctx, playerID); err != nil {
		return empty, err
	}
	result := Snapshot{QueueID: active.QueueID, State: active.State, Items: make([]Item, 0, len(entries)), Playback: "music-assistant"}
	for _, entry := range entries {
		result.Items = append(result.Items, Item{ID: entry.QueueItemID, Name: entry.Name})
	}
	return result, nil
}
