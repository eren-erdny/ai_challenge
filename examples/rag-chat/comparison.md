# День 25: два длинных диалога с RAG и памятью

Реальный DeepSeek Flash через submit/update настоящего BubbleTea TUI; два публичных сценария по 12 вопросов. На каждой реплике новый локальный поиск по полным RFC 2324/6585/7168. История сохраняется полностью, в LLM передаются последние 4 сообщения и отдельная память задачи. Пользователь явно задаёт память командами, автоматического извлечения нет.

Поиск: 20 кандидатов → cosine ≥ 0.49 → reranker logit ≥ -3 → до 5 чанков; rewrite включён. JSON output, temperature 0, thinking disabled, максимум 768 выходных токенов; без инструментов и дополнительных моделей.

| Реплика | Ожидание | Цитат | История после | Память | Полнота |
|---|---|---:|---:|---|---|
| http-api-01 | Too many requests in a given time / rate limiting. | 2 | 2 | цель/ограничения/термины сохранены | полная |
| http-api-02 | 429 responses MUST NOT be cached. | 1 | 4 | цель/ограничения/термины сохранены | полная |
| http-api-03 | No: 429 MAY include Retry-After; do not turn MAY into MUST. | 1 | 6 | цель/ограничения/термины сохранены | полная |
| http-api-04 | The origin server requires the request to be conditional. | 2 | 8 | цель/ограничения/термины сохранены | полная |
| http-api-05 | 428 responses MUST NOT be cached. | 1 | 10 | цель/ограничения/термины сохранены | полная |
| http-api-06 | Header fields are too large; request may be retried after reducing them. | 2 | 12 | цель/ограничения/термины сохранены | частичная |
| http-api-07 | Use the corrected term: 431, not the old 429 meaning. | 2 | 14 | цель/ограничения/термины сохранены | полная |
| http-api-08 | The corrected signal is 431; it MUST NOT be cached. | 1 | 16 | цель/ограничения/термины сохранены | полная |
| http-api-09 | It MAY resubmit after reducing request header fields. | 1 | 18 | цель/ограничения/термины сохранены | полная |
| http-api-10 | It SHOULD specify which header field was too large. | 1 | 20 | цель/ограничения/термины сохранены | полная |
| http-api-11 | Client needs authentication to gain network access. | 2 | 22 | цель/ограничения/термины сохранены | полная |
| http-api-12 | For the API checklist: 511 responses MUST NOT be cached. | 1 | 24 | цель/ограничения/термины сохранены | полная |
| tea-checklist-01 | message/teapot. | 1 | 2 | цель/ограничения/термины сохранены | полная |
| tea-checklist-02 | Alternates header with available tea entries. | 1 | 4 | цель/ограничения/термины сохранены | полная |
| tea-checklist-03 | Brewing has not commenced; the client must choose further options. | 1 | 6 | цель/ограничения/термины сохранены | полная |
| tea-checklist-04 | When their combination is contrary to drinkers consensus for that tea variety. | 1 | 8 | цель/ограничения/термины сохранены | полная |
| tea-checklist-05 | 503 or 418; preserve the condition and both options. | 1 | 10 | цель/ограничения/термины сохранены | полная |
| tea-checklist-06 | 503 means temporary unavailability of coffee. | 1 | 12 | цель/ограничения/термины сохранены | полная |
| tea-checklist-07 | 418 means a more permanent indication that the pot is a teapot. | 1 | 14 | цель/ограничения/термины сохранены | полная |
| tea-checklist-08 | Sugar, Xylitol, Stevia. | 1 | 16 | цель/ограничения/термины сохранены | полная |
| tea-checklist-09 | Equivalently, but POST remains deprecated. | 1 | 18 | цель/ограничения/термины сохранены | полная |
| tea-checklist-10 | Abort brewing and serve the weaker beverage. | 1 | 20 | цель/ограничения/термины сохранены | полная |
| tea-checklist-11 | No, / does not commence brewing. | 1 | 22 | цель/ограничения/термины сохранены | полная |
| tea-checklist-12 | 503 temporary coffee unavailability or 418 more permanent teapot indication. | 1 | 24 | цель/ограничения/термины сохранены | полная |

23/24 ответов полностью покрывают заранее записанные ожидания. Все 29 утверждений поддержаны привязанными цитатами, все 29 цитат точно совпадают с текущими чанками.

HTTP-06: верно объясняет причину 431, но не добавляет ожидаемое MAY-повторение после уменьшения заголовков. HTTP-07 и HTTP-09 явно содержат это дополнение. Это неполнота ответа, а не выдумка.

Два сохранённых диалога заканчиваются 24 сообщениями каждый. После шестого вопроса переоткрываются manager, history и memory stores и TUI; первая цель переживает окно истории, signal исправлен с 429 на 431, второй чат имеет независимый device. Возврат в первый чат проверен.

## Найденный и исправленный сбой

Первый прогон остановился после 7 платных вызовов: шестой сохранённый экранный ответ повлиял на формат седьмого; модель вернула текст вместо JSON. Валидатор не показал ответ и не сохранил его. Также отдельные ответы превышали ограничение двух утверждений. Обязательная схема и применение памяти перенесены после истории. Полный набор после исправления прошёл; первый неудачный прогон сохранён в initial-attempt.json и включён в расходы. Автоматических повторов внутри клиента нет.

## Границы проверки

Происхождение цитат проверяется программно; смысл, полнота, язык и связь с целью проверены ассистентом отдельно для этого конечного набора (answer-review.json связан SHA-256). Сохранение ограничения в памяти не гарантирует его исполнение любой будущей моделью. Решение не является универсальным semantic verifier и не имеет автоматического извлечения task state.

## Расходы

До записи: 24 успешных + 7 вызовов первого прогона. Резерв $0.1583271 из $0.20; оценка по максимальному тарифу Flash $0.03739258. Это расчёт по usage, не фактическое списание.
[Официальный тариф](https://api-docs.deepseek.com/quick_start/pricing/?tab=case-studies) проверен 2026-10-04. Запись учитывается отдельно и в том же общем бюджете.

## Сохранённые ответы

### http-api-01: What does signal indicate?

The 429 status code indicates that the user has sent too many requests in a given amount of time ("rate limiting"). [q1]
Responses with the 429 status code MUST NOT be stored by a cache. [q2]

- [q1] «The 429 status code indicates that the user has sent too many requests in a given amount of time ("rate limiting").» — `rfc6585.md#34df5f0df9993a9326c2dd285d7b40b97cbca62b046071353e791988705d0dfd`, RFC 6585: Additional HTTP Status Codes / 4. 429 Too Many Requests.
- [q2] «Responses with the 429 status code MUST NOT be stored by a cache.» — `rfc6585.md#34df5f0df9993a9326c2dd285d7b40b97cbca62b046071353e791988705d0dfd`, RFC 6585: Additional HTTP Status Codes / 4. 429 Too Many Requests.

### http-api-02: Can it be stored by a cache?

Responses with the 429 status code MUST NOT be stored by a cache. [q1]

- [q1] «Responses with the 429 status code MUST NOT be stored by a cache.» — `rfc6585.md#34df5f0df9993a9326c2dd285d7b40b97cbca62b046071353e791988705d0dfd`, RFC 6585: Additional HTTP Status Codes / 4. 429 Too Many Requests.

### http-api-03: Is Retry-After mandatory for signal?

No — the response MAY include a Retry-After header indicating how long to wait before making a new request. [q1]

- [q1] «The response representations SHOULD include details explaining the condition, and MAY include a Retry-After header indicating how long to wait before making a new request.» — `rfc6585.md#34df5f0df9993a9326c2dd285d7b40b97cbca62b046071353e791988705d0dfd`, RFC 6585: Additional HTTP Status Codes / 4. 429 Too Many Requests.

### http-api-04: What does HTTP 428 Precondition Required mean?

The 428 status code indicates that the origin server requires the request to be conditional. [q1]
Its typical use is to avoid the "lost update" problem, where a client GETs a resource's state, modifies it, and PUTs it back to the server, when meanwhile a third party has modified the state on the server, leading to a conflict. [q2]

- [q1] «The 428 status code indicates that the origin server requires the request to be conditional.» — `rfc6585.md#ca5ec7f63e53792ca01f7cff347d159b3174d4d14cb2f54ea4dab9c9f8fb53cf`, RFC 6585: Additional HTTP Status Codes / 3. 428 Precondition Required.
- [q2] «Its typical use is to avoid the "lost update" problem, where a client GETs a resource's state, modifies it, and PUTs it back to the server, when meanwhile a third party has modified the state on the server, leading to a conflict.» — `rfc6585.md#ca5ec7f63e53792ca01f7cff347d159b3174d4d14cb2f54ea4dab9c9f8fb53cf`, RFC 6585: Additional HTTP Status Codes / 3. 428 Precondition Required.

### http-api-05: Can that response be cached?

Responses with the 428 status code MUST NOT be stored by a cache. [q1]

- [q1] «Responses with the 428 status code MUST NOT be stored by a cache.» — `rfc6585.md#ca5ec7f63e53792ca01f7cff347d159b3174d4d14cb2f54ea4dab9c9f8fb53cf`, RFC 6585: Additional HTTP Status Codes / 3. 428 Precondition Required.

### http-api-06: What causes HTTP 431 Request Header Fields Too Large?

The 431 status code indicates that the server is unwilling to process the request because its header fields are too large. [q1]
It can be used both when the set of request header fields in total is too large, and when a single header field is at fault. [q2]

- [q1] «The 431 status code indicates that the server is unwilling to process the request because its header fields are too large.» — `rfc6585.md#72649411207e74ce22e6992c9e6b618da6dc6dd4cff0c500b0e216317c095f75`, RFC 6585: Additional HTTP Status Codes / 5. 431 Request Header Fields Too Large.
- [q2] «It can be used both when the set of request header fields in total is too large, and when a single header field is at fault.» — `rfc6585.md#72649411207e74ce22e6992c9e6b618da6dc6dd4cff0c500b0e216317c095f75`, RFC 6585: Additional HTTP Status Codes / 5. 431 Request Header Fields Too Large.

### http-api-07: What does signal indicate now?

The 431 status code indicates that the server is unwilling to process the request because its header fields are too large. [q1]
The request MAY be resubmitted after reducing the size of the request header fields. [q2]

- [q1] «The 431 status code indicates that the server is unwilling to process the request because its header fields are too large.» — `rfc6585.md#72649411207e74ce22e6992c9e6b618da6dc6dd4cff0c500b0e216317c095f75`, RFC 6585: Additional HTTP Status Codes / 5. 431 Request Header Fields Too Large.
- [q2] «The request MAY be resubmitted after reducing the size of the request header fields.» — `rfc6585.md#72649411207e74ce22e6992c9e6b618da6dc6dd4cff0c500b0e216317c095f75`, RFC 6585: Additional HTTP Status Codes / 5. 431 Request Header Fields Too Large.

### http-api-08: Can it be stored by a cache?

Responses with the 431 status code MUST NOT be stored by a cache. [q1]

- [q1] «Responses with the 431 status code MUST NOT be stored by a cache.» — `rfc6585.md#72649411207e74ce22e6992c9e6b618da6dc6dd4cff0c500b0e216317c095f75`, RFC 6585: Additional HTTP Status Codes / 5. 431 Request Header Fields Too Large.

### http-api-09: How can a client retry an HTTP 431 request successfully?

The request MAY be resubmitted after reducing the size of the request header fields. [q1]

- [q1] «The request MAY be resubmitted after reducing the size of the request header fields.» — `rfc6585.md#72649411207e74ce22e6992c9e6b618da6dc6dd4cff0c500b0e216317c095f75`, RFC 6585: Additional HTTP Status Codes / 5. 431 Request Header Fields Too Large.

### http-api-10: For HTTP 431 caused by one large header, what SHOULD the response specify?

When a single header field is at fault, the response representation SHOULD specify which header field was too large. [q1]

- [q1] «In the latter case, the response representation SHOULD specify which header field was too large.» — `rfc6585.md#72649411207e74ce22e6992c9e6b618da6dc6dd4cff0c500b0e216317c095f75`, RFC 6585: Additional HTTP Status Codes / 5. 431 Request Header Fields Too Large.

### http-api-11: What does HTTP 511 Network Authentication Required indicate?

The 511 status code indicates that the client needs to authenticate to gain network access. [q1]
The 511 status SHOULD NOT be generated by origin servers; it is intended for use by intercepting proxies that are interposed as a means of controlling access to the network. [q2]

- [q1] «The 511 status code indicates that the client needs to authenticate to gain network access.» — `rfc6585.md#607d1be6fe1596aa99b2873c2a46778779302f22b439600e917b06db7aa4aab1`, RFC 6585: Additional HTTP Status Codes / 6. 511 Network Authentication Required.
- [q2] «The 511 status SHOULD NOT be generated by origin servers; it is intended for use by intercepting proxies that are interposed as a means of controlling access to the network.» — `rfc6585.md#607d1be6fe1596aa99b2873c2a46778779302f22b439600e917b06db7aa4aab1`, RFC 6585: Additional HTTP Status Codes / 6. 511 Network Authentication Required.

### http-api-12: For our API checklist, can HTTP 511 responses be stored by a cache?

Responses with the 511 status code MUST NOT be stored by a cache. [q1]

- [q1] «Responses with the 511 status code MUST NOT be stored by a cache.» — `rfc6585.md#607d1be6fe1596aa99b2873c2a46778779302f22b439600e917b06db7aa4aab1`, RFC 6585: Additional HTTP Status Codes / 6. 511 Network Authentication Required.

### tea-checklist-01: Which Content-Type requests tea from device?

The Content-Type header of a POST or BREW request sent to a TEA-capable pot MUST be "message/teapot" if tea is to be requested. [q1]

- [q1] «The Content-Type header of a POST or BREW request sent to a TEA-capable pot MUST be "message/teapot" if tea is to be requested.» — `rfc7168.md#81cb1b3c60eb63eb0eee2e358b41ceb1ab87a3f3b27626ad12c3b51a93b1d1f5`, RFC 7168: The Hyper Text Coffee Pot Control Protocol for Tea Efflux Appliances / 3. The "message/teapot" Media Type.

### tea-checklist-02: Which header is required for an HTCPCP-TEA BREW request to the / URI?

For the "/" URI, brewing will not commence; instead, an Alternates header as defined in RFC 2295 MUST be sent, with the available tea bags and/or leaf varieties as entries. [q1]

- [q1] «For the URI "/", brewing will not commence. Instead, an Alternates header as defined in RFC 2295 [RFC2295] MUST be sent, with the available tea bags and/or leaf varieties as entries.» — `rfc7168.md#ae3f3bfc1ce0c56db5c7213adc078a1d47246ed0957ab89c2df87738ddf00491`, RFC 7168: The Hyper Text Coffee Pot Control Protocol for Tea Efflux Appliances / 2. HTCPCP-TEA Protocol Additions / 2.1. BREW and POST Methods / 2.1.1. The "/" URI.

### tea-checklist-03: What does HTTP 300 indicate for an HTCPCP-TEA BREW request to /?

A BREW request to the "/" URI will return an Alternates header indicating the URIs of the available varieties of tea to brew, and it is RECOMMENDED that this response be served with a status code of 300, to indicate that brewing has not commenced and further options must be chosen by the client. [q1]

- [q1] «A BREW request to the "/" URI, as defined in Section 2.1.1, will return an Alternates header indicating the URIs of the available varieties of tea to brew. It is RECOMMENDED that this response be served with a status code of 300, to indicate that brewing has not commenced and further options must be chosen by the client.» — `rfc7168.md#c65f3530bf44184fe60cf4e3c856a35c84a4753f2fcaf9ebbc9611ae9a3eb54b`, RFC 7168: The Hyper Text Coffee Pot Control Protocol for Tea Efflux Appliances / 2. HTCPCP-TEA Protocol Additions / 2.3. Response Codes / 2.3.1. 300 Multiple Options.

### tea-checklist-04: When MAY an HTCPCP-TEA service return 403 for requested additions?

Services that implement the Accept-Additions header field MAY return a 403 status code for a BREW request of a given variety of tea, if the service deems the combination of additions requested to be contrary to the sensibilities of a consensus of drinkers regarding the variety in question. [q1]

- [q1] «Services that implement the Accept-Additions header field MAY return a 403 status code for a BREW request of a given variety of tea, if the service deems the combination of additions requested to be contrary to the sensibilities of a consensus of drinkers regarding the variety in question.» — `rfc7168.md#9b86f8012ed3ae9b5bb4c0781bb61ea848a3627bbcbd577ace6be7c8ca5998e9`, RFC 7168: The Hyper Text Coffee Pot Control Protocol for Tea Efflux Appliances / 2. HTCPCP-TEA Protocol Additions / 2.3. Response Codes / 2.3.2. 403 Forbidden.

### tea-checklist-05: For TEA-capable pots not provisioned to brew coffee, which two codes may be returned?

TEA-capable pots that are not provisioned to brew coffee may return either a status code of 503, indicating temporary unavailability of coffee, or a code of 418 to denote a more permanent indication that the pot is a teapot. [q1]

- [q1] «TEA-capable pots that are not provisioned to brew coffee may return either a status code of 503, indicating temporary unavailability of coffee, or a code of 418 as defined in the base HTCPCP specification to denote a more permanent indication that the pot is a teapot.» — `rfc7168.md#181e1f90ac27f083d11e36665c39c9825de675e4bafe7178bb540ceb37be690d`, RFC 7168: The Hyper Text Coffee Pot Control Protocol for Tea Efflux Appliances / 2. HTCPCP-TEA Protocol Additions / 2.3. Response Codes / 2.3.3. 418 I'm a Teapot.

### tea-checklist-06: Which of those codes indicates temporary unavailability?

A status code of 503 indicates temporary unavailability of coffee. [q1]

- [q1] «either a status code of 503, indicating temporary unavailability of coffee, or a code of 418 as defined in the base HTCPCP specification to denote a more permanent indication that the pot is a teapot.» — `rfc7168.md#181e1f90ac27f083d11e36665c39c9825de675e4bafe7178bb540ceb37be690d`, RFC 7168: The Hyper Text Coffee Pot Control Protocol for Tea Efflux Appliances / 2. HTCPCP-TEA Protocol Additions / 2.3. Response Codes / 2.3.3. 418 I'm a Teapot.

### tea-checklist-07: What does that other code indicate?

A code of 418 denotes a more permanent indication that the pot is a teapot. [q1]

- [q1] «or a code of 418 as defined in the base HTCPCP specification to denote a more permanent indication that the pot is a teapot.» — `rfc7168.md#181e1f90ac27f083d11e36665c39c9825de675e4bafe7178bb540ceb37be690d`, RFC 7168: The Hyper Text Coffee Pot Control Protocol for Tea Efflux Appliances / 2. HTCPCP-TEA Protocol Additions / 2.3. Response Codes / 2.3.3. 418 I'm a Teapot.

### tea-checklist-08: Which sugar-type values are added by HTCPCP-TEA?

HTCPCP-TEA adds the sugar-type values "Sugar", "Xylitol" and "Stevia". [q1]

- [q1] «sugar-type = ( "Sugar" | "Xylitol" | "Stevia" )» — `rfc7168.md#780bc9a67735785ea1ef3a368f275d45a12fae7b162c274cbf7a570d5314a69b`, RFC 7168: The Hyper Text Coffee Pot Control Protocol for Tea Efflux Appliances / 2. HTCPCP-TEA Protocol Additions / 2.2. Modified Header Fields / 2.2.1. The Accept-Additions Header Field.

### tea-checklist-09: How does HTCPCP-TEA treat POST compared with BREW?

POST requests are treated equivalently to BREW requests, but they remain deprecated. [q1]

- [q1] «POST requests are treated equivalently, but they remain deprecated.» — `rfc7168.md#6e437a9fea7d3e9a71c29b7395d208ce043a090eb018b7075cee09bc292ac5e2`, RFC 7168: The Hyper Text Coffee Pot Control Protocol for Tea Efflux Appliances / 2. HTCPCP-TEA Protocol Additions / 2.1. BREW and POST Methods.

### tea-checklist-10: If an HTCPCP-TEA stop request arrives before recommended strength, what MUST device do?

If a BREW command with an entity body containing "stop" is received before the recommended strength is achieved, the pot MUST abort brewing and serve the resultant beverage at lesser strength. [q1]

- [q1] «If a BREW command with an entity body containing "stop" is received before the recommended strength is achieved, the pot MUST abort brewing and serve the resultant beverage at lesser strength.» — `rfc7168.md#4a4b36682595be4967b9f4680d67222ea52d77a59b5ca86263c062d6bac91053`, RFC 7168: The Hyper Text Coffee Pot Control Protocol for Tea Efflux Appliances / 2. HTCPCP-TEA Protocol Additions / 2.1. BREW and POST Methods / 2.1.2. Variety-Specific URIs.

### tea-checklist-11: Does an HTCPCP-TEA request to the / URI start brewing?

For the URI "/", brewing will not commence. [q1]

- [q1] «For the URI "/", brewing will not commence.» — `rfc7168.md#ae3f3bfc1ce0c56db5c7213adc078a1d47246ed0957ab89c2df87738ddf00491`, RFC 7168: The Hyper Text Coffee Pot Control Protocol for Tea Efflux Appliances / 2. HTCPCP-TEA Protocol Additions / 2.1. BREW and POST Methods / 2.1.1. The "/" URI.

### tea-checklist-12: For our checklist, which codes can device use for coffee when it is not provisioned to brew coffee?

A TEA-capable pot that is not provisioned to brew coffee may return either a status code of 503, indicating temporary unavailability of coffee, or a code of 418 to denote a more permanent indication that the pot is a teapot. [q1]

- [q1] «TEA-capable pots that are not provisioned to brew coffee may return either a status code of 503, indicating temporary unavailability of coffee, or a code of 418 as defined in the base HTCPCP specification to denote a more permanent indication that the pot is a teapot.» — `rfc7168.md#181e1f90ac27f083d11e36665c39c9825de675e4bafe7178bb540ceb37be690d`, RFC 7168: The Hyper Text Coffee Pot Control Protocol for Tea Efflux Appliances / 2. HTCPCP-TEA Protocol Additions / 2.3. Response Codes / 2.3.3. 418 I'm a Teapot.
