# Release draft: week-7-preview

Git range: `12acaad5648fab8b45ba636544c8ef24b8987af5` → `8fa5d10eb212796cfb93c2344a3428e3290bb51b`.

AI draft for human review. No tag or release was published.

## Added/changed

- Добавлен помощник по проекту, запускаемый из корня через Start-Developer-Assistant.cmd; в TUI доступны вопросы через /help. [q1]
- Добавлен ассистент поддержки TaskDesk — локальный веб-сервис с FAQ RAG и контекстом пользователей/тикетов через отдельный MCP stdio-процесс. [q2]
- Добавлен ассистент для работы с файлами, который исследует учебный TaskDesk, исправляет README/FAQ и создаёт карту API, не меняя исходный код. [q3]
- Добавлено автоматическое ревью PR через DeepSeek: GitHub Action реагирует на события недрафтового PR с изменениями в projects/deepseek-client. [q4]

## Evidence from selected head

- [q1] `projects/deepseek-client/README.md:1-24` at `8fa5d10eb212796cfb93c2344a3428e3290bb51b`: Из корня монорепозитория запустите [Start-Developer-Assistant.cmd](../../Start-Developer-Assistant.cmd).
- [q2] `projects/deepseek-client/README.md:991-1003` at `8fa5d10eb212796cfb93c2344a3428e3290bb51b`: [TaskDesk support](../../examples/support-assistant/README.md): локальный веб-сервис, FAQ RAG и контекст пользователей/тикетов через отдельный MCP stdio-процесс.
- [q3] `projects/deepseek-client/README.md:991-1003` at `8fa5d10eb212796cfb93c2344a3428e3290bb51b`: Агент самостоятельно исследует учебный TaskDesk, исправляет README/FAQ и создаёт карту API.
- [q4] `projects/deepseek-client/docs/ai-review.md:1-24` at `8fa5d10eb212796cfb93c2344a3428e3290bb51b`: 1. GitHub Action реагирует на opened/synchronize/reopened/ready_for_review для недрафтового PR с изменениями в projects/deepseek-client.
- [q5] `projects/deepseek-client/docs/support-assistant.md:1-14` at `8fa5d10eb212796cfb93c2344a3428e3290bb51b`: Запуск из корня: Start-Support-Assistant.cmd -BudgetUsd 0.20. Остановка: Start-Support-Assistant.ps1 -Stop.
- [q6] `projects/deepseek-client/docs/file-assistant.md:1-24` at `8fa5d10eb212796cfb93c2344a3428e3290bb51b`: ```powershell .\Start-File-Assistant.ps1 -BudgetUsd 0.20 ``` Add `-Verify` to execute both goals through scripted TUI submit/update.
- [q7] `projects/deepseek-client/docs/file-assistant.md:19-28` at `8fa5d10eb212796cfb93c2344a3428e3290bb51b`: The embedded fixture's documentation intentionally contradicts its code. The fixture represents export authorization and metadata; it is not a PDF renderer.
- [q8] `projects/deepseek-client/docs/ai-review.md:109-120` at `8fa5d10eb212796cfb93c2344a3428e3290bb51b`: Для постоянной работы сначала объедините реализацию в целевую базовую ветку и затем явно включите автоматизацию.
