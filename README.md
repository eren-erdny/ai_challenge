# AI Challenge Monorepo

Репозиторий для нескольких Go-проектов и учебных заданий. Каждый независимый
проект размещается в `projects` и при необходимости получает собственный
`go.mod`. Корневой `go.work` объединяет модули для локальной разработки.

## Проекты

| Проект | Описание |
|---|---|
| [`deepseek-client`](projects/deepseek-client) | Интерактивный клиент DeepSeek с форматами, ограничениями и локальным судьёй |

## DeepSeek Client

Запуск из корня репозитория:

```powershell
go run ./projects/deepseek-client
```

Тесты:

```powershell
go test ./projects/deepseek-client/...
```

Сборка Windows:

```powershell
New-Item -ItemType Directory -Force bin | Out-Null
go build -o bin/deepseek-chat.exe ./projects/deepseek-client
```

Подробная документация находится в
[`projects/deepseek-client/README.md`](projects/deepseek-client/README.md).

## Новый проект

Для нового независимого Go-проекта:

```powershell
New-Item -ItemType Directory projects/new-project
Set-Location projects/new-project
go mod init github.com/eren-erdny/ai_challenge/projects/new-project
Set-Location ../..
go work use ./projects/new-project
```
