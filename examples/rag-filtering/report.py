"""Render the saved retrieval/paid artifacts; never performs API calls."""
import json
import hashlib
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parent
questions = json.loads((ROOT / 'questions.json').read_text(encoding='utf-8'))
retrieval = json.loads((ROOT / 'retrieval-comparison.json').read_text(encoding='utf-8'))
paid = json.loads((ROOT / 'answers.json').read_text(encoding='utf-8'))
review = json.loads((ROOT / 'answer-review.json').read_text(encoding='utf-8'))
assert review['answers_sha256'] == hashlib.sha256((ROOT / 'answers.json').read_bytes()).hexdigest(), 'Answers changed: review the new run before rendering'
assert paid['calls'] == len(paid['rows']) == 28
assert all(r['answer']['UsageKnown'] and not r['answer']['UsageIncomplete'] and not r.get('error') and r['answer']['FinishReason'] != 'length' for r in paid['rows'])
lines = ['# День 23: реранкинг, фильтрация и query rewrite', '',
         'Корпус: полные RFC 2324, RFC 6585 и RFC 7168, 27 исходных страниц; структурный индекс дня 21.',
         'Настройки: до 20 кандидатов → cosine ≥ 0.49 → multilingual cross-encoder → raw logit ≥ -3 → до 5 фрагментов.',
         'Пороги выбраны на четырёх отдельных калибровочных вопросах: два положительных, два вне темы. Тестовые вопросы при выборе не использовались.',
         'Порог logit — округлённая середина между самым высоким отрицательным score и самым низким score релевантного положительного фрагмента. См. calibration.json.',
         'Rewrite удаляет только вводные и просьбы о кратком формате; исходный вопрос остаётся в запросе LLM. API для rewrite не используется.', '',
         '## Качество поиска', '',
         '10 вопросов с эталоном и 4 без ответа. Hit/MRR считаются только для первых 10.',
         'Precision здесь строгая: фрагмент должен содержать полную эталонную цитату из ожидаемого source. Другие полезные фрагменты могут считаться нерелевантными.', '',
         '| Режим | Hit@5 | MRR@5 | Evidence precision | Ложные отказы /10 | Отклонено без ответа /4 |',
         '|---|---:|---:|---:|---:|---:|']
for row in retrieval['summary']:
    lines.append(f"| {row['mode']} | {row['hit_at_5']:.2f} | {row['mrr_at_5']:.2f} | {row['evidence_precision']:.3f} | {row['false_abstentions']} | {row['negative_rejections']} |")
lines += ['', 'Сам cosine-порог не отклонил вопросы без ответа. Logit-фильтр отклонил вопросы про Луну и чемпионат мира, но оставил фрагменты для вопросов об AES и коммерческой цене кофейника.',
          'Все эталонные ответы сохранились. Rewrite не повысил Hit и снизил MRR с 0.95 до 0.90: его можно отключить командой `/rag rewrite off`.',
          'Метод не определяет полноту знаний базы: вопрос по её теме без ответа может пройти фильтр. Здесь LLM правильно отказалась от выдумывания фактов.', '',
          '## Ответы настоящего DeepSeek', '',
          'Прогон через текущий Go-адаптер клиента и его реальный Python worker. Изолированное состояние, свежий контекст для каждого вопроса, без истории и дополнительных инструментов.',
          'Модель deepseek-flash, temperature=0, thinking disabled, максимум 512 выходных токенов. 28 одиночных вызовов без повторов: baseline и enhanced на одних и тех же 14 вопросах.',
          'Содержательная проверка всех сохранённых ответов ассистентом (зафиксирована с SHA-256 в answer-review.json): оба режима дали 10/10 верных фактических ответов и 4/4 корректных сообщений об отсутствии данных. Дополнительного прироста точности ответов на этом наборе нет.',
          'Ниже отдельно проверяются точные ID цитат и наличие эталонной цитаты в упомянутом фрагменте. Это проверка конкретного набора, не универсальная семантическая валидация всех утверждений.', '',
          '| Режим | Входные токены | Выходные токены | Среднее фрагментов | Фактические ответы с эталоном в цитируемом чанке |',
          '|---|---:|---:|---:|---:|']
summary = []
for mode in ('baseline', 'enhanced'):
    rows = [r for r in paid['rows'] if r['mode'] == mode]
    supported_citations = 0
    for row in rows:
        q = next(q for q in questions if q['id'] == row['id'])
        citations = re.findall(r'\[([^\]\n]+#[0-9a-f]{64})\]', row['answer']['Content'])
        hits = {h['source'] + '#' + h['chunk_id']: h for h in row['retrieval']['results']}
        assert all(c in hits for c in citations), 'Invented citation ID'
        if q['evidence']:
            supported_citations += int(any(c in hits and any(e['source'] == hits[c]['source'] and ' '.join(e['text'].lower().split()) in ' '.join(hits[c]['text'].lower().split()) for e in q['evidence']) for c in citations))
    inputs = sum(r['answer']['PromptTokens'] for r in rows)
    outputs = sum(r['answer']['CompletionTokens'] for r in rows)
    chunks = sum(len(r['retrieval']['results']) for r in rows)/len(rows)
    summary.append({'mode': mode, 'input_tokens': inputs, 'output_tokens': outputs, 'mean_chunks': chunks, 'supported_citations': supported_citations})
    lines.append(f'| {mode} | {inputs} | {outputs} | {chunks:.2f} | {supported_citations}/10 |')
peak_estimate = sum(r['estimated_peak_usd'] for r in paid['rows'])
reduction = 1 - summary[1]['input_tokens']/summary[0]['input_tokens']
lines += ['', f'Сокращение входных токенов: {reduction:.1%}. Разрешённый бюджет: ${paid["budget_usd"]:.2f}; консервативно зарезервировано ${paid["reserved_usd"]:.7f}; оценка по максимальному тарифу: ${peak_estimate:.7f}.',
          'Тарифы проверены 2026-10-03: $0.30/M input cache miss, $0.006/M cache hit, $1.20/M output. Это верхняя тарифная оценка по возвращённому usage, не выписка о списании.',
          'Источник тарифа: [официальная страница DeepSeek](https://api-docs.deepseek.com/quick_start/pricing/).', '',
          '## Контрольные вопросы и сравнение', '']
for q in questions:
    expected = '; '.join(e['text'] for e in q['evidence']) or q['expected']
    source = ', '.join(e['source'] for e in q['evidence']) or 'Подтверждающего источника в корпусе нет.'
    lines += [f'### {q["id"]}', '', q['query'], '', '**Ожидание:** ' + expected, '', '**Источники:** ' + source, '']
    for mode in ('baseline', 'enhanced'):
        row = next(r for r in paid['rows'] if r['mode'] == mode and r['id'] == q['id'])
        lines += [f'**{mode}** ({len(row["retrieval"]["results"])} фрагментов): ' + row['answer']['Content'], '']
lines += ['## Воспроизведение', '',
          'Локальный поиск: из корня репозитория запустить `python examples/rag-filtering/compare.py --model-cache ПУТЬ_К_ГОТОВОМУ_CACHE` подготовленным Python из RAG-окружения. Нужны существующий индекс дня 21 и локальный Ollama с прежним nomic-embed-text.',
          'Сформировать этот отчёт без API: `python examples/rag-filtering/report.py`.',
          'Оплачиваемый прогон — только opt-in `TestHomework23RealDeepSeek` с HW23_PAID_TEST=1, HW23_BUDGET_USD, HW23_OUTPUT, HW23_CORPUS и DEEPSEEK_RAG_PYTHON / DEEPSEEK_RAG_MODEL_CACHE. Нужен официальный Flash-профиль и новое разрешение на API. Существующий answers.json не перезаписывается; повторного запуска в рамках этой проверки не было.',
          'Настройки в TUI: `/rag baseline`, `/rag enhanced`, `/rag set 20 5`, `/rag threshold 0.49`, `/rag score-threshold -3`, `/rag rewrite off`.',
          'Малый набор не доказывает качество на другой базе. Пороги необходимо перекалибровать, а уменьшение контекста иногда может убрать полезные детали.']
(ROOT / 'comparison.md').write_text('\n'.join(lines) + '\n', encoding='utf-8')
(ROOT / 'answer-stats.json').write_text(json.dumps({'summary': summary, 'input_reduction': reduction, 'estimated_peak_usd': peak_estimate}, ensure_ascii=False, indent=2), encoding='utf-8')
print(json.dumps({'summary': summary, 'estimated_peak_usd': peak_estimate, 'input_reduction': reduction}, indent=2))
