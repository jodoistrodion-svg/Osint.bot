// ============================================
// OSINT TELEGRAM BOT - ПОЛНЫЙ КОД В ОДНОМ ФАЙЛЕ
// ============================================
//
// ИНСТРУКЦИЯ ПО ЗАПУСКУ:
// 1. Установи Go 1.21+
// 2. Установи Redis (sudo apt install redis)
// 3. Создай бота через @BotFather, получи токен
// 4. Зайди на https://my.telegram.org, создай приложение, получи APP_ID и APP_HASH
// 5. Заполни .env файл (образец ниже)
// 6. go mod init osint-bot && go mod tidy
// 7. go run main.go
//
// .env файл (создай рядом с main.go):
/*
TELEGRAM_TOKEN=ваш_токен_бота
APP_ID=ваш_app_id
APP_HASH=ваш_app_hash
PHONE_NUMBER=+79001234567
ADMIN_IDS=123456789
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=
CACHE_TTL=3600
*/
// ============================================

package main

import (
    "context"
    "crypto/md5"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net/http"
    "net/url"
    "os"
    "os/signal"
    "regexp"
    "strconv"
    "strings"
    "sync"
    "syscall"
    "time"

    tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
    "github.com/gotd/td/telegram"
    "github.com/gotd/td/telegram/auth"
    "github.com/gotd/td/telegram/message"
    "github.com/gotd/td/telegram/updates"
    "github.com/gotd/td/tg"
    "github.com/gotd/td/session"
    "github.com/joho/godotenv"
    "github.com/redis/go-redis/v9"
    "golang.org/x/net/proxy"
)

// ============================================
// КОНФИГУРАЦИЯ (ВСЕ НАСТРОЙКИ СВЕРХУ)
// ============================================

var (
    TELEGRAM_TOKEN  string
    APP_ID          int
    APP_HASH        string
    PHONE_NUMBER    string
    ADMIN_IDS       []int64
    REDIS_ADDR      string
    REDIS_PASSWORD  string
    CACHE_TTL       int
    
    // Бесплатные API ключи (получить за 5 минут)
    GOOGLE_API_KEY      = ""  // https://developers.google.com/maps (бесплатно 200$/мес)
    NUMVERIFY_API_KEY   = ""  // https://numverify.com (бесплатно 250 запросов)
    ABSTRACT_API_KEY    = ""  // https://abstractapi.com (бесплатно 1000 запросов)
    HUNTER_API_KEY      = ""  // https://hunter.io (бесплатно 25 запросов/мес)
    CLEARBIT_API_KEY    = ""  // https://clearbit.com (бесплатно 50 запросов/мес)
)

// ============================================
// БЕСПЛАТНЫЕ ИСТОЧНИКИ ДАННЫХ ДЛЯ ПАРСИНГА
// ============================================

var FREE_LEAK_SOURCES = []string{
    "https://psbdmp.cc/api/search?q=",           // Pastebin дампы
    "https://scylla.so/api/search?q=",           // База утечек
    "https://leakcheck.io/api/public?check=",    // LeakCheck публичный
    "https://leakix.net/search?q=",              // LeakIX
    "https://haveibeenpwned.com/api/v3/breachedaccount/", // HIBP
    "https://emailrep.io/",                       // Email Reputation
    "https://api.proxynova.com/comb?query=",      // ProxyNova Combolist
    "https://holehe.deno.dev/",                   // Holehe email checker
}

var FREE_PHONE_SOURCES = []string{
    "https://htmlweb.ru/geo/api.php?json&telcod=",
    "https://api.apilayer.com/number_verification/validate?number=",
    "https://phonevalidation.abstractapi.com/v1/?api_key=" + ABSTRACT_API_KEY + "&phone=",
}

var FREE_EMAIL_SOURCES = []string{
    "https://api.hunter.io/v2/email-verifier?email=",
    "https://emailverification.whoisxmlapi.com/api/v1?apiKey=&emailAddress=",
}

var FREE_USERNAME_SOURCES = []string{
    "https://api.github.com/users/",
    "https://api.twitter.com/2/users/by/username/",
    "https://www.instagram.com/",
    "https://t.me/",
    "https://vk.com/",
    "https://ok.ru/",
}

var FREE_PEOPLE_SEARCH = []string{
    "https://api.nationalize.io/?name=",
    "https://api.genderize.io/?name=",
    "https://api.agify.io/?name=",
}

// ============================================
// СТРУКТУРЫ ДАННЫХ
// ============================================

type SearchResult struct {
    Query       string                 `json:"query"`
    Type        string                 `json:"type"`
    Phone       *PhoneInfo             `json:"phone,omitempty"`
    Email       *EmailInfo             `json:"email,omitempty"`
    Leaks       []LeakInfo             `json:"leaks,omitempty"`
    Social      *SocialInfo            `json:"social,omitempty"`
    Telegram    *TelegramInfo          `json:"telegram,omitempty"`
    Geo         *GeoInfo               `json:"geo,omitempty"`
    RelatedPeople []PersonInfo         `json:"related_people,omitempty"`
    Timestamp   int64                  `json:"timestamp"`
    Sources     map[string]interface{} `json:"sources"`
}

type PhoneInfo struct {
    Number      string `json:"number"`
    Valid       bool   `json:"valid"`
    Country     string `json:"country"`
    Location    string `json:"location"`
    Carrier     string `json:"carrier"`
    LineType    string `json:"line_type"`
}

type EmailInfo struct {
    Email       string   `json:"email"`
    Valid       bool     `json:"valid"`
    Disposable  bool     `json:"disposable"`
    Domain      string   `json:"domain"`
    Provider    string   `json:"provider"`
    Breaches    []string `json:"breaches"`
    SocialLinks []string `json:"social_links"`
}

type LeakInfo struct {
    Source      string `json:"source"`
    Database    string `json:"database"`
    Date        string `json:"date"`
    Email       string `json:"email"`
    Password    string `json:"password,omitempty"`
    Hash        string `json:"hash,omitempty"`
    Name        string `json:"name,omitempty"`
    Phone       string `json:"phone,omitempty"`
    Address     string `json:"address,omitempty"`
    IP          string `json:"ip,omitempty"`
}

type SocialInfo struct {
    VK          *VKProfile      `json:"vk,omitempty"`
    Instagram   *InstProfile    `json:"instagram,omitempty"`
    Facebook    *FBProfile      `json:"facebook,omitempty"`
    Twitter     *TWProfile      `json:"twitter,omitempty"`
    GitHub      *GHProfile      `json:"github,omitempty"`
    LinkedIn    *LIProfile      `json:"linkedin,omitempty"`
    Ok          *OKProfile      `json:"ok,omitempty"`
    TikTok      *TTProfile      `json:"tiktok,omitempty"`
    Snapchat    *SCProfile      `json:"snapchat,omitempty"`
    Discord     *DCProfile      `json:"discord,omitempty"`
    Steam       *SteamProfile   `json:"steam,omitempty"`
    Spotify     *SPProfile      `json:"spotify,omitempty"`
}

type VKProfile struct {
    ID          int    `json:"id"`
    FirstName   string `json:"first_name"`
    LastName    string `json:"last_name"`
    PhotoURL    string `json:"photo_url"`
    City        string `json:"city"`
    MobilePhone string `json:"mobile_phone,omitempty"`
    HomePhone   string `json:"home_phone,omitempty"`
    Site        string `json:"site"`
    Status      string `json:"status"`
    LastSeen    int64  `json:"last_seen"`
    Friends     int    `json:"friends_count"`
    Followers   int    `json:"followers_count"`
    Groups      []int  `json:"groups,omitempty"`
}

type InstProfile struct {
    ID          string `json:"id"`
    Username    string `json:"username"`
    FullName    string `json:"full_name"`
    Bio         string `json:"bio"`
    Followers   int    `json:"followers"`
    Following   int    `json:"following"`
    Posts       int    `json:"posts"`
    IsPrivate   bool   `json:"is_private"`
    IsBusiness  bool   `json:"is_business"`
    Email       string `json:"email,omitempty"`
    Phone       string `json:"phone,omitempty"`
}

type FBProfile struct {
    ID          string `json:"id"`
    Name        string `json:"name"`
    Link        string `json:"link"`
}

type TWProfile struct {
    ID          string `json:"id"`
    Username    string `json:"username"`
    Name        string `json:"name"`
    Description string `json:"description"`
    Followers   int    `json:"followers"`
    Following   int    `json:"following"`
    Location    string `json:"location"`
    Website     string `json:"website"`
}

type GHProfile struct {
    ID          int    `json:"id"`
    Login       string `json:"login"`
    Name        string `json:"name"`
    Email       string `json:"email"`
    Company     string `json:"company"`
    Blog        string `json:"blog"`
    Location    string `json:"location"`
    Bio         string `json:"bio"`
    Twitter     string `json:"twitter_username"`
    Followers   int    `json:"followers"`
    Following   int    `json:"following"`
    Repos       int    `json:"public_repos"`
}

type LIProfile struct {
    ID          string `json:"id"`
    Name        string `json:"name"`
    Headline    string `json:"headline"`
    Location    string `json:"location"`
    Industry    string `json:"industry"`
}

type OKProfile struct {
    ID          string `json:"id"`
    Name        string `json:"name"`
    Age         int    `json:"age,omitempty"`
    Location    string `json:"location"`
}

type TTProfile struct {
    ID          string `json:"id"`
    Username    string `json:"username"`
    Nickname    string `json:"nickname"`
    Bio         string `json:"bio"`
    Followers   int    `json:"followers"`
    Following   int    `json:"following"`
    Likes       int    `json:"likes"`
}

type SCProfile struct {
    Username    string `json:"username"`
    DisplayName string `json:"display_name"`
    Snapcode    string `json:"snapcode"`
    Bitmoji     string `json:"bitmoji"`
}

type DCProfile struct {
    ID          string `json:"id"`
    Username    string `json:"username"`
    Discriminator string `json:"discriminator"`
    Avatar      string `json:"avatar"`
    PublicFlags int    `json:"public_flags"`
}

type SteamProfile struct {
    SteamID     string `json:"steamid"`
    PersonaName string `json:"personaname"`
    ProfileURL  string `json:"profileurl"`
    Avatar      string `json:"avatar"`
    RealName    string `json:"realname,omitempty"`
    Location    string `json:"location,omitempty"`
    CountryCode string `json:"loccountrycode,omitempty"`
}

type SPProfile struct {
    ID          string `json:"id"`
    DisplayName string `json:"display_name"`
    Email       string `json:"email,omitempty"`
    Country     string `json:"country"`
    Followers   int    `json:"followers"`
    Product     string `json:"product"`
    Images      []string `json:"images"`
}

type TelegramInfo struct {
    ID          int64  `json:"id"`
    Username    string `json:"username"`
    FirstName   string `json:"first_name"`
    LastName    string `json:"last_name"`
    Phone       string `json:"phone"`
    Premium     bool   `json:"premium"`
    Scam        bool   `json:"scam"`
    Fake        bool   `json:"fake"`
    Bot         bool   `json:"bot"`
    Verified    bool   `json:"verified"`
    Restricted  bool   `json:"restricted"`
    Photos      []string `json:"photos,omitempty"`
    Bio         string `json:"bio"`
    CommonGroups []string `json:"common_groups,omitempty"`
    LastSeen    int64  `json:"last_seen"`
}

type GeoInfo struct {
    Lat         float64 `json:"lat"`
    Lng         float64 `json:"lng"`
    Address     string  `json:"address"`
    City        string  `json:"city"`
    Region      string  `json:"region"`
    Country     string  `json:"country"`
    PostalCode  string  `json:"postal_code"`
    Timezone    string  `json:"timezone"`
    MapURL      string  `json:"map_url"`
    StreetView  string  `json:"street_view"`
    Photos      []string `json:"photos"`
}

type PersonInfo struct {
    Name        string `json:"name"`
    Relation    string `json:"relation"`
    Phone       string `json:"phone,omitempty"`
    Email       string `json:"email,omitempty"`
    Address     string `json:"address,omitempty"`
    SocialLinks []string `json:"social_links,omitempty"`
}

// ============================================
// ГЛОБАЛЬНЫЕ ПЕРЕМЕННЫЕ
// ============================================

var (
    bot           *tgbotapi.BotAPI
    rdb           *redis.Client
    ctx           = context.Background()
    userState     = make(map[int64]string)
    userCache     = make(map[int64]*SearchResult)
    
    // MTProto клиенты (пул для ротации)
    mtprotoClients []*telegram.Client
    mtprotoAPI     []*tg.Client
    mtprotoMutex   sync.RWMutex
    
    // HTTP клиент с прокси
    httpClient     *http.Client
)

// ============================================
// MAIN
// ============================================

func main() {
    // Загрузка .env
    godotenv.Load()
    
    TELEGRAM_TOKEN = os.Getenv("TELEGRAM_TOKEN")
    APP_ID, _ = strconv.Atoi(os.Getenv("APP_ID"))
    APP_HASH = os.Getenv("APP_HASH")
    PHONE_NUMBER = os.Getenv("PHONE_NUMBER")
    REDIS_ADDR = os.Getenv("REDIS_ADDR")
    REDIS_PASSWORD = os.Getenv("REDIS_PASSWORD")
    CACHE_TTL, _ = strconv.Atoi(os.Getenv("CACHE_TTL"))
    if CACHE_TTL == 0 { CACHE_TTL = 3600 }
    
    // Парсинг админских ID
    adminStr := os.Getenv("ADMIN_IDS")
    for _, id := range strings.Split(adminStr, ",") {
        if i, err := strconv.ParseInt(strings.TrimSpace(id), 10, 64); err == nil {
            ADMIN_IDS = append(ADMIN_IDS, i)
        }
    }
    
    // Проверка обязательных переменных
    if TELEGRAM_TOKEN == "" {
        log.Fatal("TELEGRAM_TOKEN не указан в .env")
    }
    
    // Инициализация Redis
    rdb = redis.NewClient(&redis.Options{
        Addr:     REDIS_ADDR,
        Password: REDIS_PASSWORD,
        DB:       0,
    })
    
    // Проверка соединения с Redis
    if err := rdb.Ping(ctx).Err(); err != nil {
        log.Printf("⚠️ Redis не доступен: %v (кеширование отключено)", err)
        rdb = nil
    }
    
    // Инициализация HTTP клиента с SOCKS5 прокси если есть
    httpClient = &http.Client{Timeout: 30 * time.Second}
    if proxyURL := os.Getenv("PROXY_URL"); proxyURL != "" {
        if strings.HasPrefix(proxyURL, "socks5://") {
            auth := &proxy.Auth{}
            dialer, err := proxy.SOCKS5("tcp", strings.TrimPrefix(proxyURL, "socks5://"), auth, proxy.Direct)
            if err == nil {
                httpClient.Transport = &http.Transport{Dial: dialer.Dial}
            }
        }
    }
    
    // Инициализация бота
    var err error
    bot, err = tgbotapi.NewBotAPI(TELEGRAM_TOKEN)
    if err != nil {
        log.Fatal(err)
    }
    
    log.Printf("[] RAGE mode - OSINT Bot v2.0")
    log.Printf("Авторизован как: %s", bot.Self.UserName)
    
    // Инициализация MTProto клиентов (если указаны APP_ID и APP_HASH)
    if APP_ID != 0 && APP_HASH != "" && PHONE_NUMBER != "" {
        go initMTProtoClients()
    }
    
    // Запуск слушателя обновлений
    u := tgbotapi.NewUpdate(0)
    u.Timeout = 60
    updates := bot.GetUpdatesChan(u)
    
    // Graceful shutdown
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    
    go func() {
        <-sigChan
        log.Println("Завершение работы...")
        os.Exit(0)
    }()
    
    // Основной цикл обработки
    for update := range updates {
        if update.CallbackQuery != nil {
            handleCallback(update.CallbackQuery)
        } else if update.Message != nil {
            handleMessage(update.Message)
        }
    }
}

// ============================================
// ИНИЦИАЛИЗАЦИЯ MTProto
// ============================================

func initMTProtoClients() {
    log.Printf("Инициализация MTProto клиентов...")
    
    // Создаем 3 клиента для параллельных запросов
    for i := 0; i < 3; i++ {
        client := telegram.NewClient(APP_ID, APP_HASH, telegram.Options{
            SessionStorage: &session.FileStorage{Path: fmt.Sprintf("session_%d.json", i)},
            UpdateHandler:  updates.New(updates.Config{}),
        })
        
        if err := client.Run(ctx, func(ctx context.Context) error {
            flow := auth.NewFlow(
                auth.Constant(PHONE_NUMBER, auth.CodeAuthenticatorFunc(
                    func(ctx context.Context, sentCode *tg.AuthSentCode) (string, error) {
                        fmt.Printf("Введите код для сессии %d: ", i)
                        var code string
                        fmt.Scanln(&code)
                        return code, nil
                    },
                )),
                auth.SendCodeOptions{},
            )
            
            if err := client.Auth().IfNecessary(ctx, flow); err != nil {
                log.Printf("Ошибка авторизации MTProto сессии %d: %v", i, err)
                return err
            }
            
            api := client.API()
            mtprotoMutex.Lock()
            mtprotoClients = append(mtprotoClients, client)
            mtprotoAPI = append(mtprotoAPI, api)
            mtprotoMutex.Unlock()
            
            log.Printf("MTProto сессия %d готова", i)
            
            <-ctx.Done()
            return nil
        }); err != nil {
            log.Printf("Ошибка запуска MTProto клиента %d: %v", i, err)
        }
    }
}

// ============================================
// ОБРАБОТЧИКИ СООБЩЕНИЙ
// ============================================

func handleMessage(msg *tgbotapi.Message) {
    userID := msg.From.ID
    chatID := msg.Chat.ID
    
    // Проверка доступа (только админы)
    if !isAdmin(userID) {
        bot.Send(tgbotapi.NewMessage(chatID, "⛔ Доступ запрещен"))
        return
    }
    
    if msg.IsCommand() {
        switch msg.Command() {
        case "start":
            sendMainMenu(chatID)
        case "help":
            sendHelp(chatID)
        case "stats":
            showStats(chatID)
        case "clear":
            clearUserCache(userID)
            bot.Send(tgbotapi.NewMessage(chatID, "🗑 Кеш очищен"))
        }
        return
    }
    
    state, exists := userState[userID]
    if !exists {
        return
    }
    
    query := strings.TrimSpace(msg.Text)
    
    switch state {
    case "awaiting_any":
        // Универсальный поиск - определяем тип автоматически
        go performUniversalSearch(chatID, userID, query)
        
    case "awaiting_phone":
        go performPhoneSearch(chatID, userID, query)
        
    case "awaiting_email":
        go performEmailSearch(chatID, userID, query)
        
    case "awaiting_username":
        go performUsernameSearch(chatID, userID, query)
        
    case "awaiting_telegram":
        go performTelegramSearch(chatID, userID, query)
        
    case "awaiting_fio":
        go performFIOSearch(chatID, userID, query)
        
    case "awaiting_address":
        go performAddressSearch(chatID, userID, query)
        
    case "awaiting_car":
        go performCarSearch(chatID, userID, query)
    }
    
    delete(userState, userID)
}

// ============================================
// УНИВЕРСАЛЬНЫЙ ПОИСК
// ============================================

func performUniversalSearch(chatID int64, userID int64, query string) {
    statusMsg := sendStatus(chatID, "🔍 Универсальный поиск...")
    
    result := &SearchResult{
        Query:     query,
        Type:      detectQueryType(query),
        Timestamp: time.Now().Unix(),
        Sources:   make(map[string]interface{}),
    }
    
    var wg sync.WaitGroup
    
    // Определяем тип и запускаем соответствующие поиски
    switch result.Type {
    case "phone":
        wg.Add(1)
        go func() {
            defer wg.Done()
            result.Phone = searchPhone(query)
        }()
        wg.Add(1)
        go func() {
            defer wg.Done()
            result.Leaks = searchLeaksByPhone(query)
        }()
        wg.Add(1)
        go func() {
            defer wg.Done()
            result.Telegram = searchTelegramByPhone(query)
        }()
        wg.Add(1)
        go func() {
            defer wg.Done()
            result.Geo = searchGeoByPhone(query)
        }()
        
    case "email":
        wg.Add(1)
        go func() {
            defer wg.Done()
            result.Email = searchEmail(query)
        }()
        wg.Add(1)
        go func() {
            defer wg.Done()
            result.Leaks = searchLeaksByEmail(query)
        }()
        wg.Add(1)
        go func() {
            defer wg.Done()
            result.Social = searchSocialByEmail(query)
        }()
        
    case "telegram":
        wg.Add(1)
        go func() {
            defer wg.Done()
            result.Telegram = searchTelegramByUsername(query)
        }()
        
    case "username":
        wg.Add(1)
        go func() {
            defer wg.Done()
            result.Social = searchSocialByUsername(query)
        }()
        
    case "fio":
        wg.Add(1)
        go func() {
            defer wg.Done()
            result.Leaks = searchLeaksByFIO(query)
        }()
        wg.Add(1)
        go func() {
            defer wg.Done()
            result.Social = searchSocialByFIO(query)
        }()
        wg.Add(1)
        go func() {
            defer wg.Done()
            result.RelatedPeople = searchRelatedPeople(query)
        }()
    }
    
    wg.Wait()
    
    // Кешируем результат
    cacheResult(query, result)
    userCache[userID] = result
    
    // Формируем и отправляем результат
    response := formatUniversalResult(result)
    edit := tgbotapi.NewEditMessageText(chatID, statusMsg.MessageID, response)
    edit.ParseMode = "HTML"
    edit.ReplyMarkup = resultKeyboard(query, result.Type)
    bot.Send(edit)
}

func detectQueryType(query string) string {
    query = strings.TrimSpace(query)
    
    // Проверка на номер телефона
    phoneRegex := regexp.MustCompile(`^\+?[0-9]{10,15}$`)
    if phoneRegex.MatchString(strings.ReplaceAll(query, " ", "")) {
        return "phone"
    }
    
    // Проверка на email
    emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
    if emailRegex.MatchString(query) {
        return "email"
    }
    
    // Проверка на Telegram username (@username или username)
    if strings.HasPrefix(query, "@") {
        return "telegram"
    }
    tgRegex := regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{4,31}$`)
    if tgRegex.MatchString(query) {
        return "telegram"
    }
    
    // Проверка на ФИО (три слова через пробел)
    parts := strings.Fields(query)
    if len(parts) >= 2 && len(parts) <= 3 {
        return "fio"
    }
    
    return "username"
}

// ============================================
// ПОИСК ПО ТЕЛЕФОНУ
// ============================================

func performPhoneSearch(chatID int64, userID int64, phone string) {
    statusMsg := sendStatus(chatID, "📱 Поиск по номеру телефона...")
    
    result := &SearchResult{
        Query:     phone,
        Type:      "phone",
        Timestamp: time.Now().Unix(),
        Sources:   make(map[string]interface{}),
    }
    
    var wg sync.WaitGroup
    
    wg.Add(1)
    go func() {
        defer wg.Done()
        result.Phone = searchPhone(phone)
        bot.Send(tgbotapi.NewEditMessageText(chatID, statusMsg.MessageID, "📱 Определение оператора и региона..."))
    }()
    
    wg.Add(1)
    go func() {
        defer wg.Done()
        result.Leaks = searchLeaksByPhone(phone)
    }()
    
    wg.Add(1)
    go func() {
        defer wg.Done()
        result.Telegram = searchTelegramByPhone(phone)
    }()
    
    wg.Add(1)
    go func() {
        defer wg.Done()
        result.Social = searchSocialByPhone(phone)
    }()
    
    wg.Add(1)
    go func() {
        defer wg.Done()
        result.Geo = searchGeoByPhone(phone)
    }()
    
    wg.Wait()
    
    cacheResult(phone, result)
    userCache[userID] = result
    
    response := formatPhoneResult(result)
    edit := tgbotapi.NewEditMessageText(chatID, statusMsg.MessageID, response)
    edit.ParseMode = "HTML"
    edit.ReplyMarkup = resultKeyboard(phone, "phone")
    edit.DisableWebPagePreview = false
    bot.Send(edit)
}

func searchPhone(phone string) *PhoneInfo {
    info := &PhoneInfo{Number: phone, Valid: true}
    
    // 1. Бесплатное API htmlweb.ru
    resp, err := httpClient.Get("https://htmlweb.ru/geo/api.php?json&telcod=" + strings.TrimPrefix(phone, "+"))
    if err == nil {
        defer resp.Body.Close()
        var data map[string]interface{}
        json.NewDecoder(resp.Body).Decode(&data)
        if country, ok := data["country"].(map[string]interface{}); ok {
            info.Country = fmt.Sprintf("%v", country["name"])
            info.Location = fmt.Sprintf("%v", country["capital"])
        }
        if region, ok := data["region"].(map[string]interface{}); ok {
            info.Location = fmt.Sprintf("%v", region["name"])
        }
    }
    
    // 2. NumVerify API (бесплатно 250 запросов)
    if NUMVERIFY_API_KEY != "" {
        url := fmt.Sprintf("http://apilayer.net/api/validate?access_key=%s&number=%s", NUMVERIFY_API_KEY, phone)
        resp, err := httpClient.Get(url)
        if err == nil {
            defer resp.Body.Close()
            var data map[string]interface{}
            json.NewDecoder(resp.Body).Decode(&data)
            if valid, ok := data["valid"].(bool); ok {
                info.Valid = valid
            }
            if carrier, ok := data["carrier"].(string); ok {
                info.Carrier = carrier
            }
            if lineType, ok := data["line_type"].(string); ok {
                info.LineType = lineType
            }
            if location, ok := data["location"].(string); ok {
                info.Location = location
            }
        }
    }
    
    return info
}

// ============================================
// ПОИСК УТЕЧЕК
// ============================================

func searchLeaksByPhone(phone string) []LeakInfo {
    var leaks []LeakInfo
    
    // Парсинг публичных баз
    for _, source := range FREE_LEAK_SOURCES {
        resp, err := httpClient.Get(source + url.QueryEscape(phone))
        if err != nil {
            continue
        }
        defer resp.Body.Close()
        
        body, _ := io.ReadAll(resp.Body)
        bodyStr := string(body)
        
        // Psbdmp
        if strings.Contains(source, "psbdmp") {
            var data map[string]interface{}
            json.Unmarshal(body, &data)
            if results, ok := data["results"].([]interface{}); ok {
                for _, r := range results {
                    if res, ok := r.(map[string]interface{}); ok {
                        leaks = append(leaks, LeakInfo{
                            Source:   "Pastebin Dump",
                            Database: fmt.Sprintf("%v", res["id"]),
                            Date:     fmt.Sprintf("%v", res["date"]),
                        })
                    }
                }
            }
        }
        
        // Scylla
        if strings.Contains(source, "scylla") {
            var data map[string]interface{}
            json.Unmarshal(body, &data)
            if results, ok := data["data"].([]interface{}); ok {
                for _, r := range results {
                    if res, ok := r.(map[string]interface{}); ok {
                        leak := LeakInfo{Source: "Scylla"}
                        if db, ok := res["database"].(string); ok {
                            leak.Database = db
                        }
                        if email, ok := res["email"].(string); ok {
                            leak.Email = email
                        }
                        if pass, ok := res["password"].(string); ok {
                            leak.Password = pass
                        }
                        leaks = append(leaks, leak)
                    }
                }
            }
        }
        
        // Простой поиск по ключевым словам в теле ответа
        if strings.Contains(strings.ToLower(bodyStr), "password") ||
           strings.Contains(strings.ToLower(bodyStr), "pass") ||
           strings.Contains(strings.ToLower(bodyStr), "pwd") {
            // Извлекаем возможные пароли через регулярки
            passRegex := regexp.MustCompile(`(?:password|pass|pwd)[:\s]+([^\s]+)`)
            matches := passRegex.FindAllStringSubmatch(bodyStr, -1)
            for _, match := range matches {
                if len(match) > 1 {
                    leaks = append(leaks, LeakInfo{
                        Source:   "Found in text",
                        Password: match[1],
                    })
                }
            }
        }
    }
    
    // Holehe - проверка регистрации на сервисах
    resp, err := httpClient.Get("https://holehe.deno.dev/?email=" + url.QueryEscape(phone+"@gmail.com"))
    if err == nil {
        defer resp.Body.Close()
        var data map[string]interface{}
        json.NewDecoder(resp.Body).Decode(&data)
        if results, ok := data["results"].([]interface{}); ok {
            for _, r := range results {
                if res, ok := r.(map[string]interface{}); ok {
                    if exists, ok := res["exists"].(bool); ok && exists {
                        leaks = append(leaks, LeakInfo{
                            Source:   "Holehe",
                            Database: fmt.Sprintf("%v", res["name"]),
                        })
                    }
                }
            }
        }
    }
    
    return leaks
}

func searchLeaksByEmail(email string) []LeakInfo {
    var leaks []LeakInfo
    
    // HaveIBeenPwned
    resp, err := httpClient.Get("https://haveibeenpwned.com/api/v3/breachedaccount/" + url.QueryEscape(email))
    if err == nil && resp.StatusCode == 200 {
        defer resp.Body.Close()
        var breaches []map[string]interface{}
        json.NewDecoder(resp.Body).Decode(&breaches)
        for _, b := range breaches {
            leaks = append(leaks, LeakInfo{
                Source:   "HIBP",
                Database: fmt.Sprintf("%v", b["Name"]),
                Date:     fmt.Sprintf("%v", b["BreachDate"]),
                Email:    email,
            })
        }
    }
    
    // EmailRep
    resp, err = httpClient.Get("https://emailrep.io/" + url.QueryEscape(email))
    if err == nil {
        defer resp.Body.Close()
        var data map[string]interface{}
        json.NewDecoder(resp.Body).Decode(&data)
        if details, ok := data["details"].(map[string]interface{}); ok {
            if profiles, ok := details["profiles"].([]interface{}); ok {
                for _, p := range profiles {
                    leaks = append(leaks, LeakInfo{
                        Source:   "EmailRep",
                        Database: fmt.Sprintf("%v", p),
                        Email:    email,
                    })
                }
            }
        }
    }
    
    return leaks
}

func searchLeaksByFIO(fio string) []LeakInfo {
    var leaks []LeakInfo
    
    // Поиск в публичных базах по имени
    for _, source := range FREE_LEAK_SOURCES {
        resp, err := httpClient.Get(source + url.QueryEscape(fio))
        if err != nil {
            continue
        }
        defer resp.Body.Close()
        
        body, _ := io.ReadAll(resp.Body)
        bodyStr := string(body)
        
        // Поиск email и телефонов рядом с именем
        emailRegex := regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
        phoneRegex := regexp.MustCompile(`\+?[0-9]{10,15}`)
        
        emails := emailRegex.FindAllString(bodyStr, 5)
        phones := phoneRegex.FindAllString(bodyStr, 5)
        
        for _, email := range emails {
            leaks = append(leaks, LeakInfo{
                Source: "Found near name",
                Email:  email,
                Name:   fio,
            })
        }
        for _, phone := range phones {
            leaks = append(leaks, LeakInfo{
                Source: "Found near name",
                Phone:  phone,
                Name:   fio,
            })
        }
    }
    
    return leaks
}

// ============================================
// ПОИСК TELEGRAM (через MTProto)
// ============================================

func searchTelegramByPhone(phone string) *TelegramInfo {
    if len(mtprotoAPI) == 0 {
        // Fallback - поиск через публичные боты
        return searchTelegramPublic(phone)
    }
    
    mtprotoMutex.RLock()
    api := mtprotoAPI[0]
    mtprotoMutex.RUnlock()
    
    if api == nil {
        return nil
    }
    
    // Конвертируем телефон в формат для Telegram API
    cleanPhone := strings.TrimPrefix(phone, "+")
    
    contacts, err := api.ContactsImportContacts(ctx, []tg.InputPhoneContact{
        {
            ClientID:  1,
            Phone:     cleanPhone,
            FirstName: "Search",
            LastName:  "",
        },
    })
    if err != nil {
        log.Printf("MTProto search error: %v", err)
        return searchTelegramPublic(phone)
    }
    
    if len(contacts.Users) == 0 {
        return nil
    }
    
    user, ok := contacts.Users[0].(*tg.User)
    if !ok {
        return nil
    }
    
    info := &TelegramInfo{
        ID:        user.ID,
        Username:  user.Username,
        FirstName: user.FirstName,
        LastName:  user.LastName,
        Phone:     user.Phone,
        Premium:   user.Premium,
        Scam:      user.Scam,
        Fake:      user.Fake,
        Bot:       user.Bot,
        Verified:  user.Verified,
        Restricted: user.Restricted,
    }
    
    // Получаем фото профиля
    if user.Photo != nil {
        photos, err := api.PhotosGetUserPhotos(ctx, &tg.InputUser{
            UserID:     user.ID,
            AccessHash: user.AccessHash,
        })
        if err == nil {
            for _, photo := range photos.Photos {
                if p, ok := photo.(*tg.Photo); ok {
                    // Формируем URL для фото
                    info.Photos = append(info.Photos, fmt.Sprintf(
                        "https://t.me/i/user/%d/photo_%d.jpg",
                        user.ID, p.ID,
                    ))
                }
            }
        }
    }
    
    // Получаем био через GetFullUser
    fullUser, err := api.UsersGetFullUser(ctx, &tg.InputUser{
        UserID:     user.ID,
        AccessHash: user.AccessHash,
    })
    if err == nil {
        if full, ok := fullUser.(*tg.UsersUserFull); ok {
            if about, ok := full.FullUser.About.(*tg.BotInfo); ok {
                info.Bio = about.Description
            }
        }
    }
    
    return info
}

func searchTelegramPublic(query string) *TelegramInfo {
    // Парсинг через t.me
    resp, err := httpClient.Get("https://t.me/" + strings.TrimPrefix(query, "@"))
    if err != nil {
        return nil
    }
    defer resp.Body.Close()
    
    body, _ := io.ReadAll(resp.Body)
    bodyStr := string(body)
    
    info := &TelegramInfo{Username: query}
    
    // Извлекаем имя из meta-тегов
    titleRegex := regexp.MustCompile(`<meta property="og:title" content="([^"]+)"`)
    if match := titleRegex.FindStringSubmatch(bodyStr); len(match) > 1 {
        parts := strings.Split(match[1], " ")
        info.FirstName = parts[0]
        if len(parts) > 1 {
            info.LastName = strings.Join(parts[1:], " ")
        }
    }
    
    // Извлекаем описание
    descRegex := regexp.MustCompile(`<meta property="og:description" content="([^"]+)"`)
    if match := descRegex.FindStringSubmatch(bodyStr); len(match) > 1 {
        info.Bio = match[1]
    }
    
    // Извлекаем фото
    imgRegex := regexp.MustCompile(`<meta property="og:image" content="([^"]+)"`)
    if match := imgRegex.FindStringSubmatch(bodyStr); len(match) > 1 {
        info.Photos = []string{match[1]}
    }
    
    return info
}

func searchTelegramByUsername(username string) *TelegramInfo {
    return searchTelegramPublic(username)
}

// ============================================
// ПОИСК СОЦИАЛЬНЫХ СЕТЕЙ
// ============================================

func searchSocialByPhone(phone string) *SocialInfo {
    social := &SocialInfo{}
    var wg sync.WaitGroup
    
    // VK поиск через публичные методы
    wg.Add(1)
    go func() {
        defer wg.Done()
        social.VK = searchVKByPhone(phone)
    }()
    
    // OK поиск
    wg.Add(1)
    go func() {
        defer wg.Done()
        social.Ok = searchOKByPhone(phone)
    }()
    
    // Проверка регистрации через Holehe
    wg.Add(1)
    go func() {
        defer wg.Done()
        resp, err := httpClient.Get("https://holehe.deno.dev/?email=" + url.QueryEscape(phone+"@gmail.com"))
        if err == nil {
            defer resp.Body.Close()
            var data map[string]interface{}
            json.NewDecoder(resp.Body).Decode(&data)
            
            if results, ok := data["results"].([]interface{}); ok {
                for _, r := range results {
                    if res, ok := r.(map[string]interface{}); ok {
                        name := fmt.Sprintf("%v", res["name"])
                        exists := false
                        if e, ok := res["exists"].(bool); ok {
                            exists = e
                        }
                        
                        if exists {
                            switch name {
                            case "instagram":
                                social.Instagram = &InstProfile{Username: "found"}
                            case "twitter":
                                social.Twitter = &TWProfile{Username: "found"}
                            case "github":
                                social.GitHub = &GHProfile{Login: "found"}
                            case "snapchat":
                                social.Snapchat = &SCProfile{Username: "found"}
                            }
                        }
                    }
                }
            }
        }
    }()
    
    wg.Wait()
    return social
}

func searchVKByPhone(phone string) *VKProfile {
    // Используем публичный метод VK API (без токена)
    cleanPhone := strings.TrimPrefix(phone, "+")
    cleanPhone = strings.ReplaceAll(cleanPhone, " ", "")
    
    // Поиск через vk.com/phone?act=check
    resp, err := httpClient.PostForm("https://vk.com/phone", url.Values{
        "act":   {"check"},
        "phone": {cleanPhone},
    })
    if err != nil {
        return nil
    }
    defer resp.Body.Close()
    
    var data map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&data)
    
    if payload, ok := data["payload"].([]interface{}); ok && len(payload) > 0 {
        if userData, ok := payload[0].(map[string]interface{}); ok {
            profile := &VKProfile{}
            if id, ok := userData["id"].(float64); ok {
                profile.ID = int(id)
            }
            if fn, ok := userData["first_name"].(string); ok {
                profile.FirstName = fn
            }
            if ln, ok := userData["last_name"].(string); ok {
                profile.LastName = ln
            }
            return profile
        }
    }
    
    return nil
}

func searchOKByPhone(phone string) *OKProfile {
    cleanPhone := strings.TrimPrefix(phone, "+")
    
    // OK.ru поиск по телефону
    resp, err := httpClient.Get("https://ok.ru/dk?cmd=AnonymLogin&st.cmd=anonymLogin&st.pho=" + cleanPhone)
    if err != nil {
        return nil
    }
    defer resp.Body.Close()
    
    body, _ := io.ReadAll(resp.Body)
    bodyStr := string(body)
    
    if strings.Contains(bodyStr, "found") || strings.Contains(bodyStr, "registered") {
        return &OKProfile{ID: "found", Name: "Профиль найден"}
    }
    
    return nil
}

func searchSocialByEmail(email string) *SocialInfo {
    social := &SocialInfo{}
    
    // Проверка через Holehe на все соцсети
    resp, err := httpClient.Get("https://holehe.deno.dev/?email=" + url.QueryEscape(email))
    if err != nil {
        return social
    }
    defer resp.Body.Close()
    
    var data map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&data)
    
    if results, ok := data["results"].([]interface{}); ok {
        for _, r := range results {
            if res, ok := r.(map[string]interface{}); ok {
                name := fmt.Sprintf("%v", res["name"])
                exists := false
                if e, ok := res["exists"].(bool); ok {
                    exists = e
                }
                
                if exists {
                    switch strings.ToLower(name) {
                    case "instagram":
                        social.Instagram = &InstProfile{Username: email}
                    case "twitter", "x":
                        social.Twitter = &TWProfile{Username: email}
                    case "github":
                        social.GitHub = &GHProfile{Login: email}
                    case "snapchat":
                        social.Snapchat = &SCProfile{Username: email}
                    case "spotify":
                        social.Spotify = &SPProfile{ID: email}
                    }
                }
            }
        }
    }
    
    return social
}

func searchSocialByUsername(username string) *SocialInfo {
    social := &SocialInfo{}
    cleanUsername := strings.TrimPrefix(username, "@")
    
    var wg sync.WaitGroup
    
    // Проверка на разных платформах
    platforms := []struct {
        name string
        url  string
    }{
        {"instagram", "https://www.instagram.com/" + cleanUsername},
        {"twitter", "https://twitter.com/" + cleanUsername},
        {"github", "https://github.com/" + cleanUsername},
        {"tiktok", "https://www.tiktok.com/@" + cleanUsername},
        {"steam", "https://steamcommunity.com/id/" + cleanUsername},
    }
    
    for _, p := range platforms {
        wg.Add(1)
        go func(platform struct{ name, url string }) {
            defer wg.Done()
            
            resp, err := httpClient.Head(platform.url)
            if err == nil && resp.StatusCode == 200 {
                switch platform.name {
                case "instagram":
                    social.Instagram = &InstProfile{Username: cleanUsername}
                case "twitter":
                    social.Twitter = &TWProfile{Username: cleanUsername}
                case "github":
                    social.GitHub = &GHProfile{Login: cleanUsername}
                case "tiktok":
                    social.TikTok = &TTProfile{Username: cleanUsername}
                case "steam":
                    social.Steam = &SteamProfile{SteamID: cleanUsername}
                }
            }
        }(p)
    }
    
    wg.Wait()
    return social
}

func searchSocialByFIO(fio string) *SocialInfo {
    social := &SocialInfo{}
    
    // Поиск VK по имени
    parts := strings.Fields(fio)
    if len(parts) >= 2 {
        query := url.QueryEscape(fio)
        resp, err := httpClient.Get("https://vk.com/search?c%5Bname%5D=1&c%5Bq%5D=" + query)
        if err == nil {
            defer resp.Body.Close()
            body, _ := io.ReadAll(resp.Body)
            bodyStr := string(body)
            
            // Извлекаем ID профилей
            idRegex := regexp.MustCompile(`\/id(\d+)`)
            matches := idRegex.FindAllStringSubmatch(bodyStr, 3)
            if len(matches) > 0 {
                id, _ := strconv.Atoi(matches[0][1])
                social.VK = &VKProfile{
                    ID:        id,
                    FirstName: parts[0],
                    LastName:  parts[1],
                }
            }
        }
    }
    
    return social
}

// ============================================
// ПОИСК ГЕОЛОКАЦИИ
// ============================================

func searchGeoByPhone(phone string) *GeoInfo {
    // Сначала получаем регион через htmlweb.ru
    resp, err := httpClient.Get("https://htmlweb.ru/geo/api.php?json&telcod=" + strings.TrimPrefix(phone, "+"))
    if err != nil {
        return nil
    }
    defer resp.Body.Close()
    
    var data map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&data)
    
    geo := &GeoInfo{}
    
    // Извлекаем город/регион
    var location string
    if city, ok := data["capital"].(map[string]interface{}); ok {
        location = fmt.Sprintf("%v", city["name"])
        geo.City = location
    }
    if region, ok := data["region"].(map[string]interface{}); ok {
        geo.Region = fmt.Sprintf("%v", region["name"])
        if location == "" {
            location = geo.Region
        }
    }
    if country, ok := data["country"].(map[string]interface{}); ok {
        geo.Country = fmt.Sprintf("%v", country["name"])
    }
    
    // Если есть Google API ключ - получаем координаты и фото
    if GOOGLE_API_KEY != "" && location != "" {
        // Геокодинг
        geoURL := fmt.Sprintf(
            "https://maps.googleapis.com/maps/api/geocode/json?address=%s&key=%s",
            url.QueryEscape(location), GOOGLE_API_KEY,
        )
        
        resp, err := httpClient.Get(geoURL)
        if err == nil {
            defer resp.Body.Close()
            var geoData map[string]interface{}
            json.NewDecoder(resp.Body).Decode(&geoData)
            
            if results, ok := geoData["results"].([]interface{}); ok && len(results) > 0 {
                if result, ok := results[0].(map[string]interface{}); ok {
                    if geom, ok := result["geometry"].(map[string]interface{}); ok {
                        if loc, ok := geom["location"].(map[string]interface{}); ok {
                            geo.Lat = loc["lat"].(float64)
                            geo.Lng = loc["lng"].(float64)
                            geo.MapURL = fmt.Sprintf("https://www.google.com/maps?q=%f,%f", geo.Lat, geo.Lng)
                            geo.StreetView = fmt.Sprintf(
                                "https://maps.googleapis.com/maps/api/streetview?size=600x300&location=%f,%f&key=%s",
                                geo.Lat, geo.Lng, GOOGLE_API_KEY,
                            )
                        }
                    }
                    geo.Address = fmt.Sprintf("%v", result["formatted_address"])
                }
            }
        }
        
        // Поиск мест рядом (Place Search)
        if geo.Lat != 0 {
            placesURL := fmt.Sprintf(
                "https://maps.googleapis.com/maps/api/place/nearbysearch/json?location=%f,%f&radius=500&key=%s",
                geo.Lat, geo.Lng, GOOGLE_API_KEY,
            )
            resp, err := httpClient.Get(placesURL)
            if err == nil {
                defer resp.Body.Close()
                var placesData map[string]interface{}
                json.NewDecoder(resp.Body).Decode(&placesData)
                
                if results, ok := placesData["results"].([]interface{}); ok {
                    for i, r := range results {
                        if i >= 3 {
                            break
                        }
                        if place, ok := r.(map[string]interface{}); ok {
                            if photos, ok := place["photos"].([]interface{}); ok && len(photos) > 0 {
                                if photo, ok := photos[0].(map[string]interface{}); ok {
                                    ref := photo["photo_reference"]
                                    photoURL := fmt.Sprintf(
                                        "https://maps.googleapis.com/maps/api/place/photo?maxwidth=400&photoreference=%v&key=%s",
                                        ref, GOOGLE_API_KEY,
                                    )
                                    geo.Photos = append(geo.Photos, photoURL)
                                }
                            }
                        }
                    }
                }
            }
        }
    }
    
    return geo
}

// ============================================
// ПОИСК ПО EMAIL
// ============================================

func performEmailSearch(chatID int64, userID int64, email string) {
    statusMsg := sendStatus(chatID, "📧 Поиск по email...")
    
    result := &SearchResult{
        Query:     email,
        Type:      "email",
        Timestamp: time.Now().Unix(),
        Sources:   make(map[string]interface{}),
    }
    
    var wg sync.WaitGroup
    
    wg.Add(1)
    go func() {
        defer wg.Done()
        result.Email = searchEmail(email)
    }()
    
    wg.Add(1)
    go func() {
        defer wg.Done()
        result.Leaks = searchLeaksByEmail(email)
    }()
    
    wg.Add(1)
    go func() {
        defer wg.Done()
        result.Social = searchSocialByEmail(email)
    }()
    
    wg.Wait()
    
    cacheResult(email, result)
    userCache[userID] = result
    
    response := formatEmailResult(result)
    edit := tgbotapi.NewEditMessageText(chatID, statusMsg.MessageID, response)
    edit.ParseMode = "HTML"
    edit.ReplyMarkup = resultKeyboard(email, "email")
    bot.Send(edit)
}

func searchEmail(email string) *EmailInfo {
    info := &EmailInfo{Email: email}
    
    // Проверка валидности через EmailRep
    resp, err := httpClient.Get("https://emailrep.io/" + url.QueryEscape(email))
    if err == nil {
        defer resp.Body.Close()
        var data map[string]interface{}
        json.NewDecoder(resp.Body).Decode(&data)
        
        if reputation, ok := data["reputation"].(string); ok {
            info.Valid = reputation != "none"
        }
        if details, ok := data["details"].(map[string]interface{}); ok {
            if disposable, ok := details["disposable"].(bool); ok {
                info.Disposable = disposable
            }
            if domain, ok := details["domain"].(string); ok {
                info.Domain = domain
            }
            if provider, ok := details["provider"].(string); ok {
                info.Provider = provider
            }
            if profiles, ok := details["profiles"].([]interface{}); ok {
                for _, p := range profiles {
                    info.SocialLinks = append(info.SocialLinks, fmt.Sprintf("%v", p))
                }
            }
        }
    }
    
    // Hunter.io проверка
    if HUNTER_API_KEY != "" {
        url := fmt.Sprintf("https://api.hunter.io/v2/email-verifier?email=%s&api_key=%s", email, HUNTER_API_KEY)
        resp, err := httpClient.Get(url)
        if err == nil {
            defer resp.Body.Close()
            var data map[string]interface{}
            json.NewDecoder(resp.Body).Decode(&data)
            if data, ok := data["data"].(map[string]interface{}); ok {
                if result, ok := data["result"].(string); ok {
                    info.Valid = result == "deliverable"
                }
            }
        }
    }
    
    return info
}

// ============================================
// ПОИСК ПО ИМЕНИ/ФАМИЛИИ
// ============================================

func performFIOSearch(chatID int64, userID int64, fio string) {
    statusMsg := sendStatus(chatID, "👤 Поиск по ФИО...")
    
    result := &SearchResult{
        Query:     fio,
        Type:      "fio",
        Timestamp: time.Now().Unix(),
        Sources:   make(map[string]interface{}),
    }
    
    var wg sync.WaitGroup
    
    wg.Add(1)
    go func() {
        defer wg.Done()
        result.Leaks = searchLeaksByFIO(fio)
    }()
    
    wg.Add(1)
    go func() {
        defer wg.Done()
        result.Social = searchSocialByFIO(fio)
    }()
    
    wg.Add(1)
    go func() {
        defer wg.Done()
        result.RelatedPeople = searchRelatedPeople(fio)
    }()
    
    // Демографические данные
    wg.Add(1)
    go func() {
        defer wg.Done()
        parts := strings.Fields(fio)
        if len(parts) > 0 {
            firstName := parts[0]
            
            // Nationalize
            resp, _ := httpClient.Get("https://api.nationalize.io/?name=" + firstName)
            if resp != nil {
                defer resp.Body.Close()
                var data map[string]interface{}
                json.NewDecoder(resp.Body).Decode(&data)
                result.Sources["nationalize"] = data
            }
            
            // Agify
            resp, _ = httpClient.Get("https://api.agify.io/?name=" + firstName)
            if resp != nil {
                defer resp.Body.Close()
                var data map[string]interface{}
                json.NewDecoder(resp.Body).Decode(&data)
                result.Sources["age"] = data
            }
            
            // Genderize
            resp, _ = httpClient.Get("https://api.genderize.io/?name=" + firstName)
            if resp != nil {
                defer resp.Body.Close()
                var data map[string]interface{}
                json.NewDecoder(resp.Body).Decode(&data)
                result.Sources["gender"] = data
            }
        }
    }()
    
    wg.Wait()
    
    cacheResult(fio, result)
    userCache[userID] = result
    
    response := formatFIOResult(result)
    edit := tgbotapi.NewEditMessageText(chatID, statusMsg.MessageID, response)
    edit.ParseMode = "HTML"
    edit.ReplyMarkup = resultKeyboard(fio, "fio")
    bot.Send(edit)
}

func searchRelatedPeople(fio string) []PersonInfo {
    var people []PersonInfo
    
    // Поиск через поисковики
    query := url.QueryEscape(fio)
    
    // DuckDuckGo Instant Answer API
    resp, err := httpClient.Get("https://api.duckduckgo.com/?q=" + query + "&format=json")
    if err == nil {
        defer resp.Body.Close()
        var data map[string]interface{}
        json.NewDecoder(resp.Body).Decode(&data)
        
        if related, ok := data["RelatedTopics"].([]interface{}); ok {
            for _, r := range related {
                if topic, ok := r.(map[string]interface{}); ok {
                    if text, ok := topic["Text"].(string); ok {
                        // Извлекаем возможные имена из текста
                        nameRegex := regexp.MustCompile(`[A-ZА-Я][a-zа-я]+\s+[A-ZА-Я][a-zа-я]+`)
                        names := nameRegex.FindAllString(text, -1)
                        for _, name := range names {
                            if name != fio {
                                people = append(people, PersonInfo{
                                    Name:     name,
                                    Relation: "associated",
                                })
                            }
                        }
                    }
                }
            }
        }
    }
    
    return people
}

// ============================================
// ПОИСК ПО ТЕЛЕГРАМ ID/USERNAME
// ============================================

func performTelegramSearch(chatID int64, userID int64, query string) {
    statusMsg := sendStatus(chatID, "🆔 Поиск в Telegram...")
    
    result := &SearchResult{
        Query:     query,
        Type:      "telegram",
        Timestamp: time.Now().Unix(),
        Sources:   make(map[string]interface{}),
    }
    
    result.Telegram = searchTelegramByUsername(query)
    
    // Дополнительный поиск связанных данных
    if result.Telegram != nil && result.Telegram.Phone != "" {
        result.Phone = searchPhone(result.Telegram.Phone)
        result.Leaks = searchLeaksByPhone(result.Telegram.Phone)
    }
    
    cacheResult(query, result)
    userCache[userID] = result
    
    response := formatTelegramResult(result)
    edit := tgbotapi.NewEditMessageText(chatID, statusMsg.MessageID, response)
    edit.ParseMode = "HTML"
    edit.ReplyMarkup = resultKeyboard(query, "telegram")
    bot.Send(edit)
}

// ============================================
// ПОИСК ПО АДРЕСУ
// ============================================

func performAddressSearch(chatID int64, userID int64, address string) {
    statusMsg := sendStatus(chatID, "🏠 Поиск по адресу...")
    
    result := &SearchResult{
        Query:     address,
        Type:      "address",
        Timestamp: time.Now().Unix(),
        Sources:   make(map[string]interface{}),
    }
    
    // Геокодинг адреса
    if GOOGLE_API_KEY != "" {
        geoURL := fmt.Sprintf(
            "https://maps.googleapis.com/maps/api/geocode/json?address=%s&key=%s",
            url.QueryEscape(address), GOOGLE_API_KEY,
        )
        
        resp, err := httpClient.Get(geoURL)
        if err == nil {
            defer resp.Body.Close()
            var data map[string]interface{}
            json.NewDecoder(resp.Body).Decode(&data)
            
            if results, ok := data["results"].([]interface{}); ok && len(results) > 0 {
                r := results[0].(map[string]interface{})
                geom := r["geometry"].(map[string]interface{})
                loc := geom["location"].(map[string]interface{})
                
                result.Geo = &GeoInfo{
                    Lat:     loc["lat"].(float64),
                    Lng:     loc["lng"].(float64),
                    Address: r["formatted_address"].(string),
                    MapURL:  fmt.Sprintf("https://www.google.com/maps?q=%f,%f", loc["lat"].(float64), loc["lng"].(float64)),
                    StreetView: fmt.Sprintf(
                        "https://maps.googleapis.com/maps/api/streetview?size=600x300&location=%f,%f&key=%s",
                        loc["lat"].(float64), loc["lng"].(float64), GOOGLE_API_KEY,
                    ),
                }
                
                // Извлекаем компоненты адреса
                if comps, ok := r["address_components"].([]interface{}); ok {
                    for _, c := range comps {
                        comp := c.(map[string]interface{})
                        types := comp["types"].([]interface{})
                        for _, t := range types {
                            switch t.(string) {
                            case "locality":
                                result.Geo.City = comp["long_name"].(string)
                            case "administrative_area_level_1":
                                result.Geo.Region = comp["long_name"].(string)
                            case "country":
                                result.Geo.Country = comp["long_name"].(string)
                            case "postal_code":
                                result.Geo.PostalCode = comp["long_name"].(string)
                            }
                        }
                    }
                }
            }
        }
    }
    
    cacheResult(address, result)
    userCache[userID] = result
    
    response := formatAddressResult(result)
    edit := tgbotapi.NewEditMessageText(chatID, statusMsg.MessageID, response)
    edit.ParseMode = "HTML"
    
    keyboard := tgbotapi.NewInlineKeyboardMarkup(
        tgbotapi.NewInlineKeyboardRow(
            tgbotapi.NewInlineKeyboardButtonURL("📍 Открыть на карте", result.Geo.MapURL),
            tgbotapi.NewInlineKeyboardButtonURL("🚶 Street View", result.Geo.StreetView),
        ),
        tgbotapi.NewInlineKeyboardRow(
            tgbotapi.NewInlineKeyboardButtonData("📄 Экспорт JSON", "export_"+address),
            tgbotapi.NewInlineKeyboardButtonData("◀️ Меню", "menu"),
        ),
    )
    edit.ReplyMarkup = &keyboard
    bot.Send(edit)
}

// ============================================
// ПОИСК ПО АВТОМОБИЛЮ (ГОСНОМЕР/VIN)
// ============================================

func performCarSearch(chatID int64, userID int64, query string) {
    statusMsg := sendStatus(chatID, "🚗 Поиск по автомобилю...")
    
    result := &SearchResult{
        Query:     query,
        Type:      "car",
        Timestamp: time.Now().Unix(),
        Sources:   make(map[string]interface{}),
    }
    
    // Определяем тип запроса (госномер или VIN)
    isVIN := len(query) == 17
    
    if isVIN {
        // Поиск по VIN через публичные базы
        // NHTSA API (бесплатно)
        resp, err := httpClient.Get("https://vpic.nhtsa.dot.gov/api/vehicles/decodevin/" + query + "?format=json")
        if err == nil {
            defer resp.Body.Close()
            var data map[string]interface{}
            json.NewDecoder(resp.Body).Decode(&data)
            result.Sources["vin_decode"] = data
        }
    } else {
        // Поиск по госномеру через базы ГИБДД (публичные зеркала)
        // Используем публичные прокси-сервисы
        resp, err := httpClient.Get("https://гибдд.рф/check/auto?regnum=" + url.QueryEscape(query))
        if err == nil {
            defer resp.Body.Close()
            body, _ := io.ReadAll(resp.Body)
            
            // Парсим информацию об автомобиле
            bodyStr := string(body)
            
            // Извлекаем марку/модель
            brandRegex := regexp.MustCompile(`Марка[:\s]+([^<\n]+)`)
            modelRegex := regexp.MustCompile(`Модель[:\s]+([^<\n]+)`)
            yearRegex := regexp.MustCompile(`Год выпуска[:\s]+([^<\n]+)`)
            
            carInfo := make(map[string]string)
            if match := brandRegex.FindStringSubmatch(bodyStr); len(match) > 1 {
                carInfo["brand"] = strings.TrimSpace(match[1])
            }
            if match := modelRegex.FindStringSubmatch(bodyStr); len(match) > 1 {
                carInfo["model"] = strings.TrimSpace(match[1])
            }
            if match := yearRegex.FindStringSubmatch(bodyStr); len(match) > 1 {
                carInfo["year"] = strings.TrimSpace(match[1])
            }
            
            result.Sources["car_info"] = carInfo
        }
    }
    
    cacheResult(query, result)
    userCache[userID] = result
    
    response := formatCarResult(result)
    edit := tgbotapi.NewEditMessageText(chatID, statusMsg.MessageID, response)
    edit.ParseMode = "HTML"
    edit.ReplyMarkup = resultKeyboard(query, "car")
    bot.Send(edit)
}

// ============================================
// ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ
// ============================================

func sendMainMenu(chatID int64) {
    keyboard := tgbotapi.NewInlineKeyboardMarkup(
        tgbotapi.NewInlineKeyboardRow(
            tgbotapi.NewInlineKeyboardButtonData("🔍 Универсальный поиск", "search_any"),
        ),
        tgbotapi.NewInlineKeyboardRow(
            tgbotapi.NewInlineKeyboardButtonData("📱 По телефону", "search_phone"),
            tgbotapi.NewInlineKeyboardButtonData("📧 По email", "search_email"),
        ),
        tgbotapi.NewInlineKeyboardRow(
            tgbotapi.NewInlineKeyboardButtonData("🆔 Telegram ID/username", "search_telegram"),
            tgbotapi.NewInlineKeyboardButtonData("👤 По ФИО", "search_fio"),
        ),
        tgbotapi.NewInlineKeyboardRow(
            tgbotapi.NewInlineKeyboardButtonData("🏠 По адресу", "search_address"),
            tgbotapi.NewInlineKeyboardButtonData("🚗 По авто", "search_car"),
        ),
        tgbotapi.NewInlineKeyboardRow(
            tgbotapi.NewInlineKeyboardButtonData("👤 По username", "search_username"),
        ),
        tgbotapi.NewInlineKeyboardRow(
            tgbotapi.NewInlineKeyboardButtonData("📊 Статистика", "stats"),
            tgbotapi.NewInlineKeyboardButtonData("⚙️ Настройки", "settings"),
        ),
    )
    
    msg := tgbotapi.NewMessage(chatID, "🔎 <b>OSINT Bot RAGE mode</b>\n\nВыберите тип поиска:")
    msg.ParseMode = "HTML"
    msg.ReplyMarkup = keyboard
    bot.Send(msg)
}

func sendHelp(chatID int64) {
    helpText := `<b>📚 OSINT Bot - Бесплатный поиск информации</b>

<b>🔍 Доступные типы поиска:</b>
• <b>Универсальный</b> - автоопределение типа данных
• <b>Телефон</b> - оператор, регион, утечки, Telegram, соцсети
• <b>Email</b> - валидация, утечки, связанные аккаунты
• <b>Telegram</b> - профиль, фото, номер (если есть)
• <b>ФИО</b> - утечки, соцсети, связанные люди
• <b>Адрес</b> - геолокация, Street View, фото места
• <b>Авто</b> - VIN/госномер, марка, модель, год
• <b>Username</b> - поиск по 10+ соцсетям

<b>📊 Источники данных (БЕСПЛАТНО):</b>
• Psbdmp, Scylla - дампы утечек
• HaveIBeenPwned - база взломанных аккаунтов
• Holehe - проверка регистрации на 100+ сервисах
• EmailRep - репутация email
• htmlweb.ru - определение оператора/региона
• Google Maps - геолокация и фото
• VK API - поиск профилей
• DuckDuckGo - поиск связанных людей
• NHTSA - расшифровка VIN

<b>⚡ Команды:</b>
/start - главное меню
/help - справка
/stats - статистика запросов
/clear - очистить кеш

<b>💾 Кеширование:</b>
Результаты кешируются в Redis на 1 час

<b>🔐 MTProto (Telegram):</b>
Прямой доступ к API Telegram для поиска по номеру
(требуется APP_ID и APP_HASH с my.telegram.org)`

    msg := tgbotapi.NewMessage(chatID, helpText)
    msg.ParseMode = "HTML"
    bot.Send(msg)
}

func showStats(chatID int64) {
    var keys int
    if rdb != nil {
        keys, _ = rdb.DBSize(ctx).Result()
    }
    
    stats := fmt.Sprintf(`📊 <b>Статистика бота</b>

• Запросов в кеше: %d
• Время кеша: %d сек
• MTProto сессий: %d
• HTTP прокси: %v

<b>Источники данных:</b>
• Утечек: %d источников
• Телефон: %d API
• Email: %d API
• Соцсети: 10+ платформ`,
        int(keys),
        CACHE_TTL,
        len(mtprotoAPI),
        os.Getenv("PROXY_URL") != "",
        len(FREE_LEAK_SOURCES),
        len(FREE_PHONE_SOURCES),
        len(FREE_EMAIL_SOURCES),
    )
    
    msg := tgbotapi.NewMessage(chatID, stats)
    msg.ParseMode = "HTML"
    bot.Send(msg)
}

func sendStatus(chatID int64, text string) tgbotapi.Message {
    msg := tgbotapi.NewMessage(chatID, "🔄 " + text)
    sent, _ := bot.Send(msg)
    return sent
}

func isAdmin(userID int64) bool {
    for _, id := range ADMIN_IDS {
        if id == userID {
            return true
        }
    }
    return false
}

func clearUserCache(userID int64) {
    delete(userCache, userID)
}

func cacheResult(query string, result *SearchResult) {
    if rdb == nil {
        return
    }
    
    key := "osint:" + md5Hash(query)
    data, _ := json.Marshal(result)
    rdb.Set(ctx, key, data, time.Duration(CACHE_TTL)*time.Second)
}

func getCachedResult(query string) *SearchResult {
    if rdb == nil {
        return nil
    }
    
    key := "osint:" + md5Hash(query)
    data, err := rdb.Get(ctx, key).Bytes()
    if err != nil {
        return nil
    }
    
    var result SearchResult
    json.Unmarshal(data, &result)
    return &result
}

func md5Hash(s string) string {
    hash := md5.Sum([]byte(s))
    return hex.EncodeToString(hash[:])
}

func resultKeyboard(query, queryType string) tgbotapi.InlineKeyboardMarkup {
    encoded := url.QueryEscape(query)
    
    rows := [][]tgbotapi.InlineKeyboardButton{
        {
            tgbotapi.NewInlineKeyboardButtonData("📄 Полный JSON", "export_"+encoded),
            tgbotapi.NewInlineKeyboardButtonData("🗺 Карта", "map_"+encoded),
        },
    }
    
    if queryType == "phone" || queryType == "address" {
        rows = append(rows, []tgbotapi.InlineKeyboardButton{
            tgbotapi.NewInlineKeyboardButtonData("📸 Фото места", "photo_"+encoded),
        })
    }
    
    rows = append(rows, []tgbotapi.InlineKeyboardButton{
        tgbotapi.NewInlineKeyboardButtonData("🔍 Новый поиск", "menu"),
    })
    
    return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// ============================================
// ФОРМАТИРОВАНИЕ РЕЗУЛЬТАТОВ
// ============================================

func formatUniversalResult(r *SearchResult) string {
    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("<b>🔍 Результаты поиска: %s</b>\n", r.Query))
    sb.WriteString(fmt.Sprintf("<i>Тип: %s</i>\n\n", r.Type))
    
    if r.Phone != nil {
        sb.WriteString(formatPhoneSection(r.Phone))
    }
    if r.Email != nil {
        sb.WriteString(formatEmailSection(r.Email))
    }
    if r.Telegram != nil {
        sb.WriteString(formatTelegramSection(r.Telegram))
    }
    if r.Social != nil {
        sb.WriteString(formatSocialSection(r.Social))
    }
    if len(r.Leaks) > 0 {
        sb.WriteString(fmt.Sprintf("📋 <b>Утечки:</b> %d записей\n", len(r.Leaks)))
    }
    if r.Geo != nil && r.Geo.Address != "" {
        sb.WriteString(fmt.Sprintf("📍 <b>Местоположение:</b> %s\n", r.Geo.Address))
    }
    
    return sb.String()
}

func formatPhoneResult(r *SearchResult) string {
    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("<b>📱 Результаты по номеру: %s</b>\n\n", r.Query))
    
    if r.Phone != nil {
        sb.WriteString(formatPhoneSection(r.Phone))
    }
    if r.Telegram != nil {
        sb.WriteString(formatTelegramSection(r.Telegram))
    }
    if r.Social != nil {
        sb.WriteString(formatSocialSection(r.Social))
    }
    if len(r.Leaks) > 0 {
        sb.WriteString(fmt.Sprintf("\n📋 <b>Найдено в утечках:</b> %d\n", len(r.Leaks)))
        for i, leak := range r.Leaks {
            if i >= 5 {
                sb.WriteString(fmt.Sprintf("... и еще %d\n", len(r.Leaks)-5))
                break
            }
            if leak.Email != "" {
                sb.WriteString(fmt.Sprintf("  • %s: %s\n", leak.Source, leak.Email))
            }
        }
    }
    if r.Geo != nil && r.Geo.Address != "" {
        sb.WriteString(fmt.Sprintf("\n📍 <b>Геолокация:</b> %s\n", r.Geo.Address))
        if r.Geo.MapURL != "" {
            sb.WriteString(fmt.Sprintf("<a href='%s'>🗺 Открыть карту</a>\n", r.Geo.MapURL))
        }
    }
    
    return sb.String()
}

func formatEmailResult(r *SearchResult) string {
    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("<b>📧 Результаты по email: %s</b>\n\n", r.Query))
    
    if r.Email != nil {
        sb.WriteString(formatEmailSection(r.Email))
    }
    if r.Social != nil {
        sb.WriteString(formatSocialSection(r.Social))
    }
    if len(r.Leaks) > 0 {
        sb.WriteString(fmt.Sprintf("\n📋 <b>Утечки:</b> %d\n", len(r.Leaks)))
        for i, leak := range r.Leaks {
            if i >= 5 {
                break
            }
            sb.WriteString(fmt.Sprintf("  • %s (%s)\n", leak.Database, leak.Date))
        }
    }
    
    return sb.String()
}

func formatFIOResult(r *SearchResult) string {
    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("<b>👤 Результаты по ФИО: %s</b>\n\n", r.Query))
    
    if r.Social != nil {
        sb.WriteString(formatSocialSection(r.Social))
    }
    if len(r.Leaks) > 0 {
        sb.WriteString(fmt.Sprintf("📋 <b>Найдено в утечках:</b> %d записей\n", len(r.Leaks)))
    }
    if len(r.RelatedPeople) > 0 {
        sb.WriteString(fmt.Sprintf("\n👥 <b>Связанные люди:</b> %d\n", len(r.RelatedPeople)))
        for _, p := range r.RelatedPeople {
            sb.WriteString(fmt.Sprintf("  • %s (%s)\n", p.Name, p.Relation))
        }
    }
    if sources, ok := r.Sources["age"].(map[string]interface{}); ok {
        if age, ok := sources["age"].(float64); ok {
            sb.WriteString(fmt.Sprintf("\n📊 <b>Предполагаемый возраст:</b> %.0f\n", age))
        }
    }
    
    return sb.String()
}

func formatTelegramResult(r *SearchResult) string {
    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("<b>🆔 Telegram: %s</b>\n\n", r.Query))
    
    if r.Telegram != nil {
        sb.WriteString(formatTelegramSection(r.Telegram))
    }
    if r.Phone != nil {
        sb.WriteString(formatPhoneSection(r.Phone))
    }
    if len(r.Leaks) > 0 {
        sb.WriteString(fmt.Sprintf("📋 <b>Утечки:</b> %d записей\n", len(r.Leaks)))
    }
    
    return sb.String()
}

func formatAddressResult(r *SearchResult) string {
    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("<b>🏠 Адрес: %s</b>\n\n", r.Query))
    
    if r.Geo != nil {
        sb.WriteString(fmt.Sprintf("📍 <b>Координаты:</b> %.6f, %.6f\n", r.Geo.Lat, r.Geo.Lng))
        sb.WriteString(fmt.Sprintf("🏙 <b>Город:</b> %s\n", r.Geo.City))
        sb.WriteString(fmt.Sprintf("🗺 <b>Регион:</b> %s\n", r.Geo.Region))
        sb.WriteString(fmt.Sprintf("🌍 <b>Страна:</b> %s\n", r.Geo.Country))
        if r.Geo.MapURL != "" {
            sb.WriteString(fmt.Sprintf("\n<a href='%s'>🗺 Открыть на Google Maps</a>\n", r.Geo.MapURL))
        }
        if r.Geo.StreetView != "" {
            sb.WriteString(fmt.Sprintf("<a href='%s'>🚶 Открыть Street View</a>\n", r.Geo.StreetView))
        }
    }
    
    return sb.String()
}

func formatCarResult(r *SearchResult) string {
    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("<b>🚗 Автомобиль: %s</b>\n\n", r.Query))
    
    if info, ok := r.Sources["car_info"].(map[string]string); ok {
        if brand, ok := info["brand"]; ok && brand != "" {
            sb.WriteString(fmt.Sprintf("🏎 <b>Марка:</b> %s\n", brand))
        }
        if model, ok := info["model"]; ok && model != "" {
            sb.WriteString(fmt.Sprintf("🚙 <b>Модель:</b> %s\n", model))
        }
        if year, ok := info["year"]; ok && year != "" {
            sb.WriteString(fmt.Sprintf("📅 <b>Год выпуска:</b> %s\n", year))
        }
    }
    
    if decode, ok := r.Sources["vin_decode"].(map[string]interface{}); ok {
        if results, ok := decode["Results"].([]interface{}); ok && len(results) > 0 {
            sb.WriteString("\n<b>📋 Расшифровка VIN:</b>\n")
            for _, res := range results {
                if r, ok := res.(map[string]interface{}); ok {
                    if variable, ok := r["Variable"].(string); ok {
                        if value, ok := r["Value"].(string); ok && value != "" && value != "Not Applicable" {
                            sb.WriteString(fmt.Sprintf("  • %s: %s\n", variable, value))
                        }
                    }
                }
            }
        }
    }
    
    return sb.String()
}

func formatPhoneSection(p *PhoneInfo) string {
    var sb strings.Builder
    sb.WriteString("📱 <b>Телефон:</b>\n")
    sb.WriteString(fmt.Sprintf("  • Номер: %s\n", p.Number))
    sb.WriteString(fmt.Sprintf("  • Валидный: %v\n", p.Valid))
    if p.Country != "" {
        sb.WriteString(fmt.Sprintf("  • Страна: %s\n", p.Country))
    }
    if p.Location != "" {
        sb.WriteString(fmt.Sprintf("  • Регион: %s\n", p.Location))
    }
    if p.Carrier != "" {
        sb.WriteString(fmt.Sprintf("  • Оператор: %s\n", p.Carrier))
    }
    return sb.String()
}

func formatEmailSection(e *EmailInfo) string {
    var sb strings.Builder
    sb.WriteString("📧 <b>Email:</b>\n")
    sb.WriteString(fmt.Sprintf("  • Адрес: %s\n", e.Email))
    sb.WriteString(fmt.Sprintf("  • Валидный: %v\n", e.Valid))
    if e.Domain != "" {
        sb.WriteString(fmt.Sprintf("  • Домен: %s\n", e.Domain))
    }
    if e.Provider != "" {
        sb.WriteString(fmt.Sprintf("  • Провайдер: %s\n", e.Provider))
    }
    return sb.String()
}

func formatTelegramSection(t *TelegramInfo) string {
    var sb strings.Builder
    sb.WriteString("🆔 <b>Telegram:</b>\n")
    if t.ID != 0 {
        sb.WriteString(fmt.Sprintf("  • ID: %d\n", t.ID))
    }
    if t.Username != "" {
        sb.WriteString(fmt.Sprintf("  • Username: @%s\n", t.Username))
    }
    if t.FirstName != "" {
        name := t.FirstName
        if t.LastName != "" {
            name += " " + t.LastName
        }
        sb.WriteString(fmt.Sprintf("  • Имя: %s\n", name))
    }
    if t.Phone != "" {
        sb.WriteString(fmt.Sprintf("  • Телефон: %s\n", t.Phone))
    }
    if t.Premium {
        sb.WriteString("  • Premium: ✅\n")
    }
    if len(t.Photos) > 0 {
        sb.WriteString(fmt.Sprintf("  • Фото: <a href='%s'>открыть</a>\n", t.Photos[0]))
    }
    return sb.String()
}

func formatSocialSection(s *SocialInfo) string {
    var sb strings.Builder
    sb.WriteString("💬 <b>Социальные сети:</b>\n")
    
    if s.VK != nil {
        sb.WriteString(fmt.Sprintf("  • VK: vk.com/id%d (%s %s)\n", s.VK.ID, s.VK.FirstName, s.VK.LastName))
    }
    if s.Instagram != nil {
        sb.WriteString(fmt.Sprintf("  • Instagram: @%s\n", s.Instagram.Username))
    }
    if s.Twitter != nil {
        sb.WriteString(fmt.Sprintf("  • Twitter: @%s\n", s.Twitter.Username))
    }
    if s.GitHub != nil {
        sb.WriteString(fmt.Sprintf("  • GitHub: github.com/%s\n", s.GitHub.Login))
    }
    if s.TikTok != nil {
        sb.WriteString(fmt.Sprintf("  • TikTok: @%s\n", s.TikTok.Username))
    }
    if s.Steam != nil {
        sb.WriteString(fmt.Sprintf("  • Steam: steamcommunity.com/id/%s\n", s.Steam.SteamID))
    }
    if s.Ok != nil {
        sb.WriteString("  • Одноклассники: найден\n")
    }
    
    return sb.String()
}

// ============================================
// ОБРАБОТЧИК CALLBACK
// ============================================

func handleCallback(cb *tgbotapi.CallbackQuery) {
    data := cb.Data
    chatID := cb.Message.Chat.ID
    userID := cb.From.ID
    
    if !isAdmin(userID) {
        bot.Request(tgbotapi.NewCallback(cb.ID, "⛔ Доступ запрещен"))
        return
    }
    
    callback := tgbotapi.NewCallback(cb.ID, "")
    bot.Request(callback)
    
    switch {
    case data == "menu":
        edit := tgbotapi.NewEditMessageText(chatID, cb.Message.MessageID, "🔎 <b>OSINT Bot RAGE mode</b>\n\nВыберите тип поиска:")
        edit.ParseMode = "HTML"
        edit.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{
            InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{
                {tgbotapi.NewInlineKeyboardButtonData("🔍 Универсальный поиск", "search_any")},
                {tgbotapi.NewInlineKeyboardButtonData("📱 По телефону", "search_phone"), tgbotapi.NewInlineKeyboardButtonData("📧 По email", "search_email")},
                {tgbotapi.NewInlineKeyboardButtonData("🆔 Telegram", "search_telegram"), tgbotapi.NewInlineKeyboardButtonData("👤 По ФИО", "search_fio")},
                {tgbotapi.NewInlineKeyboardButtonData("🏠 По адресу", "search_address"), tgbotapi.NewInlineKeyboardButtonData("🚗 По авто", "search_car")},
                {tgbotapi.NewInlineKeyboardButtonData("👤 По username", "search_username")},
                {tgbotapi.NewInlineKeyboardButtonData("📊 Статистика", "stats"), tgbotapi.NewInlineKeyboardButtonData("⚙️ Настройки", "settings")},
            },
        }
        bot.Send(edit)
        
    case data == "search_any":
        userState[userID] = "awaiting_any"
        msg := tgbotapi.NewMessage(chatID, "🔍 <b>Универсальный поиск</b>\n\nВведите номер телефона, email, username или ФИО:")
        msg.ParseMode = "HTML"
        bot.Send(msg)
        
    case data == "search_phone":
        userState[userID] = "awaiting_phone"
        msg := tgbotapi.NewMessage(chatID, "📱 Введите номер телефона в любом формате:")
        bot.Send(msg)
        
    case data == "search_email":
        userState[userID] = "awaiting_email"
        msg := tgbotapi.NewMessage(chatID, "📧 Введите email адрес:")
        bot.Send(msg)
        
    case data == "search_telegram":
        userState[userID] = "awaiting_telegram"
        msg := tgbotapi.NewMessage(chatID, "🆔 Введите Telegram username (с @ или без) или ID:")
        bot.Send(msg)
        
    case data == "search_fio":
        userState[userID] = "awaiting_fio"
        msg := tgbotapi.NewMessage(chatID, "👤 Введите ФИО (например: Иванов Иван Иванович):")
        bot.Send(msg)
        
    case data == "search_address":
        userState[userID] = "awaiting_address"
        msg := tgbotapi.NewMessage(chatID, "🏠 Введите адрес:")
        bot.Send(msg)
        
    case data == "search_car":
        userState[userID] = "awaiting_car"
        msg := tgbotapi.NewMessage(chatID, "🚗 Введите госномер или VIN автомобиля:")
        bot.Send(msg)
        
    case data == "search_username":
        userState[userID] = "awaiting_username"
        msg := tgbotapi.NewMessage(chatID, "👤 Введите username для поиска по соцсетям:")
        bot.Send(msg)
        
    case data == "stats":
        showStats(chatID)
        
    case data == "settings":
        keyboard := tgbotapi.NewInlineKeyboardMarkup(
            tgbotapi.NewInlineKeyboardRow(
                tgbotapi.NewInlineKeyboardButtonData("🗑 Очистить кеш", "clear_cache"),
            ),
            tgbotapi.NewInlineKeyboardRow(
                tgbotapi.NewInlineKeyboardButtonData("◀️ Назад", "menu"),
            ),
        )
        edit := tgbotapi.NewEditMessageText(chatID, cb.Message.MessageID, "⚙️ <b>Настройки</b>")
        edit.ParseMode = "HTML"
        edit.ReplyMarkup = &keyboard
        bot.Send(edit)
        
    case data == "clear_cache":
        if rdb != nil {
            rdb.FlushDB(ctx)
        }
        bot.Send(tgbotapi.NewCallback(cb.ID, "✅ Кеш очищен"))
        
    case strings.HasPrefix(data, "export_"):
        query, _ := url.QueryUnescape(strings.TrimPrefix(data, "export_"))
        
        var result *SearchResult
        if cached := getCachedResult(query); cached != nil {
            result = cached
        } else if r, ok := userCache[userID]; ok {
            result = r
        }
        
        if result != nil {
            jsonData, _ := json.MarshalIndent(result, "", "  ")
            doc := tgbotapi.NewDocument(chatID, tgbotapi.FileBytes{
                Name:  fmt.Sprintf("osint_%s.json", md5Hash(query)[:8]),
                Bytes: jsonData,
            })
            bot.Send(doc)
        } else {
            bot.Send(tgbotapi.NewCallback(cb.ID, "❌ Результат не найден"))
        }
        
    case strings.HasPrefix(data, "map_"):
        query, _ := url.QueryUnescape(strings.TrimPrefix(data, "map_"))
        
        var result *SearchResult
        if cached := getCachedResult(query); cached != nil {
            result = cached
        }
        
        if result != nil && result.Geo != nil && result.Geo.Lat != 0 {
            loc := tgbotapi.NewLocation(chatID, result.Geo.Lat, result.Geo.Lng)
            bot.Send(loc)
        } else {
            bot.Send(tgbotapi.NewCallback(cb.ID, "❌ Координаты не найдены"))
        }
        
    case strings.HasPrefix(data, "photo_"):
        query, _ := url.QueryUnescape(strings.TrimPrefix(data, "photo_"))
        
        var result *SearchResult
        if cached := getCachedResult(query); cached != nil {
            result = cached
        }
        
        if result != nil && result.Geo != nil && len(result.Geo.Photos) > 0 {
            for _, photoURL := range result.Geo.Photos {
                photo := tgbotapi.NewPhoto(chatID, tgbotapi.FileURL(photoURL))
                bot.Send(photo)
            }
        } else if result != nil && result.Geo != nil && result.Geo.StreetView != "" {
            photo := tgbotapi.NewPhoto(chatID, tgbotapi.FileURL(result.Geo.StreetView))
            bot.Send(photo)
        } else {
            bot.Send(tgbotapi.NewCallback(cb.ID, "❌ Фото не найдены"))
        }
    }
}

// ============================================
// ЗАГЛУШКИ ДЛЯ НЕРЕАЛИЗОВАННЫХ МЕТОДОВ
// ============================================

func performUsernameSearch(chatID int64, userID int64, username string) {
    statusMsg := sendStatus(chatID, "🔍 Поиск username...")
    
    result := &SearchResult{
        Query:     username,
        Type:      "username",
        Timestamp: time.Now().Unix(),
    }
    
    result.Social = searchSocialByUsername(username)
    
    cacheResult(username, result)
    userCache[userID] = result
    
    response := fmt.Sprintf("<b>🔍 Результаты поиска username: %s</b>\n\n", username)
    if result.Social != nil {
        response += formatSocialSection(result.Social)
    }
    
    edit := tgbotapi.NewEditMessageText(chatID, statusMsg.MessageID, response)
    edit.ParseMode = "HTML"
    edit.ReplyMarkup = resultKeyboard(username, "username")
    bot.Send(edit)
}