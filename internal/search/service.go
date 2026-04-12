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
	"golang.org/x/sync/semaphore"
	"osint.bot/internal/model"
	"osint.bot/internal/storage"
)

type Source interface {
	Name() string
	Search(ctx context.Context, query string, qType model.QueryType) ([]model.SearchHit, error)
}

type Service struct {
	sources            []Source
	store              storage.KVStore
	cacheTTL           time.Duration
	cachePref          string
	sourceTimeout      time.Duration
	sourceRetryCount   int
	sourceRetryBackoff time.Duration
	sourceMaxParallel  int64
}

type Options struct {
	CacheTTL           time.Duration
	SourceTimeout      time.Duration
	SourceRetryCount   int
	SourceRetryBackoff time.Duration
	SourceMaxParallel  int64
	CachePrefix        string
}

func NewService(store storage.KVStore, opts Options, sources ...Source) *Service {
	if opts.CacheTTL <= 0 {
		opts.CacheTTL = time.Hour
	}
	if opts.SourceTimeout <= 0 {
		opts.SourceTimeout = 12 * time.Second
	}
	if opts.SourceRetryBackoff <= 0 {
		opts.SourceRetryBackoff = 350 * time.Millisecond
	}
	if opts.SourceMaxParallel <= 0 {
		opts.SourceMaxParallel = 6
	}
	if opts.CachePrefix == "" {
		opts.CachePrefix = "osint"
	}
	if opts.SourceRetryCount < 0 {
		opts.SourceRetryCount = 0
	}

	return &Service{
		sources:            sources,
		store:              store,
		cacheTTL:           opts.CacheTTL,
		cachePref:          opts.CachePrefix,
		sourceTimeout:      opts.SourceTimeout,
		sourceRetryCount:   opts.SourceRetryCount,
		sourceRetryBackoff: opts.SourceRetryBackoff,
		sourceMaxParallel:  opts.SourceMaxParallel,
	}
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
	normalizedQuery := NormalizeQuery(query)
	qType := explicitType
	if qType == "" || qType == model.QueryUniversal {
		qType = DetectType(normalizedQuery)
	}
	if qType == "" {
		qType = model.QueryUniversal
	}

	res := &model.SearchResult{
		Query:     normalizedQuery,
		Type:      qType,
		Timestamp: started.UTC(),
		Meta: map[string]interface{}{
			"sources_total": len(s.sources),
		},
	}

	cacheKey := s.buildCacheKey(qType, normalizedQuery)
	if s.store != nil {
		var cached model.SearchResult
		if err := s.store.GetJSON(ctx, cacheKey, &cached); err == nil {
			cached.Meta = cloneMeta(cached.Meta)
			cached.Meta["cached"] = true
			cached.DurationMS = time.Since(started).Milliseconds()
			log.Printf("level=info msg=search_cache_hit key=%s", cacheKey)
			return &cached
		} else if !errors.Is(err, redis.Nil) {
			log.Printf("level=warn msg=search_cache_read_failed key=%s err=%v", cacheKey, err)
		}
	}

	if len(s.sources) == 0 {
		res.DurationMS = time.Since(started).Milliseconds()
		return res
	}

	if s.store != nil {
		ok, err := s.store.SetNX(ctx, s.inFlightKey(cacheKey), "1", s.sourceTimeout*2)
		if err != nil {
			log.Printf("level=warn msg=inflight_set_failed key=%s err=%v", cacheKey, err)
		} else if !ok {
			log.Printf("level=info msg=inflight_duplicate_query key=%s", cacheKey)
		}
		defer s.releaseInFlight(ctx, cacheKey)
	}

	var sourceErrors sync.Map
	var sourceHits sync.Map
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := semaphore.NewWeighted(s.sourceMaxParallel)

	for _, src := range s.sources {
		src := src
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if rec := recover(); rec != nil {
					msg := fmt.Sprintf("panic: %v", rec)
					sourceErrors.Store(src.Name(), msg)
					log.Printf("level=error msg=source_panic source=%s err=%s", src.Name(), msg)
				}
			}()

			if err := sem.Acquire(ctx, 1); err != nil {
				sourceErrors.Store(src.Name(), err.Error())
				return
			}
			defer sem.Release(1)

			hits, err := s.searchSourceWithRetry(ctx, src, normalizedQuery, qType)
			if err != nil {
				sourceErrors.Store(src.Name(), err.Error())
				return
			}
			sourceHits.Store(src.Name(), len(hits))
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
	res.Meta["source_hits"] = mapFromSyncMap(&sourceHits)
	res.Meta["source_errors"] = mapFromSyncMap(&sourceErrors)

	if s.store != nil {
		if err := s.store.SetJSON(ctx, cacheKey, res, s.cacheTTL); err != nil {
			log.Printf("level=warn msg=search_cache_write_failed key=%s err=%v", cacheKey, err)
		}
	}

	return res
}

func (s *Service) searchSourceWithRetry(ctx context.Context, src Source, query string, qType model.QueryType) ([]model.SearchHit, error) {
	var lastErr error
	attempts := s.sourceRetryCount + 1
	for attempt := 1; attempt <= attempts; attempt++ {
		sourceCtx, cancel := context.WithTimeout(ctx, s.sourceTimeout)
		hits, err := src.Search(sourceCtx, query, qType)
		cancel()
		if err == nil {
			log.Printf("level=info msg=source_done source=%s attempt=%d hits=%d", src.Name(), attempt, len(hits))
			return hits, nil
		}
		lastErr = err
		if attempt < attempts {
			backoff := s.sourceRetryBackoff * time.Duration(1<<(attempt-1))
			log.Printf("level=warn msg=source_retry source=%s attempt=%d err=%v backoff=%s", src.Name(), attempt, err, backoff)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
	}
	return nil, fmt.Errorf("source %s failed after retries: %w", src.Name(), lastErr)
}

func (s *Service) releaseInFlight(ctx context.Context, cacheKey string) {
	if s.store == nil {
		return
	}
	_ = s.store.Delete(ctx, s.inFlightKey(cacheKey))
}

func (s *Service) inFlightKey(cacheKey string) string {
	return fmt.Sprintf("%s:inflight", cacheKey)
}

func (s *Service) buildCacheKey(qType model.QueryType, query string) string {
	normalized := NormalizeQuery(query)
	return fmt.Sprintf("%s:%s:%s", s.cachePref, qType, normalized)
}

func NormalizeQuery(query string) string {
	normalized := strings.TrimSpace(strings.ToLower(query))
	normalized = strings.Join(strings.Fields(normalized), " ")

	rePhone := regexp.MustCompile(`[^\d+]`)
	if regexp.MustCompile(`^\+?[\d\s\-()]{7,}$`).MatchString(normalized) {
		normalized = rePhone.ReplaceAllString(normalized, "")
	}
	if strings.HasPrefix(normalized, "mailto:") {
		normalized = strings.TrimPrefix(normalized, "mailto:")
	}
	return normalized
}

func cloneMeta(in map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mapFromSyncMap(sm *sync.Map) map[string]interface{} {
	out := map[string]interface{}{}
	sm.Range(func(key, value any) bool {
		out[fmt.Sprintf("%v", key)] = value
		return true
	})
	return out
}
