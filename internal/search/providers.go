package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"osint.bot/internal/model"
)

type LeakInfo struct {
	Source   string            `json:"source"`
	Title    string            `json:"title"`
	Snippet  string            `json:"snippet"`
	Fields   map[string]string `json:"fields"`
	Severity string            `json:"severity,omitempty"`
}

type HTTPSource struct {
	name   string
	client *http.Client
}

func (s *HTTPSource) Name() string { return s.name }

func NewPsbdmpSource(client *http.Client) Source {
	return &psbdmpSource{HTTPSource{name: "psbdmp", client: client}}
}
func NewScyllaSource(client *http.Client) Source {
	return &scyllaSource{HTTPSource{name: "scylla", client: client}}
}
func NewLeakCheckSource(client *http.Client, apiKey string) Source {
	return &leakCheckSource{HTTPSource{name: "leakcheck", client: client}, apiKey}
}
func NewHIBPSource(client *http.Client, apiKey string) Source {
	return &hibpSource{HTTPSource{name: "hibp", client: client}, apiKey}
}
func NewHoleheSource(client *http.Client) Source {
	return &holeheSource{HTTPSource{name: "holehe", client: client}}
}

type psbdmpSource struct{ HTTPSource }
type scyllaSource struct{ HTTPSource }
type leakCheckSource struct {
	HTTPSource
	apiKey string
}
type hibpSource struct {
	HTTPSource
	apiKey string
}
type holeheSource struct{ HTTPSource }

func (s *psbdmpSource) Search(ctx context.Context, query string, _ model.QueryType) ([]model.SearchHit, error) {
	endpoint := "https://psbdmp.cc/api/search?q=" + url.QueryEscape(query)
	var payload map[string]interface{}
	if err := doJSON(ctx, s.client, endpoint, nil, &payload); err != nil {
		return nil, err
	}
	return leakMapToHits(s.name, payload), nil
}

func (s *scyllaSource) Search(ctx context.Context, query string, _ model.QueryType) ([]model.SearchHit, error) {
	endpoint := "https://scylla.so/api/search?q=" + url.QueryEscape(query)
	var payload map[string]interface{}
	if err := doJSON(ctx, s.client, endpoint, nil, &payload); err != nil {
		return nil, err
	}
	return leakMapToHits(s.name, payload), nil
}

func (s *leakCheckSource) Search(ctx context.Context, query string, _ model.QueryType) ([]model.SearchHit, error) {
	endpoint := "https://leakcheck.io/api/public?check=" + url.QueryEscape(query)
	headers := map[string]string{}
	if s.apiKey != "" {
		headers["X-API-Key"] = s.apiKey
	}
	var payload map[string]interface{}
	if err := doJSON(ctx, s.client, endpoint, headers, &payload); err != nil {
		return nil, err
	}
	return leakMapToHits(s.name, payload), nil
}

func (s *hibpSource) Search(ctx context.Context, query string, qType model.QueryType) ([]model.SearchHit, error) {
	if qType != model.QueryEmail {
		return nil, nil
	}
	endpoint := "https://haveibeenpwned.com/api/v3/breachedaccount/" + url.PathEscape(query)
	headers := map[string]string{"User-Agent": "osint-bot/1.0"}
	if s.apiKey != "" {
		headers["hibp-api-key"] = s.apiKey
	}
	var breaches []map[string]interface{}
	if err := doJSON(ctx, s.client, endpoint, headers, &breaches); err != nil {
		if strings.Contains(err.Error(), "404") {
			return nil, nil
		}
		return nil, err
	}

	hits := make([]model.SearchHit, 0, len(breaches))
	for _, b := range breaches {
		title := fmt.Sprintf("Breach: %v", b["Name"])
		hits = append(hits, model.SearchHit{
			Source:  s.name,
			Title:   title,
			Snippet: fmt.Sprintf("Domain: %v | Date: %v", b["Domain"], b["BreachDate"]),
			Fields: map[string]string{
				"pwn_count": fmt.Sprintf("%v", b["PwnCount"]),
			},
		})
	}
	return hits, nil
}

func (s *holeheSource) Search(ctx context.Context, query string, qType model.QueryType) ([]model.SearchHit, error) {
	if qType != model.QueryEmail {
		return nil, nil
	}
	endpoint := "https://holehe.deno.dev/?email=" + url.QueryEscape(query)
	var payload []map[string]interface{}
	if err := doJSON(ctx, s.client, endpoint, nil, &payload); err != nil {
		return nil, err
	}
	hits := make([]model.SearchHit, 0, len(payload))
	for _, v := range payload {
		if ok, _ := v["exists"].(bool); !ok {
			continue
		}
		hits = append(hits, model.SearchHit{
			Source:  s.name,
			Title:   fmt.Sprintf("Account exists on %v", v["name"]),
			Snippet: fmt.Sprintf("Rate limit: %v", v["rateLimit"]),
			Fields:  map[string]string{"email": query},
		})
	}
	return hits, nil
}

func doJSON(ctx context.Context, client *http.Client, endpoint string, headers map[string]string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "osint-bot/1.0")
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode failed: %w", err)
	}
	return nil
}

func leakMapToHits(source string, payload map[string]interface{}) []model.SearchHit {
	if len(payload) == 0 {
		return nil
	}
	flat := make(map[string]string, len(payload))
	for k, v := range payload {
		flat[k] = fmt.Sprintf("%v", v)
	}
	return []model.SearchHit{{
		Source:  source,
		Title:   "Leak record found",
		Snippet: "Найдены данные в публичной базе утечек",
		Fields:  flat,
	}}
}
