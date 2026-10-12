package musicassistant

import (
    "context"
    "encoding/json"
    "errors"
    "strings"
    "unicode/utf8"
)

// Track is a deliberately narrow projection of Music Assistant's SearchResults.
// Do not return raw provider metadata, credentials, media mappings or URLs.
type Track struct {
    URI string `json:"uri"`
    Name string `json:"name"`
    Artist string `json:"artist"`
    Provider string `json:"provider"`
}

// SearchTracks uses the upstream 2.10.6 music/search command. This is a
// read-only federated search; Music Assistant decides available providers.
func (c *Client) SearchTracks(ctx context.Context, query string) ([]Track, error) {
    query = strings.TrimSpace(query)
    n := utf8.RuneCountInString(query)
    if n < 2 || n > 80 {
        return nil, errors.New("search must be between 2 and 80 characters")
    }
    raw, err := c.command(ctx, "music/search", map[string]any{
        "search_query": query,
        "media_types": []string{"track"},
        "limit": 15,
    })
    if err != nil { return nil, err }
    var data struct {
        Tracks []struct {
            URI string `json:"uri"`
            Name string `json:"name"`
            Provider string `json:"provider"`
            Artists []struct { Name string `json:"name"` } `json:"artists"`
        } `json:"tracks"`
    }
    if err := json.Unmarshal(raw,&data); err != nil || data.Tracks == nil {
        return nil, ErrUnavailable
    }
    result := make([]Track, 0, min(len(data.Tracks),15))
    for _,item := range data.Tracks {
        if item.URI == "" || item.Name == "" { continue }
        artist := ""
        if len(item.Artists)>0 { artist = item.Artists[0].Name }
        result = append(result,Track{URI:item.URI, Name:item.Name, Artist:artist,Provider:item.Provider})
        if len(result)==15 { break }
    }
    return result,nil
}
