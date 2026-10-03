# Локальная индексация документов — домашка 21

Документы → текст → fixed/structured чанки → embeddings Ollama → Qdrant Local
→ переносимый JSON с векторами и метаданными. Чанки содержат source, title/file,
section и chunk_id. Параметры: 400 токенов, overlap 60; nomic-embed-text.
Сравнение на 10 вопросах и полный корпус из 27 страниц:
[examples/rag-indexing](../../examples/rag-indexing/README.md).

После установки зависимостей rag-service/requirements.txt и моделей:
`python examples/rag-indexing/build.py --help` из корня репозитория.
`python -m rag index PATH` из rag-service создаёт обе стратегии индекса;
`python -m rag search QUESTION` выполняет локальный поиск. Для embeddings
нужен работающий Ollama, для повторного чтения используются локальные модели.
Индексы/модели/окружения не включены в Git: инструкции и результаты проверки
сохранены рядом с корпусом. Управление базами из клиента добавляется в домашке 22.
