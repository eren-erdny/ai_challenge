# Домашка 22: сравнение DeepSeek без RAG и с RAG

[Видео OBS](C:/Users/erdni/Videos/ai_challenge_hw22.mp4) · [10 вопросов](questions.json) · [все ответы, usage и чанки](comparison.json)

Источник: полный [RFC 7168](https://www.rfc-editor.org/rfc/rfc7168.txt), информационный первоапрельский протокол чайника. Исходный текст сохранён с лицензией в corpus/rfc7168.txt.

Дата: 2026-10-02. Использована настроенная deepseek-v4-flash; API возвращал deepseek-flash. Реальный TUI, F2 → создание базы, F2 → 6 для отключения, F2 → 2 для включения. Сценарный ввод автоматизирован. Локальные Ollama embeddings, Qdrant и reranker; реальные ответы DeepSeek. Временный статусный текст обозначает режим и номер вопроса.

Оба режима получили одинаковые вопросы, температуру 0 и лимит 512 выходных токенов. Использован отдельный чат без истории и дополнительных инструментов; каждый вопрос независим. Ожидания и разделы источника составлены до запросов. Повторов не было.

## Итог

| Проверка | Без RAG | С RAG |
|---|---:|---:|
| Автоматический поиск ключевых фраз | 3/10 | 8/10 |
| Полностью правильные ответы после содержательного просмотра | 1/10 | 8/10 |
| Частично правильные ответы с выдуманными дополнениями | 2/10 | 0/10 |
| Правильные фактические ответы с подтверждённым найденным текстом | не применимо | 6/8 |

Содержательный просмотр здесь выполнен Codex, а не независимым экспертом. Полный зачёт требует правильных ключевых фактов без ложных дополнений. Автопроверка только ищет слова и не проверяет отрицание, полноту и ложные дополнения. Поэтому Q01 и Q10 без RAG по автопроверке прошли, но при просмотре получили частичный зачёт.

Все восемь фактических RAG-ответов содержат существующий source#chunk_id. Однако два ответа с отказом ссылаются на неинформативные колонтитулы: совпадение ID ещё не доказывает релевантность источника. Для шести успешных фактических ответов нужное правило действительно есть в сохранённом retrieved_text. Два вопроса без ответа не считаются фактическим покрытием источника.

| ID | Ожидание | Раздел RFC | Без RAG: просмотр | С RAG: просмотр | Комментарий |
|---|---|---|---|---|---|
| Q01 | message/teapot | 3 | Частично | Верно | Правильный message/teapot, но добавлен несуществующий application/teapot. |
| Q02 | Sugar, Xylitol, Stevia | 2.2.1 | Ошибка | Ошибка поиска | Без RAG придуманы sugar/Sugar/sugar? no thanks. С RAG нужный чанк не найден: ответ указывает на колонтитулы. |
| Q03 | 300; Alternates; brewing does not commence | 2.1.1 and 2.3.1 | Ошибка | Верно | Без RAG ошибочно заявлено отсутствие кода и заголовка. С RAG: 300 + Alternates, приготовление не начато. |
| Q04 | Equivalent treatment; POST remains deprecated | 2.1 | Ошибка | Верно | Без RAG ошибочно заявлено, что POST никогда не поддерживался. С RAG: эквивалентен BREW, но deprecated. |
| Q05 | Opacity | 2.1.2 | Ошибка | Ошибка поиска | Оба режима не дали opacity; RAG вернул колонтитулы вместо раздела 2.1.2. |
| Q06 | Abort brewing and serve the beverage at lesser strength | 2.1.2 | Ошибка | Верно | Без RAG ошибочно заявлено отсутствие правила. С RAG: остановить приготовление и подать менее крепкий напиток. |
| Q07 | 503 temporary unavailability; 418 permanent teapot indication | 2.3.3 | Ошибка | Верно | Без RAG: 418 + ошибочный 406. С RAG: 503 + 418. |
| Q08 | 403 Forbidden | 2.3.2 | Ошибка | Верно | Без RAG ошибочный 418. С RAG правильный 403. |
| Q09 | Not specified; do not invent a password | No supporting passage | Верно | Верно | Оба режима не выдумали пароль. Документ проверен целиком: пароля нет. Найденные RAG-чанки сами по себе отсутствие во всём документе не доказывают. |
| Q10 | No TLS version is specified; do not invent one | No supporting passage (section 5 does not define TLS) | Частично | Верно | Без RAG правильно не назвал версию, но выдумал обязательное использование TLS. С RAG осторожно признал отсутствие сведений в найденном контексте; в полном документе версии тоже нет. |

## Все ответы

### Q01

Which Content-Type is required when requesting tea from a TEA-capable pot?

Ожидание: message/teapot. Источник: rfc7168.txt, раздел 3.

**Без RAG:**

RFC 7168 specifies that a TEA-capable pot uses the Content-Type `message/teapot` (or `application/teapot` in some interpretations). However, the RFC actually defines `message/teapot` for HTCPCP-TEA requests.

**С RAG:**

The required Content-Type is "message/teapot" [rfc7168.txt#43f7820c7d1149a53e94ffa0bd7a46f4e7cc2276294df937ddd17ee00f182db8].

### Q02

Which three sugar-type values does HTCPCP-TEA add to Accept-Additions?

Ожидание: Sugar, Xylitol, Stevia. Источник: rfc7168.txt, раздел 2.2.1.

**Без RAG:**

RFC 7168 adds three sugar-type values to Accept-Additions: "sugar", "Sugar", and "sugar? no thanks".

**С RAG:**

The retrieved excerpts only contain page headers/footers from RFC 7168 and do not include the Accept-Additions sugar-type values. Therefore, the three sugar-type values are not specified in the provided evidence. [rfc7168.txt#30c8012da475c7071ac6c8930735dddf3dc5d238f71261f969fdf0625381932a]

### Q03

What status code and response header are recommended for a BREW request to the root URI /?

Ожидание: 300; Alternates; brewing does not commence. Источник: rfc7168.txt, раздел 2.1.1 and 2.3.1.

**Без RAG:**

RFC 7168 does not specify a status code or response header for a BREW request to the root URI /. It is not specified.

**С RAG:**

A BREW request to "/" returns an Alternates header listing available tea varieties; it is RECOMMENDED that the response use status code 300, indicating brewing has not commenced and the client must choose further options [rfc7168.txt#2b4871ab1654bb7aa2d48e6cbfaca84cda7d7edf25e41b2f068efda0cc0d44f8].

### Q04

How are POST requests treated relative to BREW, and are they deprecated?

Ожидание: Equivalent treatment; POST remains deprecated. Источник: rfc7168.txt, раздел 2.1.

**Без RAG:**

RFC 7168 states that POST is not a valid method for the HTCPCP protocol; BREW is the required method for coffee pot requests. POST is not deprecated—it was never supported.

**С RAG:**

POST requests are treated equivalently to BREW requests, but they remain deprecated [rfc7168.txt#2f39253ae324c0e92a893a94d68c86538099b70b4a19d8d53c09fc19717ef29e].

### Q05

What physical property is suggested for measuring the strength of the beverage?

Ожидание: Opacity. Источник: rfc7168.txt, раздел 2.1.2.

**Без RAG:**

RFC 7168 does not specify a physical property for measuring beverage strength. It humorously discusses tea, coffee, and the "HTCPCP" protocol, but defines no such measurement.

**С RAG:**

The retrieved excerpts from RFC 7168 contain only page headers and footers, with no substantive text about measuring beverage strength. Therefore, it is not specified in the provided evidence. [rfc7168.txt#69610406820cc1f2021bd5316cc0ebf7812c8b82572d608765ae45278b1ef3f9]

### Q06

What must a pot do if it receives a stop body before the recommended brewing strength is achieved?

Ожидание: Abort brewing and serve the beverage at lesser strength. Источник: rfc7168.txt, раздел 2.1.2.

**Без RAG:**

RFC 7168 does not specify what a pot must do if it receives a stop body before the recommended brewing strength is achieved. It is not specified.

**С RAG:**

The pot MUST abort brewing and serve the resultant beverage at lesser strength [rfc7168.txt#0c3e161e30324c05a1416f8b7d84df8e8f6d3aa0ec2cc09ae255e8e6d62ae1fa].

### Q07

Which two status codes may a tea-capable pot return when it is not provisioned to brew coffee?

Ожидание: 503 temporary unavailability; 418 permanent teapot indication. Источник: rfc7168.txt, раздел 2.3.3.

**Без RAG:**

RFC 7168 specifies that a tea-capable pot, when asked to brew coffee and not provisioned to do so, may return **418 I'm a teapot** or **406 Not Acceptable**.

**С RAG:**

A tea-capable pot not provisioned to brew coffee may return 503 (temporary unavailability of coffee) or 418 (permanent indication that the pot is a teapot) [rfc7168.txt#d0a681bdec22234438732c7cd139d867927837383bb76b784d7d81efc178d97f].

### Q08

Which status code may be returned when requested tea additions offend the consensus of tea drinkers?

Ожидание: 403 Forbidden. Источник: rfc7168.txt, раздел 2.3.2.

**Без RAG:**

RFC 7168 specifies status code 418 "I'm a teapot" for when tea additions offend the consensus of tea drinkers (e.g., adding milk to a tea that should not have it).

**С RAG:**

RFC 7168 says services implementing Accept-Additions MAY return a 403 status code for a BREW request if the requested additions are contrary to a consensus of drinkers' sensibilities [rfc7168.txt#dfe393264cd9244174858a4eab429beb1db7b404c87b03f04a9f0e5df38cc689].

### Q09

What is the default administrator password for a TEA-capable pot?

Ожидание: Not specified; do not invent a password. Источник: rfc7168.txt, раздел No supporting passage.

**Без RAG:**

RFC 7168 does not specify a default administrator password for a TEA-capable pot. It is not specified.

**С RAG:**

RFC 7168 does not specify a default administrator password for a TEA-capable pot. The retrieved excerpts contain only page headers and footers, with no relevant content [rfc7168.txt#69610406820cc1f2021bd5316cc0ebf7812c8b82572d608765ae45278b1ef3f9].

### Q10

Which exact TLS version does this RFC require for TEA-capable pots?

Ожидание: No TLS version is specified; do not invent one. Источник: rfc7168.txt, раздел No supporting passage (section 5 does not define TLS).

**Без RAG:**

RFC 7168 does not specify any TLS version for TEA-capable pots. It only requires that TEA-capable servers use TLS and that the TEA parameters be sent over the encrypted channel; no particular TLS version is mandated.

**С RAG:**

The retrieved excerpts from RFC 7168 contain only page headers and footers, with no text about TLS requirements for TEA-capable pots. Therefore, based on these excerpts, the TLS version is not specified. [rfc7168.txt#30c8012da475c7071ac6c8930735dddf3dc5d238f71261f969fdf0625381932a]

## Токены и бюджет

Сделано 20 запросов: 14303 входных и 1150 выходных токенов по usage API. Оценка этих запросов по максимальному тарифу Flash: $0.0056709. Это расчёт по usage, не выписка о списании.

Проверенный официальный тариф: [DeepSeek Models & Pricing](https://api-docs.deepseek.com/quick_start/pricing/) — peak $0.30/M uncached input, $0.006/M cached input, $1.20/M output. Использован максимальный тариф независимо от времени запроса.

Общий консервативный резерв с предыдущими тремя запросами: $0.0757301 из разрешённых $0.20. Предыдущий резерв $0.02318 сохранён. Перед каждым вызовом применялись ограничения числа вызовов, входных UTF-8 байтов с запасом на протокол и выходных токенов.

## Ограничения и следующий шаг

RAG улучшил ответы, но не решил проблему двух вопросов: вместо текста выбраны повторяющиеся заголовки/колонтитулы. Перед повторной проверкой стоит отдельно подготовить копию документа без пагинации и оценить релевантность выдачи. В этом запуске корпус не менялся и неудачные ответы не переснимались.

Поисковый отказ не доказывает, что ответа нет во всей базе. Проверка Q09/Q10 включает просмотр полного исходного документа. Стандартный клиент пока не валидирует смысл цитат автоматически.

## Проверка видео

OBS: MP4/H.264, 2560×1440, 60 fps, 224.90 секунд, 21221432 байта. Запись полностью декодирована без ошибок; проверены кадры добавления базы, обычных ответов и ответов с найденными чанками. Исходная сцена и изменённые настройки звука восстановлены. Запись без озвучки.

SHA256 исходного RFC: 9087cd8dd9f00bd9da0fed69b5ab06c8deedb1b845f5e7a6ee13763a59388517
