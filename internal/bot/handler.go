package bot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"golang.org/x/sync/semaphore"
	"osint.bot/internal/config"
	"osint.bot/internal/export"
	"osint.bot/internal/formatter"
	"osint.bot/internal/maps"
	"osint.bot/internal/model"
	"osint.bot/internal/mtproto"
	"osint.bot/internal/search"
	"osint.bot/internal/storage"
)

type Handler struct {
	cfg      *config.Config
	bot      *tgbotapi.BotAPI
	store    *storage.RedisStore
	state    *UserStateStore
	mtp      *mtproto.Pool
	search   *search.Service
	geocoder *maps.Geocoder

	wg         sync.WaitGroup
	semMu      sync.Mutex
	searchSems map[int64]*semaphore.Weighted
}

func NewHandler(cfg *config.Config, botAPI *tgbotapi.BotAPI, redis *storage.RedisStore, mtp *mtproto.Pool) *Handler {
	httpClient := search.NewHTTPClient(cfg.ProxyURL)
	searchSvc := search.NewService(
		redis,
		time.Hour,
		search.NewTelegramMTSource(mtp),
		search.NewLeakCheckSource(httpClient, cfg.LeakCheckKey),
		search.NewHIBPSource(httpClient, cfg.HIBPAPIKey),
		search.NewPsbdmpSource(httpClient),
		search.NewScyllaSource(httpClient),
		search.NewHoleheSource(httpClient),
	)
	return &Handler{
		cfg:        cfg,
		bot:        botAPI,
		store:      redis,
		state:      NewUserStateStore(),
		mtp:        mtp,
		search:     searchSvc,
		geocoder:   maps.NewGeocoder(),
		searchSems: make(map[int64]*semaphore.Weighted),
	}
}

func (h *Handler) ProcessUpdate(ctx context.Context, upd tgbotapi.Update) {
	if upd.CallbackQuery != nil {
		h.wg.Add(1)
		go func() {
			defer h.wg.Done()
			defer func() {
				if rec := recover(); rec != nil {
					log.Printf("panic recovered in callback handler: %v", rec)
				}
			}()
			h.handleCallback(ctx, upd.CallbackQuery)
		}()
		return
	}
	if upd.Message == nil {
		return
	}

	h.wg.Add(1)
	go func(msg *tgbotapi.Message) {
		defer h.wg.Done()
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic recovered in message handler: %v", rec)
			}
		}()
		h.handleMessage(ctx, msg)
	}(upd.Message)
}

func (h *Handler) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
	if msg == nil || msg.From == nil {
		return
	}
	if !h.isAdmin(msg.From.ID) {
		h.reply(msg.Chat.ID, "⛔ Доступ запрещен")
		return
	}

	if msg.IsCommand() {
		h.handleCommand(ctx, msg)
		return
	}

	if state, ok := h.state.PopState(msg.From.ID); ok {
		h.handleStatefulInput(ctx, msg.Chat.ID, msg.From.ID, state, strings.TrimSpace(msg.Text))
		return
	}

	h.runSearch(ctx, msg.Chat.ID, msg.From.ID, strings.TrimSpace(msg.Text), model.QueryUniversal)
}

func (h *Handler) handleCommand(ctx context.Context, msg *tgbotapi.Message) {
	if msg == nil || msg.From == nil {
		return
	}
	switch msg.Command() {
	case "start", "help":
		h.reply(msg.Chat.ID, "OSINT bot online. Команды: /menu /search /stats /status /export /clear")
		h.sendMainMenu(msg.Chat.ID)
	case "menu":
		h.sendMainMenu(msg.Chat.ID)
	case "search":
		h.state.SetState(msg.From.ID, "await:universal")
		h.reply(msg.Chat.ID, "Введите запрос (телефон/email/FIO/address/car).")
	case "stats":
		h.showStats(ctx, msg.Chat.ID)
	case "status":
		h.showStatus(ctx, msg.Chat.ID)
	case "clear":
		h.state.ClearCache(msg.From.ID)
		h.reply(msg.Chat.ID, "🗑 Кеш очищен")
	case "export":
		h.sendExportMenu(msg.Chat.ID)
	default:
		h.reply(msg.Chat.ID, "Неизвестная команда")
	}
}

func (h *Handler) handleCallback(ctx context.Context, cb *tgbotapi.CallbackQuery) {
	if cb == nil || cb.Message == nil {
		return
	}
	if !h.isAdmin(cb.From.ID) {
		h.answerCallback(cb.ID, "Нет доступа")
		return
	}

	chatID := cb.Message.Chat.ID
	userID := cb.From.ID
	data := cb.Data

	switch data {
	case "search:universal", "search:phone", "search:email", "search:fio", "search:address", "search:car", "map:lookup":
		state := strings.Replace(data, "search:", "await:", 1)
		if data == "map:lookup" {
			state = "await:map"
		}
		h.state.SetState(userID, state)
		h.reply(chatID, "Ок, отправьте запрос.")
		h.answerCallback(cb.ID, "Режим установлен")
	case "menu:search":
		h.sendSearchTypeMenu(chatID)
		h.answerCallback(cb.ID, "Открыт поиск")
	case "menu:export":
		h.sendExportMenu(chatID)
		h.answerCallback(cb.ID, "Открыт экспорт")
	case "export:json":
		h.sendExport(chatID, userID, "json")
		h.answerCallback(cb.ID, "JSON отправлен")
	case "export:csv":
		h.sendExport(chatID, userID, "csv")
		h.answerCallback(cb.ID, "CSV отправлен")
	default:
		h.answerCallback(cb.ID, "Неизвестная кнопка")
	}

	_ = ctx
}

func (h *Handler) handleStatefulInput(ctx context.Context, chatID, userID int64, state, query string) {
	switch state {
	case "await:phone":
		h.runSearch(ctx, chatID, userID, query, model.QueryPhone)
	case "await:email":
		h.runSearch(ctx, chatID, userID, query, model.QueryEmail)
	case "await:fio":
		h.runSearch(ctx, chatID, userID, query, model.QueryFIO)
	case "await:address":
		h.runSearch(ctx, chatID, userID, query, model.QueryAddress)
	case "await:car":
		h.runSearch(ctx, chatID, userID, query, model.QueryCar)
	case "await:universal":
		h.runSearch(ctx, chatID, userID, query, model.QueryUniversal)
	case "await:map":
		h.handleMapLookup(ctx, chatID, query)
	default:
		h.reply(chatID, "Состояние устарело, выберите /menu")
	}
}

func (h *Handler) runSearch(ctx context.Context, chatID, userID int64, query string, qType model.QueryType) {
	if strings.TrimSpace(query) == "" {
		h.reply(chatID, "Пустой запрос")
		return
	}
	sem := h.userSearchSemaphore(userID)
	if err := sem.Acquire(ctx, 1); err != nil {
		h.reply(chatID, "Поиск отменен")
		return
	}
	defer sem.Release(1)

	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("panic recovered in search for user=%d: %v", userID, rec)
			h.reply(chatID, "Произошла внутренняя ошибка поиска")
		}
	}()

	log.Printf("search request: user=%d query=%q type=%s", userID, query, qType)
	res := h.search.Search(ctx, query, qType)
	h.state.SetCache(userID, res)
	chunks := formatter.RenderResult(res)
	for _, chunk := range chunks {
		h.reply(chatID, chunk)
	}
}

func (h *Handler) showStats(ctx context.Context, chatID int64) {
	if h.store == nil {
		h.reply(chatID, "⚠️ Redis отключен")
		return
	}

	keys, err := h.store.DBSize(ctx)
	if err != nil {
		h.reply(chatID, fmt.Sprintf("Ошибка получения статистики: %v", err))
		return
	}

	msg := fmt.Sprintf("📊 Статистика\nRedis keys: %d\nMTProto ready: %d", keys, h.mtp.ReadyCount())
	h.reply(chatID, msg)
}

func (h *Handler) showStatus(ctx context.Context, chatID int64) {
	redisStatus := "disabled"
	keys := int64(0)
	if h.store != nil {
		if err := h.store.Ping(ctx); err != nil {
			redisStatus = "error: " + err.Error()
		} else {
			redisStatus = "ok"
			if c, err := h.store.DBSize(ctx); err == nil {
				keys = c
			}
		}
	}
	msg := fmt.Sprintf("🩺 Status\nVersion: %s\nMTProto sessions: %d\nRedis: %s\nRedis keys: %d", h.cfg.BotVersion, h.mtp.ReadyCount(), redisStatus, keys)
	h.reply(chatID, msg)
}

func (h *Handler) handleMapLookup(ctx context.Context, chatID int64, query string) {
	geo, err := h.geocoder.Lookup(ctx, query)
	if err != nil {
		h.reply(chatID, "Карта: адрес не найден")
		return
	}
	msg := fmt.Sprintf("📍 %s\nКоординаты: %.6f, %.6f\n%s", geo.DisplayName, geo.Lat, geo.Lon, maps.BuildMapLink(geo.Lat, geo.Lon))
	h.reply(chatID, msg)
}

func (h *Handler) sendMainMenu(chatID int64) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔎 Поиск", "menu:search"),
			tgbotapi.NewInlineKeyboardButtonData("📤 Экспорт", "menu:export"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🗺 Карта", "map:lookup"),
		),
	)
	msg := tgbotapi.NewMessage(chatID, "Главное меню")
	msg.ReplyMarkup = kb
	if _, err := h.bot.Send(msg); err != nil {
		log.Printf("send menu failed: %v", err)
	}
}

func (h *Handler) sendSearchTypeMenu(chatID int64) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Универсальный", "search:universal"),
			tgbotapi.NewInlineKeyboardButtonData("Телефон", "search:phone"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Email", "search:email"),
			tgbotapi.NewInlineKeyboardButtonData("ФИО", "search:fio"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Адрес", "search:address"),
			tgbotapi.NewInlineKeyboardButtonData("Авто", "search:car"),
		),
	)
	msg := tgbotapi.NewMessage(chatID, "Выберите тип поиска")
	msg.ReplyMarkup = kb
	if _, err := h.bot.Send(msg); err != nil {
		log.Printf("send search menu failed: %v", err)
	}
}

func (h *Handler) sendExportMenu(chatID int64) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("JSON", "export:json"),
			tgbotapi.NewInlineKeyboardButtonData("CSV", "export:csv"),
		),
	)
	msg := tgbotapi.NewMessage(chatID, "Экспорт последнего результата")
	msg.ReplyMarkup = kb
	if _, err := h.bot.Send(msg); err != nil {
		log.Printf("send export menu failed: %v", err)
	}
}

func (h *Handler) sendExport(chatID, userID int64, format string) {
	res, ok := h.state.GetCache(userID)
	if !ok || res == nil {
		h.reply(chatID, "Нет данных для экспорта")
		return
	}

	var (
		data []byte
		err  error
		name string
	)
	switch format {
	case "json":
		data, err = export.AsJSON(res)
		name = "osint_result.json"
	case "csv":
		data, err = export.AsCSV(res)
		name = "osint_result.csv"
	default:
		h.reply(chatID, "Неподдерживаемый формат")
		return
	}
	if err != nil {
		h.reply(chatID, fmt.Sprintf("Ошибка экспорта: %v", err))
		return
	}

	file := tgbotapi.FileBytes{Name: name, Bytes: data}
	doc := tgbotapi.NewDocument(chatID, file)
	doc.Caption = "Экспорт готов"
	if _, err := h.bot.Send(doc); err != nil {
		log.Printf("send export failed: %v", err)
		return
	}
}

func (h *Handler) answerCallback(callbackID, text string) {
	cfg := tgbotapi.NewCallback(callbackID, text)
	if _, err := h.bot.Request(cfg); err != nil {
		log.Printf("callback answer failed: %v", err)
	}
}

func (h *Handler) reply(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	if len(text) > formatter.TelegramChunkSize {
		msg.Text = text[:formatter.TelegramChunkSize]
	}
	if _, err := h.bot.Send(msg); err != nil {
		log.Printf("send reply failed: %v", err)
	}
}

func (h *Handler) isAdmin(userID int64) bool {
	for _, id := range h.cfg.AdminIDs {
		if id == userID {
			return true
		}
	}
	return len(h.cfg.AdminIDs) == 0
}

func (h *Handler) userSearchSemaphore(userID int64) *semaphore.Weighted {
	h.semMu.Lock()
	defer h.semMu.Unlock()
	if s, ok := h.searchSems[userID]; ok {
		return s
	}
	s := semaphore.NewWeighted(5)
	h.searchSems[userID] = s
	return s
}

func (h *Handler) Wait(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		log.Printf("handler wait timeout reached: %s", timeout)
	}
}
