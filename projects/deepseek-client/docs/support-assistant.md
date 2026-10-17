# Support assistant

[TaskDesk support service: setup, sample CRM, FAQ and verification](../../../examples/support-assistant/README.md).

Запуск из корня: Start-Support-Assistant.cmd -BudgetUsd 0.20.
Остановка: Start-Support-Assistant.ps1 -Stop.

Команда cmd/support-assistant запускает loopback HTTP UI и отдельный
MCP stdio-процесс с закреплённым пользователем. Пакет support содержит JSON CRM,
TF-IDF FAQ retrieval, HTTP API и учёт бюджета. Генерация и проверка цитат используют
существующие llm и agent. Данные и история основного TUI не изменяются.

Продукт TaskDesk и данные пользователей/тикетов — синтетическая демонстрация.
