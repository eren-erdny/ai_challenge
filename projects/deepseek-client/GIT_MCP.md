# Git MCP: запуск на удалённом Linux-сервере

Самостоятельная программа `cmd/git-mcp` обслуживает **один клон на сервере**.
Она предоставляет `git_status`, `git_log(limit)`, `git_diff(staged)` и
`git_branches`, а также четыре инструмента расписаний `git_summary_*`.
[Периодические сводки](GIT_SCHEDULES.md) сохраняются в JSON и выполняются сервером
независимо от подключённых клиентов. Команды Git выполняются без shell. Push, commit, merge и исправление
конфликтов пока не реализованы. Подключение: MCP Streamable HTTP с персональным
Bearer-токеном (не OAuth); для локальных MCP-хостов есть `--stdio`.

## 1. Соберите и перенесите программу

Из корня проекта в PowerShell, для обычного Linux x86_64 VPS:

```powershell
$env:GOOS = 'linux'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'
go build -o bin/git-mcp-linux-amd64 ./projects/deepseek-client/cmd/git-mcp
Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED
scp ./bin/git-mcp-linux-amd64 USER@SERVER:git-mcp
```

Для ARM64 замените `amd64` на `arm64` в сборке и имени файла. Здесь и ниже
`USER`, `SERVER` и путь клона заменяются вашими значениями. На VPS нужен Git;
Go для готового бинарника не нужен. Передаётся текущая локальная реализация,
поэтому предварительный push исходников не требуется.

## 2. Запустите на VPS

В SSH-сеансе под обычным пользователем, который владеет клоном:

```sh
chmod 700 "$HOME/git-mcp"
git --version
# Если нужного клона ещё нет, создайте его вручную в отдельной папке:
git clone https://github.com/eren-erdny/ai_challenge.git "$HOME/ai_challenge"
```

Создайте отдельный случайный токен и сохраните его в менеджере паролей. Например,
`openssl rand -hex 32` выдаст подходящее значение. Передайте его серверу без
включения в аргументы процесса или Git-файлы (пример для Bash):

```bash
read -rsp 'MCP token: ' GIT_MCP_TOKEN; echo
export GIT_MCP_TOKEN
"$HOME/git-mcp" --repo "$HOME/ai_challenge" --listen 127.0.0.1:8080
```

Процесс остаётся на переднем плане. Он напечатает адрес `/mcp` в stderr.
Токен обязателен для HTTP и должен содержать минимум 32 символа без пробелов.
`--repo` должен указывать на корень рабочего клона, не bare-репозиторий.

## 3. Подключитесь с компьютера через SSH-туннель

В отдельном локальном терминале, оставив его открытым:

```powershell
ssh -N -o ExitOnForwardFailure=yes -L 127.0.0.1:18080:127.0.0.1:8080 USER@SERVER
```

В терминале проекта (PowerShell 7) задайте **тот же токен**:

```powershell
$env:GIT_MCP_URL = 'http://127.0.0.1:18080/mcp'
$env:GIT_MCP_TOKEN = Read-Host 'MCP token' -MaskInput
go run ./projects/deepseek-client --mcp-list
```

Ожидается сообщение `MCP connection established` и восемь инструментов:
`git_branches`, `git_diff`, `git_log`, `git_status`, `git_summary_schedule`,
`git_summary_list`, `git_summary_get`, `git_summary_cancel`. Это закрывает задание на
подключение и получение списка инструментов; запрос к модели не выполняется.

Чтобы использовать сервер в диалоге, в том же терминале:

```powershell
go run ./projects/deepseek-client
```

В чате: `/mode free`, `/tools on`, затем «Используй git_status и git_log:
покажи состояние серверного репозитория и последние пять коммитов».
Диалог использует ваш обычный API-профиль модели. `/tools` показывает весь
набор инструментов. Локальные файловые инструменты работают с локальной рабочей
папкой, а `git_*` — с клоном на VPS; эти папки не синхронизируются автоматически.
Git-вывод будет передан выбранной модели.

`GIT_MCP_URL` заменяет прежнее автоматическое подключение GitHub MCP.
После изменения URL или токена перезапустите приложение; `/reload` их не меняет.
При недоступном сервере приложение сообщает ошибку подключения и завершает запуск.

## Постоянный процесс и HTTPS

Для постоянной работы используйте systemd от выделенного пользователя. Пример
`~/.config/systemd/user/git-mcp.service` (подставьте абсолютные пути):

```ini
[Unit]
Description=Read-only Git MCP server

[Service]
ExecStart=/home/USER/git-mcp --repo /home/USER/ai_challenge
EnvironmentFile=/home/USER/.config/git-mcp.env
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
UMask=0077

[Install]
WantedBy=default.target
```

В файле `~/.config/git-mcp.env` укажите `GIT_MCP_TOKEN=ваше_значение`, задайте ему
права `chmod 600`, затем `systemctl --user daemon-reload` и
`systemctl --user enable --now git-mcp`. Для работы после выхода пользователя
администратор может включить lingering: `sudo loginctl enable-linger USER`.
Остановите перед этим экземпляр, запущенный вручную на том же порту.

Вместо SSH-туннеля допустим HTTPS reverse proxy на VPS (например Caddy):

```caddyfile
git-mcp.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

Потребуются ваш домен, DNS и настроенный TLS. Клиенту задайте
`GIT_MCP_URL=https://git-mcp.example.com/mcp` и тот же токен. Proxy должен
передавать Authorization. Сам MCP-сервер намеренно отказывается слушать публичный
HTTP-адрес; клиент отказывается посылать токен по HTTP кроме loopback-IP и не
следует редиректам. Для первого опыта используйте SSH-туннель.

## Границы и проверка

Сервер предназначен для личного доступа к доверенному клону и его Git-конфигурации.
Токен даёт доступ к Git-инструментам и управлению расписаниями этого клона. Разные репозитории или
пользователей пока разделяйте отдельными процессами и токенами. HTTP-запросы с
Origin отвергаются: браузерные клиенты не поддерживаются. Встроенного OAuth,
многопользовательских прав, аудита и rate limiting пока нет.

Git-команда ограничена 10 секундами и 256 KiB вывода (есть флаг `truncated`).
`git_diff` не запускает external diff/textconv и не показывает содержимое
untracked-файлов. `git_branches` использует локально сохранённые remote refs без
fetch. Сервер не принимает произвольные команды, пути или адреса репозиториев
от модели. Обновление серверного клона выполняется вручную.

```powershell
go test ./projects/deepseek-client/gitmcp -v
```

Тест создаёт временный Git-клон и HTTP MCP-сервер на localhost, проверяет токен,
список и вызов инструментов, ошибки аргументов и использование результата циклом
агента со сценарной моделью. Он не обращается к внешним API. Реальный VPS,
SSH-туннель, systemd, HTTPS proxy и live модель проверяются при вашем развёртывании.
