# День 24: цитаты, источники и анти-галлюцинации

Реальный DeepSeek через Go-клиент, его TUI-команду и Python worker. Публичная база: полные RFC 2324, RFC 6585, RFC 7168; 27 исходных страниц из дня 21.
Для каждого вопроса — свежий контекст, без личной истории, инструментов и дополнительных моделей. Temperature=0, thinking disabled, JSON output, максимум 1024 выходных токена; автоматических повторов нет.
Поиск: 20 кандидатов → cosine ≥ 0.49 → reranker logit ≥ -3 → до 5 фрагментов; rewrite включён. Пороги сохранены с дня 23 и на этом наборе не подбирались.

## Проверка 10 вопросов

| Вопрос | Ожидание | Источники | Цитаты | Смысл | LLM-вызовы |
|---|---|---:|---:|---|---:|
| Q01 | WHEN. Do not confuse it with BREW or PROPFIND. | 1 | 2 | подтверждён / корректный отказ | 1 |
| Q02 | message/teapot is required, not message/coffeepot. | 2 | 2 | подтверждён / корректный отказ | 1 |
| Q03 | Sugar, Xylitol and Stevia; do not add alternatives. | 1 | 2 | подтверждён / корректный отказ | 1 |
| Q04 | Network Time Protocol (NTP), a SHOULD recommendation. | 1 | 1 | подтверждён / корректный отказ | 1 |
| Q05 | 428 Precondition Required; explain the conditional request without confusing it with 429. | 2 | 3 | подтверждён / корректный отказ | 1 |
| Q06 | Yes, MAY include it, not MUST. Header indicates how long to wait. | 1 | 3 | подтверждён / корректный отказ | 1 |
| Q07 | No. Responses with 431 MUST NOT be stored by a cache; preserve the negation. | 1 | 1 | подтверждён / корректный отказ | 1 |
| Q08 | For TEA-capable pots not provisioned to brew coffee, either 503 (temporary unavailability) or 418 (more permanent teapot indication) may be returned. Do not claim 418 is mandatory. | 1 | 1 | подтверждён / корректный отказ | 1 |
| Q09 | Unknown + clarification. No fabricated astronomy answer, sources or quotes. Empty context must result in zero LLM calls. | 0 | 0 | подтверждён / корректный отказ | 0 |
| Q10 | Unknown + clarification even if topic-related chunks are found. Do not invent a key length or cite unrelated security text as proof. | 0 | 0 | подтверждён / корректный отказ | 1 |

Результат: 8/8 содержательных ответов имеют источники и цитаты; проверены все 16 утверждений и 15 дословных цитат. Два вопроса без ответа получили «не знаю» с уточнением.
Отказы намеренно не содержат источников и цитат: подставлять нерелевантный фрагмент как подтверждение отсутствующего факта было бы ошибкой. Поэтому наличие источников оценивается для содержательных ответов отдельно от корректных отказов.
Q09: поиск не оставил фрагментов — локальный ответ, 0 LLM-вызовов. Q10: поиск нашёл 5 близких к теме фрагментов, но в них нет обязательной длины AES-ключа — модель отказалась угадывать.

## Что проверяет клиент

- Единственный JSON-объект заданной схемы, без лишних полей, повторяющихся ключей и обрезанной генерации.
- Каждое утверждение ссылается на существующие цитаты; каждая цитата используется.
- Chunk принадлежит текущему поиску и проходит его пороги; source и section берутся из локальных метаданных.
- Цитата — точный непрерывный фрагмент чанка после нормализации пробелов. Регистр, числа, знаки и отрицания не исправляются.
- Невалидный ответ не показывается и не сохраняется в историю. При пустом/слабом контексте нет генерации, инструментов, сжатия или аудита инвариантов.

Проверка происхождения цитат не гарантирует логическую правильность перефразирования. Смысл всех сохранённых утверждений проверен ассистентом по привязанным цитатам и разделам; review связан с answers.json через SHA-256. Это проверка данного набора, не гарантия любых будущих ответов. Дополнительный LLM-судья не использовался.

## Бюджет

9 платных одиночных вызовов, вход 21905 токенов, выход 2015 токенов. Разрешено $0.20; до вызовов зарезервировано $0.0457776.
Оценка по максимальному тарифу Flash: $0.00868844; это оценка по usage, не точное списание. [Тариф](https://api-docs.deepseek.com/quick_start/pricing/) проверен 2026-10-03.

## Сохранённые ответы

### Q01: Which method tells the coffee pot that enough milk has been added?

Ожидание: WHEN. Do not confuse it with BREW or PROPFIND.

The WHEN method tells the coffee pot that sufficient milk has been introduced into the coffee. [q1] [q2]

- [q1] «When coffee is poured, and milk is offered, it is necessary for the holder of the recipient of milk to say "when" at the time when sufficient milk has been introduced into the coffee.» — `rfc2324.md#ebeb735ad069845816100d378b606edd19053b1153a0d3bbe0c0918a91e02615`, раздел RFC 2324: Hyper Text Coffee Pot Control Protocol (HTCPCP/1.0) / 2. HTCPCP Protocol / 2.1. HTCPCP Added Methods / 2.1.4. WHEN method.
- [q2] «For this purpose, the "WHEN" method has been added to HTCPCP. Enough? Say WHEN.» — `rfc2324.md#ebeb735ad069845816100d378b606edd19053b1153a0d3bbe0c0918a91e02615`, раздел RFC 2324: Hyper Text Coffee Pot Control Protocol (HTCPCP/1.0) / 2. HTCPCP Protocol / 2.1. HTCPCP Added Methods / 2.1.4. WHEN method.

### Q02: What content type requests tea from a TEA-capable pot?

Ожидание: message/teapot is required, not message/coffeepot.

The Content-Type header of a POST or BREW request sent to a TEA-capable pot MUST be "message/teapot" if tea is to be requested. [q1]
A TEA-capable pot that receives a BREW message of content type "message/teapot" MUST respond in accordance with the URI requested. [q2]

- [q1] «The Content-Type header of a POST or BREW request sent to a TEA-capable pot MUST be "message/teapot" if tea is to be requested.» — `rfc7168.md#81cb1b3c60eb63eb0eee2e358b41ceb1ab87a3f3b27626ad12c3b51a93b1d1f5`, раздел RFC 7168: The Hyper Text Coffee Pot Control Protocol for Tea Efflux Appliances / 3. The "message/teapot" Media Type.
- [q2] «a TEA-capable pot that receives a BREW message of content type "message/teapot" MUST respond in accordance with the URI requested, as below.» — `rfc7168.md#6e437a9fea7d3e9a71c29b7395d208ce043a090eb018b7075cee09bc292ac5e2`, раздел RFC 7168: The Hyper Text Coffee Pot Control Protocol for Tea Efflux Appliances / 2. HTCPCP-TEA Protocol Additions / 2.1. BREW and POST Methods.

### Q03: Which sugar-type additions are permitted by HTCPCP-TEA?

Ожидание: Sugar, Xylitol and Stevia; do not add alternatives.

HTCPCP-TEA updates the Accept-Additions header field to allow addition-type options including sugar-type. [q1]
The permitted sugar-type additions are "Sugar", "Xylitol", and "Stevia". [q2]

- [q1] «Additions header field defined in the base HTCPCP specification is updated to allow the following options: addition-type = ( "*" | milk-type | syrup-type | sweetener-type | spice-type | alcohol-type | sugar-type ) *( ";" parameter )» — `rfc7168.md#780bc9a67735785ea1ef3a368f275d45a12fae7b162c274cbf7a570d5314a69b`, раздел RFC 7168: The Hyper Text Coffee Pot Control Protocol for Tea Efflux Appliances / 2. HTCPCP-TEA Protocol Additions / 2.2. Modified Header Fields / 2.2.1. The Accept-Additions Header Field.
- [q2] «sugar-type = ( "Sugar" | "Xylitol" | "Stevia" )» — `rfc7168.md#780bc9a67735785ea1ef3a368f275d45a12fae7b162c274cbf7a570d5314a69b`, раздел RFC 7168: The Hyper Text Coffee Pot Control Protocol for Tea Efflux Appliances / 2. HTCPCP-TEA Protocol Additions / 2.2. Modified Header Fields / 2.2.1. The Accept-Additions Header Field.

### Q04: Which protocol synchronizes the clocks of coffee pots?

Ожидание: Network Time Protocol (NTP), a SHOULD recommendation.

Coffee pots SHOULD use the Network Time Protocol [NTP] to synchronize their clocks to a globally accurate time standard. [q1]

- [q1] «Coffee pots SHOULD use the Network Time Protocol [NTP] to synchronize their clocks to a globally accurate time standard.» — `rfc2324.md#c841e7fcb15b3e23e063613761dad158cf11bb5e75cf58e0f6883a75738feabe`, раздел RFC 2324: Hyper Text Coffee Pot Control Protocol (HTCPCP/1.0) / 5. Operational constraints / 5.1. Timing Considerations.

### Q05: Which status code requires a conditional request to prevent lost updates?

Ожидание: 428 Precondition Required; explain the conditional request without confusing it with 429.

The 428 status code indicates that the origin server requires the request to be conditional. [q1]
Its typical use is to avoid the "lost update" problem. [q2]
The 428 status code is optional; clients cannot rely upon its use to prevent "lost update" conflicts. [q3]

- [q1] «The 428 status code indicates that the origin server requires the request to be conditional.» — `rfc6585.md#ca5ec7f63e53792ca01f7cff347d159b3174d4d14cb2f54ea4dab9c9f8fb53cf`, раздел RFC 6585: Additional HTTP Status Codes / 3. 428 Precondition Required.
- [q2] «Its typical use is to avoid the "lost update" problem, where a client GETs a resource's state, modifies it, and PUTs it back to the server, when meanwhile a third party has modified the state on the server, leading to a conflict.» — `rfc6585.md#ca5ec7f63e53792ca01f7cff347d159b3174d4d14cb2f54ea4dab9c9f8fb53cf`, раздел RFC 6585: Additional HTTP Status Codes / 3. 428 Precondition Required.
- [q3] «The 428 status code is optional; clients cannot rely upon its use to prevent "lost update" conflicts.» — `rfc6585.md#fb1f9875bafaeaeab2a28878f11ec7757573545b365e8266812a1230dfb14ca3`, раздел RFC 6585: Additional HTTP Status Codes / 7. Security Considerations / 7.1. 428 Precondition Required.

### Q06: May HTTP 429 responses include a Retry-After header?

Ожидание: Yes, MAY include it, not MUST. Header indicates how long to wait.

HTTP 429 responses MAY include a Retry-After header indicating how long to wait before making a new request. [q1]
The 429 status code indicates that the user has sent too many requests in a given amount of time ("rate limiting"). [q2]
Responses with the 429 status code MUST NOT be stored by a cache. [q3]

- [q1] «The response representations SHOULD include details explaining the condition, and MAY include a Retry-After header indicating how long to wait before making a new request.» — `rfc6585.md#34df5f0df9993a9326c2dd285d7b40b97cbca62b046071353e791988705d0dfd`, раздел RFC 6585: Additional HTTP Status Codes / 4. 429 Too Many Requests.
- [q2] «The 429 status code indicates that the user has sent too many requests in a given amount of time ("rate limiting").» — `rfc6585.md#34df5f0df9993a9326c2dd285d7b40b97cbca62b046071353e791988705d0dfd`, раздел RFC 6585: Additional HTTP Status Codes / 4. 429 Too Many Requests.
- [q3] «Responses with the 429 status code MUST NOT be stored by a cache.» — `rfc6585.md#34df5f0df9993a9326c2dd285d7b40b97cbca62b046071353e791988705d0dfd`, раздел RFC 6585: Additional HTTP Status Codes / 4. 429 Too Many Requests.

### Q07: May HTTP 431 responses be stored by a cache? Preserve the normative requirement.

Ожидание: No. Responses with 431 MUST NOT be stored by a cache; preserve the negation.

Responses with the 431 status code MUST NOT be stored by a cache. [q1]

- [q1] «Responses with the 431 status code MUST NOT be stored by a cache.» — `rfc6585.md#72649411207e74ce22e6992c9e6b618da6dc6dd4cff0c500b0e216317c095f75`, раздел RFC 6585: Additional HTTP Status Codes / 5. 431 Request Header Fields Too Large.

### Q08: According to HTCPCP-TEA, does a TEA-capable pot that cannot brew coffee have to return 418, or is another code allowed? Explain both codes.

Ожидание: For TEA-capable pots not provisioned to brew coffee, either 503 (temporary unavailability) or 418 (more permanent teapot indication) may be returned. Do not claim 418 is mandatory.

TEA-capable pots that are not provisioned to brew coffee may return either a status code of 503 or a code of 418. [q1]
503 indicates temporary unavailability of coffee. [q1]
418 is defined in the base HTCPCP specification to denote a more permanent indication that the pot is a teapot. [q1]

- [q1] «TEA-capable pots that are not provisioned to brew coffee may return either a status code of 503, indicating temporary unavailability of coffee, or a code of 418 as defined in the base HTCPCP specification to denote a more permanent indication that the pot is a teapot.» — `rfc7168.md#181e1f90ac27f083d11e36665c39c9825de675e4bafe7178bb540ceb37be690d`, раздел RFC 7168: The Hyper Text Coffee Pot Control Protocol for Tea Efflux Appliances / 2. HTCPCP-TEA Protocol Additions / 2.3. Response Codes / 2.3.3. 418 I'm a Teapot.

### Q09: What is the distance from Earth to the Moon?

Ожидание: Unknown + clarification. No fabricated astronomy answer, sources or quotes. Empty context must result in zero LLM calls.

Не знаю: в выбранной базе знаний недостаточно подтверждений для ответа.

Уточните вопрос или добавьте документ с нужной информацией.

### Q10: Which AES encryption key length does HTCPCP-TEA mandate?

Ожидание: Unknown + clarification even if topic-related chunks are found. Do not invent a key length or cite unrelated security text as proof.

Не знаю: в выбранной базе знаний недостаточно подтверждений для ответа.

The retrieved excerpts from RFC 7168 do not mention AES encryption or any key length. Please provide a document that discusses AES encryption in HTCPCP-TEA, or clarify your question.
