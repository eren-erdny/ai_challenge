# Живая проверка AI-ревью

2026-10-16: реальный запрос DeepSeek через review-pr CLI на изолированном
тестовом Git-репозитории. Исходники fixture создаёт
[make_fixture.py](../../scripts/ai-review/make_fixture.py).

- Base документирует UserName(nil) → пустая строка.
- Head удаляет nil guard: UserName(nil) разыменовывает nil.
- RAG нашёл README, docs/api.md и user.go из base.
- Модель нашла один bug на удалённой строке 6 (LEFT), с цитатами docs и кода.
- Формат, anchor и все цитаты прошли валидатор; trigger и fix проверены вручную.
- Разделы архитектуры и рекомендаций пустые: модель не придумала замечания.

[input.json](input.json), [review.json](review.json), [review.md](review.md),
[completion.json](completion.json), [usage.json](usage.json) — сохранённые
подлинные результаты, без API-ключей и пользовательской конфигурации.

Бюджет $0.20, один запрос. Резерв $0.004092; оценка по известным токенам и peak
$0.000699 (806 input + 381 output, cache hit 0). Это не точное списание.
[Тарифы](https://api-docs.deepseek.com/quick_start/pricing/).

Ограничение смысловой проверки: модель пишет «роняя обработчик/сервер».
Доказан panic внутри функции; завершение процесса не доказано. Например,
стандартный net/http обычно перехватывает panic обработчика. Приоритет P1 —
оценка модели. Сохранённый ответ не редактировался после генерации.

Go tests/vet/build, пять offline тестов publisher и actionlint прошли.
Проверка корпуса текущего Git base: 892 chunks, без API-запроса.
Запуск GitHub Actions и комментарий в настоящем PR пока не проверены:
изменения ещё не опубликованы, secret/variables не настраивались.
