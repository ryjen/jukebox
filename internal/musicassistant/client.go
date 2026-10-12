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
    ErrGrouped = errors.New("room is currently using a different queue")
)

const maxResponseBytes = 1 << 20

type Client struct {
    endpoint string
    token string
    http *http.Client
}

type Item struct {
    ID string `json:"id"`
    Name string `json:"name"`
}

type Snapshot struct {
    QueueID string `json:"queueId"`
    State string `json:"state"`
    Items []Item `json:"items"`
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
        token: token,
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
    if name != "player_queues/get_active_queue" && name != "player_queues/items" {
        return nil, ErrUnavailable
    }
    payload, err := json.Marshal(struct {
        MessageID string `json:"message_id"`
        Command string `json:"command"`
        Args any `json:"args"`
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

// ReadQueue refuses group-player queue changes until Jukebox implements
// explicit group membership authorization. No write or autoplay is possible.
func (c *Client) ReadQueue(ctx context.Context, playerID string) (Snapshot, error) {
    var empty Snapshot
    if playerID == "" {
        return empty, ErrUnavailable
    }
    raw, err := c.command(ctx, "player_queues/get_active_queue", map[string]any{"player_id": playerID})
    if err != nil { return empty, err }
    if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) { return empty, ErrUnavailable }
    var active struct {
        QueueID string `json:"queue_id"`
        State string `json:"state"`
    }
    if err := json.Unmarshal(raw, &active); err != nil || active.QueueID == "" {
        return empty, fmt.Errorf("%w: unknown queue response", ErrUnavailable)
    }
    if active.QueueID != playerID {
        return empty, ErrGrouped
    }
    raw, err = c.command(ctx, "player_queues/items", map[string]any{"queue_id": active.QueueID, "limit": 25, "offset": 0})
    if err != nil { return empty, err }
    var entries []struct {
        QueueItemID string `json:"queue_item_id"`
        Name string `json:"name"`
    }
    if err := json.Unmarshal(raw, &entries); err != nil || entries == nil {
        return empty, fmt.Errorf("%w: unknown item response", ErrUnavailable)
    }
    result := Snapshot{QueueID:active.QueueID, State:active.State, Items:make([]Item, 0, len(entries)), Playback:"music-assistant"}
    for _, entry := range entries {
        result.Items = append(result.Items, Item{ID:entry.QueueItemID, Name:entry.Name})
    }
    return result,nil
}
