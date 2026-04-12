package app

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"osint.bot/internal/bot"
	"osint.bot/internal/config"
	"osint.bot/internal/mtproto"
	"osint.bot/internal/storage"
)

func Run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	botAPI, err := tgbotapi.NewBotAPI(cfg.TelegramToken)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	redisStore := storage.NewRedisStore(cfg.RedisAddr, cfg.RedisPassword)
	if err := redisStore.Ping(ctx); err != nil {
		log.Printf("redis unavailable, cache disabled: %v", err)
		redisStore = nil
	}

	mtp := mtproto.NewPool()
	mtp.Start(ctx, cfg.AppID, cfg.AppHash, cfg.PoolSize)

	handler := bot.NewHandler(cfg, botAPI, redisStore, mtp)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := botAPI.GetUpdatesChan(u)

	log.Printf("bot authorized as @%s", botAPI.Self.UserName)
	for {
		select {
		case <-ctx.Done():
			log.Printf("shutdown signal received")
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			handler.Wait(30 * time.Second)
			mtp.Close()
			if redisStore != nil {
				if err := redisStore.Close(); err != nil {
					log.Printf("redis close failed: %v", err)
				}
			}
			_ = shutdownCtx
			return nil
		case upd, ok := <-updates:
			if !ok {
				return nil
			}
			handler.ProcessUpdate(ctx, upd)
		}
	}
}

func Main() {
	if err := Run(); err != nil {
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
}
