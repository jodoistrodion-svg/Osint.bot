package bot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"osint.bot/internal/config"
	"osint.bot/internal/model"
	"osint.bot/internal/mtproto"
	"osint.bot/internal/storage"
)

type Handler struct {
	cfg   *config.Config
	bot   *tgbotapi.BotAPI
	store *storage.RedisStore
	state *UserStateStore
	mtp   *mtproto.Pool
}

func NewHandler(cfg *config.Config, botAPI *tgbotapi.BotAPI, redis *storage.RedisStore, mtp *mtproto.Pool) *Handler {
	return &Handler{
		cfg:   cfg,
		bot:   botAPI,
		store: redis,
		state: NewUserStateStore(),
		mtp:   mtp,
	}
}

func (h *Handler) ProcessUpdate(ctx context.Context, upd tgbotapi.Update) {
	if upd.Message == nil {
		return
	}

	msg := upd.Message
	if !h.isAdmin(msg.From.ID) {
		h.reply(msg.Chat.ID, "⛔ Доступ запрещен")
		return
	}

	if msg.IsCommand() {
		h.handleCommand(ctx, msg)
		return
	}

	if state, ok := h.state.PopState(msg.From.ID); ok {
		h.handleStatefulInput(msg.Chat.ID, msg.From.ID, state, strings.TrimSpace(msg.Text))
	}
}

func (h *Handler) handleCommand(ctx context.Context, msg *tgbotapi.Message) {
	switch msg.Command() {
	case "start", "help":
		h.reply(msg.Chat.ID, "OSINT bot online. Команды: /stats /clear")
	case "stats":
		h.showStats(ctx, msg.Chat.ID)
	case "clear":
		h.state.ClearCache(msg.From.ID)
		h.reply(msg.Chat.ID, "🗑 Кеш очищен")
	default:
		h.reply(msg.Chat.ID, "Неизвестная команда")
	}
}

func (h *Handler) handleStatefulInput(chatID, userID int64, state, query string) {
	result := &model.SearchResult{
		Query:     query,
		Type:      state,
		Timestamp: time.Now().UTC(),
		Sources:   map[string]interface{}{"status": "queued"},
	}
	h.state.SetCache(userID, result)
	h.reply(chatID, fmt.Sprintf("Принято: %s", query))
}

func (h *Handler) showStats(ctx context.Context, chatID int64) {
	if h.store == nil {
		h.reply(chatID, "⚠️ Redis отключен")
		return
	}

	keys, err := h.store.DBSize(ctx) // int64, compile-safe
	if err != nil {
		h.reply(chatID, fmt.Sprintf("Ошибка получения статистики: %v", err))
		return
	}

	msg := fmt.Sprintf("📊 Статистика\nRedis keys: %d\nMTProto ready: %d", keys, h.mtp.ReadyCount())
	h.reply(chatID, msg)
}

func (h *Handler) reply(chatID int64, text string) {
	if _, err := h.bot.Send(tgbotapi.NewMessage(chatID, text)); err != nil {
		log.Printf("send message failed: %v", err)
	}
}

func (h *Handler) isAdmin(userID int64) bool {
	for _, adminID := range h.cfg.AdminIDs {
		if adminID == userID {
			return true
		}
	}
	return false
}
