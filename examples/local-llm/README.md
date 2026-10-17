# День 26: локальная LLM

Задание выполнено на уже установленном Ollama 0.35.0 и уже загруженной
`batiai/gemma4-12b:iq3` (Gemma 4, 11.9B, GGUF). Повторная установка и скачивание
не потребовались. Генерация выполняется локально на NVIDIA GeForce RTX 5070 Ti.

Запуск приложения: дважды нажмите [Start-Local-LLM.cmd](../../Start-Local-LLM.cmd).
Нужны установленный Go и Ollama. Launcher проверяет API, при необходимости запускает
`ollama serve`, скачивает модель только при её отсутствии, прогревает её и собирает
обычный клиент. Используется профиль [ollama_local](profile.example.json).
Конфигурация, память и история этого запуска находятся в `.local-llm/appdata/`;
папка исключена из Git. Обычная пользовательская история не копируется.

В профилях добавлен необязательный `reasoning_effort`. Для этой модели задано
`none`: без этого параметра проба с маленьким лимитом потратила токены на thinking
и вернула пустое содержимое. Поле проходит через TUI, agent, сжатие и HTTP adapter;
если поле отсутствует, в API оно не отправляется.
[Совместимость Ollama с OpenAI API](https://docs.ollama.com/api/openai-compatibility).
Лимит контекста приложения — 4096 токенов, лимит ответа — 512; это не меняет
контекст runner Ollama: `/api/ps` показал 131072. Режим ответа свободный, температура 0.

## Проверка требований

| Требование | Фактическое подтверждение |
|---|---|
| Модель работает локально | `/api/ps`: GGUF, size_vram = size = 6030926807 байт; `ollama ps`: 100% GPU |
| Доступ через HTTP | Настоящие запросы `POST http://127.0.0.1:11434/v1/chat/completions` из клиента |
| Простой запрос | Столица Франции → «Париж» |
| Средняя сложность | 3×80 + 2×35 = 310; скидка 31; итог **279 рублей** |
| Сложный запрос | Python-функция со стеком для (), [] и {}; **12/12** проверок |

[Исходные вопросы](requests.json),
[ответы проверочного прогона](verification-20261010/answers.json),
[проверка результатов](verification-20261010/review.json),
[новые реальные ответы во время записи](recording-20261010/answers.json),
[проверка ответов записи](recording-20261010/review.json).
Каждый прогон: 3 законченных ответа с `finish_reason=stop`, 196 входных и 252
выходных токена по usage Ollama. Все запросы генерации локальные; облачных
вызовов и расходов на платные API нет. Помимо этих двух прогонов выполнены
3 диагностические пробы; одна показала проблему thinking при маленьком лимите.
Проверки launcher дополнительно прогревали модель без запроса генерации текста.

Повторная **офлайн** проверка сохранённых ответов:

```powershell
python examples/local-llm/review.py
python examples/local-llm/review.py examples/local-llm/recording-20261010/answers.json
cd projects/deepseek-client
go test ./...
go vet ./...
```

Новый локальный прогон (создаёт свежую папку; существующие результаты не перезаписывает):

```powershell
cd projects/deepseek-client
$env:HW26_LOCAL_TEST = '1'
$env:HW26_OUTPUT = Join-Path $env:TEMP ('hw26-' + [guid]::NewGuid())
go test -run '^TestHomework26LocalLLM$' -count=1 -v .
```

Интеграционный тест по умолчанию пропущен. Он использует настоящий TUI submit,
agent и HTTP adapter, фиксированный loopback endpoint, отключённый proxy и запрет
redirect. Записанный режим запускает настоящий BubbleTea event loop и View;
ввод автоматизирован, временная оболочка показывает этапы и фактический `/api/ps`.
Для сценария отключены инструменты и RAG, используются публичные учебные вопросы.

Прямой HTTP-запрос без приложения:

```powershell
$body = '{"model":"batiai/gemma4-12b:iq3","reasoning_effort":"none","messages":[{"role":"user","content":"Say hello in one sentence."}],"max_tokens":64,"stream":false}'
Invoke-RestMethod http://127.0.0.1:11434/v1/chat/completions -Method Post -ContentType application/json -Body $body
ollama ps
```

## Видео

[ai_challenge_hw26.mp4](C:/Users/erdni/Videos/ai_challenge_hw26.mp4) —
**1:53.77**, 2560×1440, 60 fps, без озвучки. Полное декодирование: exit code 0.
Просмотрены кадры на 18, 50, 85 и 110 секундах: простой ответ, расчёт, полный код,
итог 3/3. [Технический отчёт](obs-recording.json).
Исходная сцена OBS и оба исходных состояния звука восстановлены.

Проверены `go test ./...`, `go vet ./...`, сборка клиента, тест TUI → provider
для `reasoning_effort`, передача/отсутствие поля в HTTP и запуск launcher с
`/profile` и `/exit` (exit code 0). Коммиты, push и теги не выполнялись.
Модель оставлена запущенной; Ollama может выгрузить её после периода простоя,
следующий запуск launcher снова прогреет её. Проверка трёх заданий не является
универсальной оценкой качества модели.
