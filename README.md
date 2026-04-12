# Osint.bot

Репозиторий приведен к базовой модульной архитектуре Go:

- `cmd/osintbot` — точка входа.
- `internal/app` — сборка приложения и lifecycle.
- `internal/config` — загрузка/валидация ENV.
- `internal/bot` — обработчики Telegram и потокобезопасное состояние.
- `internal/storage` — Redis-слой.
- `internal/mtproto` — старт/учет пула MTProto клиентов.
- `internal/model` — модели домена.
- `legacy` — архив исходного монолитного файла.

## Запуск

```bash
go mod tidy
go run ./cmd/osintbot
```
