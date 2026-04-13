package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	TelegramToken string
	AppID         int
	AppHash       string
	PhoneNumber   string
	AdminIDs      []int64
	RedisAddr     string
	RedisPassword string
	CacheTTL      int
	ProxyURL      string
	PoolSize      int
	BotVersion    string
	HIBPAPIKey    string
	LeakCheckKey  string

	SearchTimeout      time.Duration
	SourceTimeout      time.Duration
	SourceRetryCount   int
	SourceRetryBackoff time.Duration
	SourceMaxParallel  int64
	UserRateLimit      time.Duration
	UpdateMaxParallel  int64
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		TelegramToken: os.Getenv("TELEGRAM_TOKEN"),
		AppHash:       strings.TrimSpace(os.Getenv("APP_HASH")),
		PhoneNumber:   strings.TrimSpace(os.Getenv("PHONE_NUMBER")),
		RedisAddr:     defaultString(os.Getenv("REDIS_ADDR"), "localhost:6379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		CacheTTL:      defaultInt(os.Getenv("CACHE_TTL"), 3600),
		ProxyURL:      strings.TrimSpace(os.Getenv("PROXY_URL")),
		PoolSize:      defaultInt(os.Getenv("MTPROTO_POOL_SIZE"), 3),
		BotVersion:    defaultString(os.Getenv("BOT_VERSION"), "1.0.0"),
		HIBPAPIKey:    strings.TrimSpace(os.Getenv("HIBP_API_KEY")),
		LeakCheckKey:  strings.TrimSpace(os.Getenv("LEAKCHECK_API_KEY")),

		SearchTimeout:      time.Duration(defaultInt(os.Getenv("SEARCH_TIMEOUT_SEC"), 45)) * time.Second,
		SourceTimeout:      time.Duration(defaultInt(os.Getenv("SOURCE_TIMEOUT_SEC"), 12)) * time.Second,
		SourceRetryCount:   defaultInt(os.Getenv("SOURCE_RETRY_COUNT"), 2),
		SourceRetryBackoff: time.Duration(defaultInt(os.Getenv("SOURCE_RETRY_BACKOFF_MS"), 350)) * time.Millisecond,
		SourceMaxParallel:  int64(defaultInt(os.Getenv("SOURCE_MAX_PARALLEL"), 6)),
		UserRateLimit:      time.Duration(defaultInt(os.Getenv("USER_RATE_LIMIT_MS"), 1200)) * time.Millisecond,
		UpdateMaxParallel:  int64(defaultInt(os.Getenv("UPDATE_MAX_PARALLEL"), 64)),
	}

	appIDRaw := strings.TrimSpace(os.Getenv("APP_ID"))
	if appIDRaw != "" {
		parsed, err := strconv.Atoi(appIDRaw)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("APP_ID must be a positive integer")
		}
		cfg.AppID = parsed
	}

	for _, id := range strings.Split(os.Getenv("ADMIN_IDS"), ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		parsed, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid ADMIN_IDS value %q: %w", id, err)
		}
		cfg.AdminIDs = append(cfg.AdminIDs, parsed)
	}

	if cfg.TelegramToken == "" {
		return nil, fmt.Errorf("TELEGRAM_TOKEN is required")
	}
	if cfg.CacheTTL <= 0 {
		return nil, fmt.Errorf("CACHE_TTL must be > 0")
	}
	if cfg.PoolSize < 0 {
		return nil, fmt.Errorf("MTPROTO_POOL_SIZE must be >= 0")
	}
	if cfg.SourceRetryCount < 0 {
		return nil, fmt.Errorf("SOURCE_RETRY_COUNT must be >= 0")
	}
	if cfg.SourceMaxParallel <= 0 {
		return nil, fmt.Errorf("SOURCE_MAX_PARALLEL must be > 0")
	}
	if cfg.UpdateMaxParallel <= 0 {
		return nil, fmt.Errorf("UPDATE_MAX_PARALLEL must be > 0")
	}
	if cfg.AppID > 0 && cfg.AppHash == "" {
		return nil, fmt.Errorf("APP_HASH is required when APP_ID is set")
	}
	if len(cfg.AdminIDs) == 0 {
		return nil, fmt.Errorf("ADMIN_IDS is required and must contain at least one Telegram user ID")
	}

	return cfg, nil
}

func defaultInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return parsed
}

func defaultString(raw, fallback string) string {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	return strings.TrimSpace(raw)
}
