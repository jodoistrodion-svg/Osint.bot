package model

import "time"

type SearchResult struct {
	Query     string                 `json:"query"`
	Type      string                 `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Sources   map[string]interface{} `json:"sources"`
}
