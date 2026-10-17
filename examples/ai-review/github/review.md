<!-- deepseek-ai-review -->
## AI-ревью DeepSeek

Head: 766ece2916b483c8501daa21df8b1d0dc6d7c9c8

Base: 0070f58a662addaef40e4182afe425a575dc951b

В рамках области projects/deepseek-client изменён только демонстрационный фикстур-файл prreview/testdata/demo/user.go: удалена проверка u == nil в функции UserName. Согласно retrieved&#95;context, этот файл является демонстрационным фикстуром, а не прикладным кодом, однако документация прямо фиксирует контракт: UserName должен возвращать пустую строку при nil, а удаление guard приводит к панике. Это единственное изменение в диффе и оно создаёт регрессию поведения относительно задокументированного контракта.

Область: projects/deepseek-client. Файлов diff: 1; пропущено: 0. Корпус: 184 файлов, 933 фрагментов; найдено: 8.

### Потенциальные баги

- **[P1] Удалён nil-guard в UserName, что приводит к панике при nil** — projects/deepseek-client/prreview/testdata/demo/user.go:7 (LEFT)

  Триггер: вызов UserName(nil) — например, HTTP-обработчик передаёт отсутствующего аутентифицированного пользователя, представленного nil. После удаления блока 'if u == nil { return "" }' функция сразу выполняет 'return u.Name', то есть разыменовывает nil-указатель, что вызывает panic вместо возврата пустой строки. Это нарушает задокументированный контракт: 'UserName must return an empty string when User is nil' и 'Removing the nil guard causes UserName(nil) to panic'.

  Исправление: Восстановить удалённый guard в начале функции: 'if u == nil { return "" }' перед 'return u.Name', чтобы сохранить задокументированное поведение и избежать паники при nil.

  Основание: projects/deepseek-client/docs/review-demo.md:1–7 (documentation): «UserName must return an empty string when User is nil.»

  Основание: projects/deepseek-client/docs/review-demo.md:1–7 (documentation): «Removing the nil guard causes UserName(nil) to panic.»

  Основание: projects/deepseek-client/prreview/testdata/demo/user.go:1–12 (code): «func UserName(u &#42;User) string { if u == nil { return "" } return u.Name }»

### Архитектурные проблемы

Подтверждённых замечаний этой категории не найдено.

### Рекомендации

Подтверждённых замечаний этой категории не найдено.

Код PR не запускался. Проверены формат, строки diff и наличие цитат; смысл замечаний требует проверки человеком. Отсутствие замечаний не доказывает отсутствие ошибок.
