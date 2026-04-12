package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

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
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		TelegramToken: os.Getenv("TELEGRAM_TOKEN"),
		AppHash:       os.Getenv("APP_HASH"),
		PhoneNumber:   os.Getenv("PHONE_NUMBER"),
		RedisAddr:     defaultString(os.Getenv("REDIS_ADDR"), "localhost:6379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		CacheTTL:      defaultInt(os.Getenv("CACHE_TTL"), 3600),
		ProxyURL:      strings.TrimSpace(os.Getenv("PROXY_URL")),
		PoolSize:      defaultInt(os.Getenv("MTPROTO_POOL_SIZE"), 3),
		BotVersion:    defaultString(os.Getenv("BOT_VERSION"), "1.0.0"),
		HIBPAPIKey:    strings.TrimSpace(os.Getenv("HIBP_API_KEY")),
		LeakCheckKey:  strings.TrimSpace(os.Getenv("LEAKCHECK_API_KEY")),
	}
	cfg.AppID, _ = strconv.Atoi(strings.TrimSpace(os.Getenv("APP_ID")))

	for _, id := range strings.Split(os.Getenv("ADMIN_IDS"), ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		parsed, err := strconv.ParseInt(id, 10, 64)
		if err == nil {
			cfg.AdminIDs = append(cfg.AdminIDs, parsed)
		}
	}

	if cfg.TelegramToken == "" {
		return nil, fmt.Errorf("TELEGRAM_TOKEN is required")
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
