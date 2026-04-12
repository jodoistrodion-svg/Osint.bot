package model

import "time"

type QueryType string

const (
	QueryUniversal QueryType = "universal"
	QueryPhone     QueryType = "phone"
	QueryEmail     QueryType = "email"
	QueryFIO       QueryType = "fio"
	QueryAddress   QueryType = "address"
	QueryCar       QueryType = "car"
)

type SearchHit struct {
	Source  string            `json:"source"`
	Title   string            `json:"title"`
	Snippet string            `json:"snippet"`
	Fields  map[string]string `json:"fields"`
}

type SearchResult struct {
	Query      string                 `json:"query"`
	Type       QueryType              `json:"type"`
	Timestamp  time.Time              `json:"timestamp"`
	DurationMS int64                  `json:"duration_ms"`
	Hits       []SearchHit            `json:"hits"`
	Meta       map[string]interface{} `json:"meta,omitempty"`
}
