package search

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"osint.bot/internal/model"
	"osint.bot/internal/storage"
)

type Source interface {
	Name() string
	Search(ctx context.Context, query string, qType model.QueryType) ([]model.SearchHit, error)
}

type Service struct {
	sources   []Source
	store     *storage.RedisStore
	cacheTTL  time.Duration
	cachePref string
}

func NewService(store *storage.RedisStore, cacheTTL time.Duration, sources ...Source) *Service {
	if cacheTTL <= 0 {
		cacheTTL = time.Hour
	}
	return &Service{sources: sources, store: store, cacheTTL: cacheTTL, cachePref: "osint"}
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

	cacheKey := s.buildCacheKey(qType, query)
	if s.store != nil {
		var cached model.SearchResult
		if err := s.store.GetJSON(ctx, cacheKey, &cached); err == nil {
			cached.Meta = cloneMeta(cached.Meta)
			cached.Meta["cached"] = true
			cached.DurationMS = time.Since(started).Milliseconds()
			log.Printf("search cache hit: key=%s", cacheKey)
			return &cached
		} else if !errors.Is(err, redis.Nil) {
			log.Printf("search cache read failed: key=%s err=%v", cacheKey, err)
		}
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
			defer func() {
				if rec := recover(); rec != nil {
					log.Printf("panic recovered in source %s: %v", src.Name(), rec)
				}
			}()
			log.Printf("source search started: %s query=%q type=%s", src.Name(), query, qType)
			hits, err := src.Search(ctx, query, qType)
			if err != nil {
				log.Printf("source search failed: %s err=%v", src.Name(), err)
				return
			}
			if len(hits) == 0 {
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
	res.Meta["cached"] = false

	if s.store != nil {
		if err := s.store.SetJSON(ctx, cacheKey, res, s.cacheTTL); err != nil {
			log.Printf("search cache write failed: key=%s err=%v", cacheKey, err)
		} else {
			log.Printf("search cache write: key=%s ttl=%s", cacheKey, s.cacheTTL)
		}
	}

	return res
}

func (s *Service) buildCacheKey(qType model.QueryType, query string) string {
	normalized := strings.ToLower(strings.TrimSpace(query))
	normalized = strings.Join(strings.Fields(normalized), " ")
	return fmt.Sprintf("%s:%s:%s", s.cachePref, qType, normalized)
}

func cloneMeta(in map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
