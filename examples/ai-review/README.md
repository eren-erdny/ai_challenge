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
Это был первоначальный локальный снимок. Теперь выполнена и проверка на GitHub:
[PR №7](https://github.com/eren-erdny/ai_challenge/pull/7),
[Action success](https://github.com/eren-erdny/ai_challenge/actions/runs/38925358936),
[бот-комментарий](https://github.com/eren-erdny/ai_challenge/pull/7#issuecomment-6219003672).

[CI verification](github/verification.json) и [текст ревью](github/review.md) сохранены.
В CI корпус: 184 файла / 933 chunks, найдено 8 chunks; один bug на LEFT:7.
Все цитаты и anchor проверены, опубликованный комментарий совпадает с artifact.
CI: один запрос, резерв $0.0070851, оценка peak $0.0016038.
Итого локальная + CI оценка $0.0023028 в разрешённых $0.20.
После проверки AI_REVIEW_ENABLED=false, лимит на будущий запуск 0.02 USD;
ключ хранится в GitHub Secrets. Демонстрационный PR с намеренной ошибкой не сливать.
Реализация опубликована отдельно от незакоммиченной домашки 31.

## Видео домашки 32

[ai_challenge_hw32.mp4](C:/Users/erdni/Videos/ai_challenge_hw32.mp4) — 169.05 секунды,
2560x1440, 60 fps, без звука. Показ готового реального GitHub-прогона явно
обозначен в кадре; новых запросов DeepSeek и расходов нет.
[Сценарий](recording-scenario.md), [проверка видео](obs-recording.json).
OBS Chromium загружал настоящие страницы GitHub; подписи и viewport настроены
для читаемости. Полный decode и девять кадров проверены, сцена/звук восстановлены.
