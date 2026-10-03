# День 23: реранкинг, фильтрация и query rewrite

Корпус: полные RFC 2324, RFC 6585 и RFC 7168, 27 исходных страниц; структурный индекс дня 21.
Настройки: до 20 кандидатов → cosine ≥ 0.49 → multilingual cross-encoder → raw logit ≥ -3 → до 5 фрагментов.
Пороги выбраны на четырёх отдельных калибровочных вопросах: два положительных, два вне темы. Тестовые вопросы при выборе не использовались.
Порог logit — округлённая середина между самым высоким отрицательным score и самым низким score релевантного положительного фрагмента. См. calibration.json.
Rewrite удаляет только вводные и просьбы о кратком формате; исходный вопрос остаётся в запросе LLM. API для rewrite не используется.

## Качество поиска

10 вопросов с эталоном и 4 без ответа. Hit/MRR считаются только для первых 10.
Precision здесь строгая: фрагмент должен содержать полную эталонную цитату из ожидаемого source. Другие полезные фрагменты могут считаться нерелевантными.

| Режим | Hit@5 | MRR@5 | Evidence precision | Ложные отказы /10 | Отклонено без ответа /4 |
|---|---:|---:|---:|---:|---:|
| baseline | 1.00 | 0.95 | 0.200 | 0 | 0 |
| rerank | 1.00 | 0.95 | 0.200 | 0 | 0 |
| cosine_only | 1.00 | 0.95 | 0.200 | 0 | 0 |
| filtered | 1.00 | 0.95 | 0.317 | 0 | 2 |
| enhanced | 1.00 | 0.90 | 0.315 | 0 | 2 |

Сам cosine-порог не отклонил вопросы без ответа. Logit-фильтр отклонил вопросы про Луну и чемпионат мира, но оставил фрагменты для вопросов об AES и коммерческой цене кофейника.
Все эталонные ответы сохранились. Rewrite не повысил Hit и снизил MRR с 0.95 до 0.90: его можно отключить командой `/rag rewrite off`.
Метод не определяет полноту знаний базы: вопрос по её теме без ответа может пройти фильтр. Здесь LLM правильно отказалась от выдумывания фактов.

## Ответы настоящего DeepSeek

Прогон через текущий Go-адаптер клиента и его реальный Python worker. Изолированное состояние, свежий контекст для каждого вопроса, без истории и дополнительных инструментов.
Модель deepseek-flash, temperature=0, thinking disabled, максимум 512 выходных токенов. 28 одиночных вызовов без повторов: baseline и enhanced на одних и тех же 14 вопросах.
Содержательная проверка всех сохранённых ответов ассистентом (зафиксирована с SHA-256 в answer-review.json): оба режима дали 10/10 верных фактических ответов и 4/4 корректных сообщений об отсутствии данных. Дополнительного прироста точности ответов на этом наборе нет.
Ниже отдельно проверяются точные ID цитат и наличие эталонной цитаты в упомянутом фрагменте. Это проверка конкретного набора, не универсальная семантическая валидация всех утверждений.

| Режим | Входные токены | Выходные токены | Среднее фрагментов | Фактические ответы с эталоном в цитируемом чанке |
|---|---:|---:|---:|---:|
| baseline | 31990 | 845 | 5.00 | 10/10 |
| enhanced | 25284 | 874 | 3.50 | 10/10 |

Сокращение входных токенов: 21.0%. Разрешённый бюджет: $0.20; консервативно зарезервировано $0.1147422; оценка по максимальному тарифу: $0.0192450.
Тарифы проверены 2026-10-03: $0.30/M input cache miss, $0.006/M cache hit, $1.20/M output. Это верхняя тарифная оценка по возвращённому usage, не выписка о списании.
Источник тарифа: [официальная страница DeepSeek](https://api-docs.deepseek.com/quick_start/pricing/).

## Контрольные вопросы и сравнение

### Q01

Please, tell me, Which method discovers metadata about a brewed coffee resource? Answer briefly.

**Ожидание:** discovered using the PROPFIND method

**Источники:** rfc2324.md

**baseline** (5 фрагментов): The PROPFIND method discovers metadata about the brewed coffee resource [rfc2324.md#313573f7d72388992e98ea1f3acca2924c6f5797961da1e0cfdb5a76077a0cd8].

**enhanced** (4 фрагментов): The PROPFIND method discovers metadata about the brewed resource [rfc2324.md#313573f7d72388992e98ea1f3acca2924c6f5797961da1e0cfdb5a76077a0cd8].

### Q02

Please, tell me, Which method tells the coffee pot that enough milk has been added? Answer briefly.

**Ожидание:** the "WHEN" method has been added to HTCPCP

**Источники:** rfc2324.md

**baseline** (5 фрагментов): The **WHEN** method tells the coffee pot that enough milk has been added [rfc2324.md#ebeb735ad069845816100d378b606edd19053b1153a0d3bbe0c0918a91e02615].

**enhanced** (2 фрагментов): The **WHEN** method tells the coffee pot that enough milk has been added [rfc2324.md#ebeb735ad069845816100d378b606edd19053b1153a0d3bbe0c0918a91e02615].

### Q03

Please, tell me, Which protocol synchronizes the clocks of coffee pots? Answer briefly.

**Ожидание:** Coffee pots SHOULD use the Network Time Protocol

**Источники:** rfc2324.md

**baseline** (5 фрагментов): Coffee pots SHOULD use the Network Time Protocol (NTP) to synchronize their clocks to a globally accurate time standard [rfc2324.md#c841e7fcb15b3e23e063613761dad158cf11bb5e75cf58e0f6883a75738feabe].

**enhanced** (5 фрагментов): The Network Time Protocol (NTP) synchronizes coffee pot clocks, according to RFC 2324 [rfc2324.md#c841e7fcb15b3e23e063613761dad158cf11bb5e75cf58e0f6883a75738feabe].

### Q04

Please, tell me, What content type requests tea from a TEA-capable pot? Answer briefly.

**Ожидание:** MUST be "message/teapot"

**Источники:** rfc7168.md

**baseline** (5 фрагментов): The Content-Type is `message/teapot` [rfc7168.md#81cb1b3c60eb63eb0eee2e358b41ceb1ab87a3f3b27626ad12c3b51a93b1d1f5].

**enhanced** (5 фрагментов): The content type is `message/teapot` [rfc7168.md#81cb1b3c60eb63eb0eee2e358b41ceb1ab87a3f3b27626ad12c3b51a93b1d1f5].

### Q05

Please, tell me, Which sugar-type additions are permitted by HTCPCP-TEA? Answer briefly.

**Ожидание:** sugar-type      = ( "Sugar" | "Xylitol" | "Stevia" )

**Источники:** rfc7168.md

**baseline** (5 фрагментов): HTCPCP-TEA permits these sugar-type additions: "Sugar", "Xylitol", and "Stevia" [rfc7168.md#780bc9a67735785ea1ef3a368f275d45a12fae7b162c274cbf7a570d5314a69b].

**enhanced** (5 фрагментов): HTCPCP-TEA permits these sugar-type additions: **Sugar**, **Xylitol**, and **Stevia** [rfc7168.md#780bc9a67735785ea1ef3a368f275d45a12fae7b162c274cbf7a570d5314a69b].

### Q06

Please, tell me, How should a teapot measure beverage strength? Answer briefly.

**Ожидание:** detection of the opacity of the beverage

**Источники:** rfc7168.md

**baseline** (5 фрагментов): A TEA-capable pot should measure beverage strength by detecting the opacity of the beverage currently under brew [rfc7168.md#4a4b36682595be4967b9f4680d67222ea52d77a59b5ca86263c062d6bac91053].

**enhanced** (1 фрагментов): A teapot should measure beverage strength by detecting the opacity of the beverage currently under brew [rfc7168.md#4a4b36682595be4967b9f4680d67222ea52d77a59b5ca86263c062d6bac91053].

### Q07

Please, tell me, Which status code requires a conditional request to prevent lost updates? Answer briefly.

**Ожидание:** The 428 status code indicates that the origin server requires the request to be conditional.

**Источники:** rfc6585.md

**baseline** (5 фрагментов): The **428 Precondition Required** status code indicates that the origin server requires the request to be conditional, and its typical use is to avoid the "lost update" problem [rfc6585.md#ca5ec7f63e53792ca01f7cff347d159b3174d4d14cb2f54ea4dab9c9f8fb53cf].

**enhanced** (5 фрагментов): The **428 Precondition Required** status code indicates that the origin server requires the request to be conditional, and its typical use is to avoid the "lost update" problem [rfc6585.md#ca5ec7f63e53792ca01f7cff347d159b3174d4d14cb2f54ea4dab9c9f8fb53cf].

### Q08

Please, tell me, May HTTP 429 responses include a Retry-After header? Answer briefly.

**Ожидание:** MAY include a Retry-After header

**Источники:** rfc6585.md

**baseline** (5 фрагментов): Yes. HTTP 429 responses MAY include a Retry-After header indicating how long to wait before making a new request [rfc6585.md#34df5f0df9993a9326c2dd285d7b40b97cbca62b046071353e791988705d0dfd].

**enhanced** (5 фрагментов): Yes. RFC 6585 says a 429 response "MAY include a Retry-After header indicating how long to wait before making a new request" [rfc6585.md#34df5f0df9993a9326c2dd285d7b40b97cbca62b046071353e791988705d0dfd].

### Q09

Please, tell me, May a cache store an HTTP 431 response? Answer briefly.

**Ожидание:** Responses with the 431 status code MUST NOT be stored by a cache.

**Источники:** rfc6585.md

**baseline** (5 фрагментов): No. Responses with the 431 status code MUST NOT be stored by a cache [rfc6585.md#72649411207e74ce22e6992c9e6b618da6dc6dd4cff0c500b0e216317c095f75].

**enhanced** (5 фрагментов): No. Responses with the 431 status code MUST NOT be stored by a cache. [rfc6585.md#72649411207e74ce22e6992c9e6b618da6dc6dd4cff0c500b0e216317c095f75]

### Q10

Please, tell me, Should origin servers generate HTTP status 511? Answer briefly.

**Ожидание:** The 511 status SHOULD NOT be generated by origin servers

**Источники:** rfc6585.md

**baseline** (5 фрагментов): No. The 511 status SHOULD NOT be generated by origin servers; it is intended for use by intercepting proxies that control network access [rfc6585.md#607d1be6fe1596aa99b2873c2a46778779302f22b439600e917b06db7aa4aab1].

**enhanced** (5 фрагментов): No. The 511 status SHOULD NOT be generated by origin servers; it is intended for use by intercepting proxies that control network access [rfc6585.md#607d1be6fe1596aa99b2873c2a46778779302f22b439600e917b06db7aa4aab1].

### Q11

Please, tell me, What is the distance from Earth to the Moon? Answer briefly.

**Ожидание:** The supplied documents do not specify this; say evidence is missing, without invented facts or citations.

**Источники:** Подтверждающего источника в корпусе нет.

**baseline** (5 фрагментов): The selected knowledge base has no supporting evidence for the distance from Earth to the Moon. The retrieved excerpts are from RFC documents about HTTP status codes and the Hyper Text Coffee Pot Control Protocol, and none contain that information.

**enhanced** (0 фрагментов): The selected knowledge base has no supporting evidence for this question, so I can't provide a cited answer from it.

### Q12

Please, tell me, Who won the 2022 football World Cup? Answer briefly.

**Ожидание:** The supplied documents do not specify this; say evidence is missing, without invented facts or citations.

**Источники:** Подтверждающего источника в корпусе нет.

**baseline** (5 фрагментов): The selected knowledge base has no supporting evidence about the 2022 football World Cup. The retrieved excerpts are from RFC 2324 (HTCPCP/1.0) and concern coffee pot protocol topics, not football.

**enhanced** (0 фрагментов): The selected knowledge base has no supporting evidence for this question, so I can't provide a document-based answer or citation.

### Q13

Please, tell me, Which AES encryption key length does HTCPCP-TEA mandate? Answer briefly.

**Ожидание:** The supplied documents do not specify this; say evidence is missing, without invented facts or citations.

**Источники:** Подтверждающего источника в корпусе нет.

**baseline** (5 фрагментов): The selected knowledge base has no supporting evidence for an AES encryption key length mandated by HTCPCP-TEA. The retrieved RFC 7168 excerpts cover protocol additions, response codes, and security considerations, but none mention AES or any encryption key length.

**enhanced** (5 фрагментов): The selected knowledge base has no supporting evidence for an AES encryption key length mandated by HTCPCP-TEA. The retrieved excerpts from RFC 7168 cover response codes, the "/" URI, and brewing behavior, but none mention AES or encryption key lengths.

### Q14

Please, tell me, What is the exact commercial price of a compliant HTCPCP coffee pot? Answer briefly.

**Ожидание:** The supplied documents do not specify this; say evidence is missing, without invented facts or citations.

**Источники:** Подтверждающего источника в корпусе нет.

**baseline** (5 фрагментов): The selected knowledge base has no supporting evidence for a commercial price. The excerpts are RFC 2324 and RFC 7168, which describe the HTCPCP protocol and its tea extension, but contain no pricing or commercial product information.

**enhanced** (2 фрагментов): The selected knowledge base has no supporting evidence for a commercial price. The excerpts from [rfc7168.md#181e1f90ac27f083d11e36665c39c9825de675e4bafe7178bb540ceb37be690d] and [rfc2324.md#1e3099e6785412945a3b0396f5ea17f2ab49606c7da458dd34813f41071339d0] describe protocol behavior and rationale, not pricing.

## Воспроизведение

Локальный поиск: из корня репозитория запустить `python examples/rag-filtering/compare.py --model-cache ПУТЬ_К_ГОТОВОМУ_CACHE` подготовленным Python из RAG-окружения. Нужны существующий индекс дня 21 и локальный Ollama с прежним nomic-embed-text.
Сформировать этот отчёт без API: `python examples/rag-filtering/report.py`.
Оплачиваемый прогон — только opt-in `TestHomework23RealDeepSeek` с HW23_PAID_TEST=1, HW23_BUDGET_USD, HW23_OUTPUT, HW23_CORPUS и DEEPSEEK_RAG_PYTHON / DEEPSEEK_RAG_MODEL_CACHE. Нужен официальный Flash-профиль и новое разрешение на API. Существующий answers.json не перезаписывается; повторного запуска в рамках этой проверки не было.
Настройки в TUI: `/rag baseline`, `/rag enhanced`, `/rag set 20 5`, `/rag threshold 0.49`, `/rag score-threshold -3`, `/rag rewrite off`.
Малый набор не доказывает качество на другой базе. Пороги необходимо перекалибровать, а уменьшение контекста иногда может убрать полезные детали.
