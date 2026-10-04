"""Offline HW25 provenance, persistence, memory and reviewed-answer report.

No API calls. Semantic verdicts are a separate finite manual review, not an
automatic entailment checker. Both failed and successful paid attempts count.
"""
import hashlib
import json
from pathlib import Path

HERE = Path(__file__).resolve().parent
read = lambda name: json.loads((HERE / name).read_text(encoding='utf-8'))
norm = lambda s: ' '.join(s.split())
data, old, review, scenarios = (read(n) for n in
    ('answers.json', 'initial-attempt.json', 'answer-review.json', 'scenarios.json'))
assert review['answers_sha256'] == hashlib.sha256((HERE/'answers.json').read_bytes()).hexdigest()
assert data['earlier_attempt_calls'] == old['provider_calls'] == 7
assert abs(data['earlier_attempt_reserved_usd'] - old['reserved_usd']) < 1e-12
assert data['provider_calls'] == len(data['rows']) == len(review['rows']) == 24
assert data['reopens'] == 2 and data['return_to_first_chat']
assert old['rows'][-1]['error'] and old['rows'][-1]['history_after'] == old['rows'][-1]['history_before'] == 12
assert data['reserved_usd']+old['reserved_usd'] <= data['budget_usd'] <= .20
lines = ['# День 25: два длинных диалога с RAG и памятью', '',
 'Реальный DeepSeek Flash через submit/update настоящего BubbleTea TUI; два публичных сценария по 12 вопросов. '
 'На каждой реплике новый локальный поиск по полным RFC 2324/6585/7168. История сохраняется полностью, '
 'в LLM передаются последние 4 сообщения и отдельная память задачи. '
 'Пользователь явно задаёт память командами, автоматического извлечения нет.', '',
 'Поиск: 20 кандидатов → cosine ≥ 0.49 → reranker logit ≥ -3 → до 5 чанков; rewrite включён. '
 'JSON output, temperature 0, thinking disabled, максимум 768 выходных токенов; без инструментов и дополнительных моделей.', '',
 '| Реплика | Ожидание | Цитат | История после | Память | Полнота |',
 '|---|---|---:|---:|---|---|']
claims = quotes = complete = 0
for scenario in scenarios:
    rows = [r for r in data['rows'] if r['scenario'] == scenario['id']]
    assert len(rows) == len(scenario['questions']) == 12
    for i, (q, r) in enumerate(zip(scenario['questions'], rows)):
        a = next(x for x in review['rows'] if x['id'] == r['id'])
        assert r['id'] == q['id'] and r['query'] == q['query'] and not r.get('error')
        assert r['history_before'] == i*2 and r['history_after'] == (i+1)*2
        assert r['history_messages_sent'] == min(i*2, 4) and r['brief_sent']
        assert r['brief']['goal'] == scenario['goal'] and r['brief']['constraints']['style']
        assert r['reopened'] == (i >= scenario['reopen_after'])
        assert r['brief']['clarifications']['scope']
        if scenario['id'] == 'http-api':
            assert ('429' if i < 6 else '431') in r['brief']['terms']['signal']
        else:
            assert r['brief']['terms']['device'] == 'TEA-capable pot'
            if i >= 6:
                assert r['brief']['clarifications']['focus']
        g = r['grounding']
        assert g['status'] == 'answered' and g['claims'] and g['quotes'] and g['sources']
        assert 'Источники:' in r['visible'] and 1 <= len(g['claims']) <= 2
        hits = {h['chunk_id']:h for h in r['retrieval']['results']}
        assert any(h['source'] == e['source'] and norm(e['text']) in norm(h['text'])
                   for e in q['evidence'] for h in hits.values())
        bound = {quote['id']: quote for quote in g['quotes']}
        assert len(bound) == len(g['quotes'])
        for quote in bound.values():
            h = hits[quote['chunk_id']]
            assert norm(quote['text']) in norm(h['text'])
            assert h['vector_score'] >= .49 and h['rerank_score'] >= -3
        for source in g['sources']:
            h = hits[source['chunk_id']]
            assert source['source'] == h['source'] and source['section'] == h['section']
        assert {s['chunk_id'] for s in g['sources']} == {v['chunk_id'] for v in bound.values()}
        assert len(a['claims']) == len(g['claims'])
        for claim, verdict in zip(g['claims'], a['claims']):
            assert claim['quote_ids'] and set(claim['quote_ids']) <= set(bound)
            assert verdict['text'] == claim['text'] and verdict['quote_ids'] == claim['quote_ids'] and verdict['supported']
        assert set(bound) == {v for c in g['claims'] for v in c['quote_ids']}
        assert a['goal_consistent'] and a['constraints_met']
        assert r['calls'] == 1 and r['raw_answer']['UsageKnown'] and not r['raw_answer']['UsageIncomplete'] and r['raw_answer']['FinishReason'] == 'stop'
        complete += a['expected_met']
        claims += len(g['claims']); quotes += len(g['quotes'])
        lines.append(f"| {r['id']} | {q['expected']} | {len(g['quotes'])} | {r['history_after']} | цель/ограничения/термины сохранены | {'полная' if a['expected_met'] else 'частичная'} |")
peak = sum(r['estimated_peak_usd'] for r in data['rows']+old['rows'])
stats = dict(scenarios=2, turns_per_scenario=12, factual_answers=24,
             answers_with_sources=24, answers_with_quotes=24, fresh_searches=24,
             brief_sent=24, goals_preserved=24, constraints_met=24,
             full_expected_answers=complete, partially_complete=24-complete,
             reviewed_claims=claims, exact_quotes=quotes, reopens=2,
             current_calls=24, earlier_calls=7, total_calls_before_recording=31,
             reserved_before_recording_usd=data['reserved_usd']+old['reserved_usd'],
             estimated_peak_before_recording_usd=peak, budget_usd=.20)
if (HERE/'recording-comparison.json').exists():
    film, film_review, video = (read(n) for n in
        ('recording-comparison.json', 'recording-review.json', 'obs-recording.json'))
    assert film_review['recording_answers_sha256'] == hashlib.sha256((HERE/'recording-comparison.json').read_bytes()).hexdigest()
    assert film['prior_answers_sha256'] == review['answers_sha256']
    assert film['calls'] == 3 and len(film['rows']) == len(film_review['rows']) == 4
    assert film['stores_and_tui_reopened'] and film['second_chat_isolated']
    assert film['prior_reserve_usd'] == stats['reserved_before_recording_usd']
    assert abs(film['total_reserve_usd'] - film['prior_reserve_usd'] - sum(r['reserved_usd'] for r in film['rows'])) < 1e-12
    assert film['total_reserve_usd'] <= film['budget_usd'] == .20
    for r, verdict in zip(film['rows'], film_review['rows']):
        assert r['id'] == verdict['id'] and verdict['goal_consistent'] and verdict['constraints_met']
        assert r['history_after'] == r['history_before']+2 and not r.get('error')
        g = r['grounding']
        hits = {h['chunk_id']:h for h in r['retrieval']['results']}
        if g['status'] == 'answered':
            assert r['calls'] == 1 and r['brief_sent'] and g['sources'] and g['quotes']
            for quote in g['quotes']:
                assert norm(quote['text']) in norm(hits[quote['chunk_id']]['text'])
            for source in g['sources']:
                assert source['source'] == hits[source['chunk_id']]['source'] and source['section'] == hits[source['chunk_id']]['section']
            assert {s['chunk_id'] for s in g['sources']} == {q['chunk_id'] for q in g['quotes']}
            assert {q['id'] for q in g['quotes']} == {v for c in g['claims'] for v in c['quote_ids']}
            assert len(g['claims']) == len(verdict['claims'])
            for c, v in zip(g['claims'], verdict['claims']):
                assert c['text'] == v['text'] and c['quote_ids'] == v['quote_ids'] and v['supported']
        else:
            assert g['status'] == 'unknown' and g['local'] and not hits and r['calls'] == 0 and not r['brief_sent']
            assert not g['sources'] and not g['quotes'] and not g['claims'] and g['clarification']
    assert video['full_decode_exit_code'] == 0 and video['scene_restored'] and video['audio_mutes_restored'] and video['owned_terminal_closed']
    assert abs(video['total_reserve_usd'] - film['total_reserve_usd']) < 1e-12
    stats.update(filming_calls=3, total_provider_calls=34,
                 reserved_total_usd=film['total_reserve_usd'],
                 estimated_peak_total_usd=peak+sum(r['estimated_peak_usd'] for r in film['rows']))
(HERE/'stats.json').write_text(json.dumps(stats, ensure_ascii=False, indent=2)+'\n', encoding='utf-8', newline='\n')
lines += ['', f'{complete}/24 ответов полностью покрывают заранее записанные ожидания. '
 f'Все {claims} утверждений поддержаны привязанными цитатами, все {quotes} цитат точно совпадают с текущими чанками.', '',
 'HTTP-06: верно объясняет причину 431, но не добавляет ожидаемое MAY-повторение после уменьшения заголовков. '
 'HTTP-07 и HTTP-09 явно содержат это дополнение. Это неполнота ответа, а не выдумка.', '',
 'Два сохранённых диалога заканчиваются 24 сообщениями каждый. После шестого вопроса '
 'переоткрываются manager, history и memory stores и TUI; первая цель переживает окно истории, '
 'signal исправлен с 429 на 431, второй чат имеет независимый device. Возврат в первый чат проверен.', '',
 '## Найденный и исправленный сбой', '',
 'Первый прогон остановился после 7 платных вызовов: шестой сохранённый экранный ответ повлиял на '
 'формат седьмого; модель вернула текст вместо JSON. Валидатор не показал ответ и не сохранил его. '
 'Также отдельные ответы превышали ограничение двух утверждений. '
 'Обязательная схема и применение памяти перенесены после истории. Полный набор после исправления '
 'прошёл; первый неудачный прогон сохранён в initial-attempt.json и включён в расходы. '
 'Автоматических повторов внутри клиента нет.', '',
 '## Границы проверки', '',
 'Происхождение цитат проверяется программно; смысл, полнота, язык и связь с целью '
 'проверены ассистентом отдельно для этого конечного набора (answer-review.json связан SHA-256). '
 'Сохранение ограничения в памяти не гарантирует его исполнение любой будущей моделью. '
 'Решение не является универсальным semantic verifier и не имеет автоматического извлечения task state.', '',
 '## Расходы', '',
 f"До записи: 24 успешных + 7 вызовов первого прогона. Резерв ${stats['reserved_before_recording_usd']:.7f} "
 f"из $0.20; оценка по максимальному тарифу Flash ${peak:.8f}. Это расчёт по usage, не фактическое списание.",
 '[Официальный тариф](https://api-docs.deepseek.com/quick_start/pricing/?tab=case-studies) проверен 2026-10-04. '
 'Запись учитывается отдельно и в том же общем бюджете.', '', '## Сохранённые ответы', '']
for r in data['rows']:
    lines += [f"### {r['id']}: {r['query']}", '', r['grounding']['answer'], '']
    for quote in r['grounding']['quotes']:
        source = next(s for s in r['grounding']['sources'] if s['chunk_id'] == quote['chunk_id'])
        lines += [f"- [{quote['id']}] «{quote['text']}» — `{source['source']}#{source['chunk_id']}`, {source['section']}."]
    lines += ['']
(HERE/'comparison.md').write_text('\n'.join(lines).rstrip()+'\n', encoding='utf-8', newline='\n')
print(json.dumps(stats, ensure_ascii=False, indent=2))
