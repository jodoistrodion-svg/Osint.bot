package search

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"

	"osint.bot/internal/model"
)

type Source interface {
	Name() string
	Search(ctx context.Context, query string, qType model.QueryType) ([]model.SearchHit, error)
}

type Service struct {
	sources []Source
}

func NewService(sources ...Source) *Service {
	return &Service{sources: sources}
}

func DetectType(query string) model.QueryType {
	q := strings.TrimSpace(query)
	if q == "" {
		return model.QueryUniversal
	}

	phonePattern := regexp.MustCompile(`^\+?[0-9\-\s()]{7,}$`)
	emailPattern := regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	carPattern := regexp.MustCompile(`^[А-ЯA-Z]\d{3}[А-ЯA-Z]{2}\d{2,3}$`)

	switch {
	case emailPattern.MatchString(q):
		return model.QueryEmail
	case phonePattern.MatchString(q):
		return model.QueryPhone
	case carPattern.MatchString(strings.ToUpper(strings.ReplaceAll(q, " ", ""))):
		return model.QueryCar
	case strings.Count(q, " ") >= 2:
		return model.QueryFIO
	case strings.ContainsAny(strings.ToLower(q), "ул., дом kv apt street avenue"):
		return model.QueryAddress
	default:
		return model.QueryUniversal
	}
}

func (s *Service) Search(ctx context.Context, query string, explicitType model.QueryType) *model.SearchResult {
	started := time.Now()
	qType := explicitType
	if qType == "" || qType == model.QueryUniversal {
		qType = DetectType(query)
	}
	if qType == "" {
		qType = model.QueryUniversal
	}

	res := &model.SearchResult{
		Query:     query,
		Type:      qType,
		Timestamp: started.UTC(),
		Meta: map[string]interface{}{
			"sources_total": len(s.sources),
		},
	}

	if len(s.sources) == 0 {
		res.DurationMS = time.Since(started).Milliseconds()
		return res
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, src := range s.sources {
		src := src
		wg.Add(1)
		go func() {
			defer wg.Done()
			hits, err := src.Search(ctx, query, qType)
			if err != nil || len(hits) == 0 {
				return
			}
			mu.Lock()
			res.Hits = append(res.Hits, hits...)
			mu.Unlock()
		}()
	}

	wg.Wait()
	res.DurationMS = time.Since(started).Milliseconds()
	res.Meta["hits_total"] = len(res.Hits)
	return res
}
