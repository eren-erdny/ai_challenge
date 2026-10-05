"""Independently verify saved provenance and render the reviewed HW24 results."""
import hashlib
import json
from pathlib import Path

HERE = Path(__file__).resolve().parent
questions = json.loads((HERE / 'questions.json').read_text(encoding='utf-8'))
paid = json.loads((HERE / 'answers.json').read_text(encoding='utf-8'))
review = json.loads((HERE / 'answer-review.json').read_text(encoding='utf-8'))
assert review['answers_sha256'] == hashlib.sha256((HERE / 'answers.json').read_bytes()).hexdigest(), 'Saved answers changed: review them again'
assert len(questions) == len(paid['rows']) == len(review['rows']) == 10
assert paid['calls'] == sum(r['calls'] for r in paid['rows']) <= 10
assert paid['reserved_usd'] <= paid['budget_usd'] <= .20

def norm(s):
    return ' '.join(s.split())

lines = ['# День 24: цитаты, источники и анти-галлюцинации', '',
         'Реальный DeepSeek через Go-клиент, его TUI-команду и Python worker. Публичная база: полные RFC 2324, RFC 6585, RFC 7168; 27 исходных страниц из дня 21.',
         'Для каждого вопроса — свежий контекст, без личной истории, инструментов и дополнительных моделей. Temperature=0, thinking disabled, JSON output, максимум 1024 выходных токена; автоматических повторов нет.',
         'Поиск: 20 кандидатов → cosine ≥ 0.49 → reranker logit ≥ -3 → до 5 фрагментов; rewrite включён. Пороги сохранены с дня 23 и на этом наборе не подбирались.', '',
         '## Проверка 10 вопросов', '',
         '| Вопрос | Ожидание | Источники | Цитаты | Смысл | LLM-вызовы |',
         '|---|---|---:|---:|---|---:|']
facts = unknown = claim_count = quote_count = 0
for q, r, audit in zip(questions, paid['rows'], review['rows']):
    assert q['id'] == r['id'] == audit['id'] and not r.get('error')
    g = r['grounding']
    assert audit['expected_met']
    if q['evidence']:
        assert g['status'] == 'answered' and g['claims'] and g['quotes'] and g['sources']
        chunks = {h['chunk_id']: h for h in r['retrieval']['results']}
        quotes = {quote['id']: quote for quote in g['quotes']}
        assert len(quotes) == len(g['quotes'])
        for quote in quotes.values():
            chunk = chunks[quote['chunk_id']]
            assert norm(quote['text']) in norm(chunk['text'])
            assert chunk['vector_score'] >= .49 and chunk['rerank_score'] >= -3
        for s in g['sources']:
            chunk = chunks[s['chunk_id']]
            assert s['source'] == chunk['source'] and s['section'] == chunk['section']
        assert {s['chunk_id'] for s in g['sources']} == {quote['chunk_id'] for quote in quotes.values()}
        assert len(g['claims']) == len(audit['claims'])
        for i, (claim, verdict) in enumerate(zip(g['claims'], audit['claims']), 1):
            assert claim['quote_ids'] and all(x in quotes for x in claim['quote_ids'])
            assert verdict['index'] == i and verdict['text'] == claim['text'] and verdict['quote_ids'] == claim['quote_ids'] and verdict['supported']
        assert set(quotes) == {x for c in g['claims'] for x in c['quote_ids']}
        facts += 1
    else:
        assert g['status'] == 'unknown' and not g['claims'] and not g['sources'] and not g['quotes'] and g['clarification'] and 'Не знаю' in g['answer']
        unknown += 1
    if g['local']:
        assert r['calls'] == 0 and not r['retrieval']['results']
    else:
        assert r['calls'] == 1 and r['raw_answer']['UsageKnown'] and not r['raw_answer']['UsageIncomplete'] and r['raw_answer']['FinishReason'] == 'stop'
    claim_count += len(g['claims'])
    quote_count += len(g['quotes'])
    lines.append(f"| {q['id']} | {q['expected']} | {len(g['sources'])} | {len(g['quotes'])} | подтверждён / корректный отказ | {r['calls']} |")

peak = sum(r['estimated_peak_usd'] for r in paid['rows'])
stats = dict(factual_answers=facts, correct_abstentions=unknown, factual_answers_with_sources=facts, factual_answers_with_quotes=facts,
             reviewed_claims=claim_count, supported_claims=claim_count, exact_quotes=quote_count, calls=paid['calls'],
             input_tokens=sum(r['raw_answer']['PromptTokens'] for r in paid['rows']), output_tokens=sum(r['raw_answer']['CompletionTokens'] for r in paid['rows']),
             budget_usd=paid['budget_usd'], reserved_usd=paid['reserved_usd'], estimated_peak_usd=peak)
(HERE / 'stats.json').write_text(json.dumps(stats, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
lines += ['', f'Результат: {facts}/8 содержательных ответов имеют источники и цитаты; проверены все {claim_count} утверждений и {quote_count} дословных цитат. Два вопроса без ответа получили «не знаю» с уточнением.',
          'Отказы намеренно не содержат источников и цитат: подставлять нерелевантный фрагмент как подтверждение отсутствующего факта было бы ошибкой. Поэтому наличие источников оценивается для содержательных ответов отдельно от корректных отказов.',
          'Q09: поиск не оставил фрагментов — локальный ответ, 0 LLM-вызовов. Q10: поиск нашёл 5 близких к теме фрагментов, но в них нет обязательной длины AES-ключа — модель отказалась угадывать.', '',
          '## Что проверяет клиент', '',
          '- Единственный JSON-объект заданной схемы, без лишних полей, повторяющихся ключей и обрезанной генерации.',
          '- Каждое утверждение ссылается на существующие цитаты; каждая цитата используется.',
          '- Chunk принадлежит текущему поиску и проходит его пороги; source и section берутся из локальных метаданных.',
          '- Цитата — точный непрерывный фрагмент чанка после нормализации пробелов. Регистр, числа, знаки и отрицания не исправляются.',
          '- Невалидный ответ не показывается и не сохраняется в историю. При пустом/слабом контексте нет генерации, инструментов, сжатия или аудита инвариантов.', '',
          'Проверка происхождения цитат не гарантирует логическую правильность перефразирования. Смысл всех сохранённых утверждений проверен ассистентом по привязанным цитатам и разделам; review связан с answers.json через SHA-256. Это проверка данного набора, не гарантия любых будущих ответов. Дополнительный LLM-судья не использовался.', '',
          '## Бюджет', '',
          f"{paid['calls']} платных одиночных вызовов, вход {stats['input_tokens']} токенов, выход {stats['output_tokens']} токенов. Разрешено ${paid['budget_usd']:.2f}; до вызовов зарезервировано ${paid['reserved_usd']:.7f}.",
          f'Оценка по максимальному тарифу Flash: ${peak:.8f}; это оценка по usage, не точное списание. [Тариф](https://api-docs.deepseek.com/quick_start/pricing/) проверен 2026-10-03.', '',
          '## Сохранённые ответы', '']
for q, r in zip(questions, paid['rows']):
    lines += [f"### {q['id']}: {q['query']}", '', f"Ожидание: {q['expected']}", '', r['grounding']['answer'], '']
    for quote in r['grounding']['quotes']:
        source = next(s for s in r['grounding']['sources'] if s['chunk_id'] == quote['chunk_id'])
        lines += [f"- [{quote['id']}] «{quote['text']}» — `{source['source']}#{source['chunk_id']}`, раздел {source['section']}."]
    if r['grounding']['clarification']:
        lines += [r['grounding']['clarification']]
    lines += ['']
(HERE / 'comparison.md').write_text('\n'.join(lines).rstrip() + '\n', encoding='utf-8')
print(json.dumps(stats, ensure_ascii=False, indent=2))
