"""Prepare ten HW24 questions and actual local retrieval; never call an LLM API."""
import argparse
import json
import os
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[2]
HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(ROOT / 'projects/deepseek-client/rag-service'))

QUESTIONS = [
    dict(id='Q01', category='ordinary', query='Which method tells the coffee pot that enough milk has been added?', expected='WHEN. Do not confuse it with BREW or PROPFIND.', evidence=[dict(source='rfc2324.md', section='2.1.4. WHEN method', text='the "WHEN" method has been added to HTCPCP')]),
    dict(id='Q02', category='ordinary', query='What content type requests tea from a TEA-capable pot?', expected='message/teapot is required, not message/coffeepot.', evidence=[dict(source='rfc7168.md', section='3. The "message/teapot" Media Type', text='MUST be "message/teapot"')]),
    dict(id='Q03', category='ordinary', query='Which sugar-type additions are permitted by HTCPCP-TEA?', expected='Sugar, Xylitol and Stevia; do not add alternatives.', evidence=[dict(source='rfc7168.md', section='2.2.1. The Accept-Additions Header Field', text='sugar-type      = ( "Sugar" | "Xylitol" | "Stevia" )')]),
    dict(id='Q04', category='ordinary', query='Which protocol synchronizes the clocks of coffee pots?', expected='Network Time Protocol (NTP), a SHOULD recommendation.', evidence=[dict(source='rfc2324.md', section='5.1. Timing Considerations', text='Coffee pots SHOULD use the Network Time Protocol')]),
    dict(id='Q05', category='ordinary', query='Which status code requires a conditional request to prevent lost updates?', expected='428 Precondition Required; explain the conditional request without confusing it with 429.', evidence=[dict(source='rfc6585.md', section='3. 428 Precondition Required', text='The 428 status code indicates that the origin server requires the request to be conditional.')]),
    dict(id='Q06', category='ordinary', query='May HTTP 429 responses include a Retry-After header?', expected='Yes, MAY include it, not MUST. Header indicates how long to wait.', evidence=[dict(source='rfc6585.md', section='4. 429 Too Many Requests', text='MAY include a Retry-After header')]),
    dict(id='Q07', category='negation', query='May HTTP 431 responses be stored by a cache? Preserve the normative requirement.', expected='No. Responses with 431 MUST NOT be stored by a cache; preserve the negation.', evidence=[dict(source='rfc6585.md', section='5. 431 Request Header Fields Too Large', text='Responses with the 431 status code MUST NOT be stored by a cache.')]),
    dict(id='Q08', category='condition', query='According to HTCPCP-TEA, does a TEA-capable pot that cannot brew coffee have to return 418, or is another code allowed? Explain both codes.', expected='For TEA-capable pots not provisioned to brew coffee, either 503 (temporary unavailability) or 418 (more permanent teapot indication) may be returned. Do not claim 418 is mandatory.', evidence=[dict(source='rfc7168.md', section="2.3.3. 418 I'm a Teapot", text='TEA-capable pots that are not provisioned to brew coffee may return either a status code of 503')]),
    dict(id='Q09', category='unsupported_far', query='What is the distance from Earth to the Moon?', expected='Unknown + clarification. No fabricated astronomy answer, sources or quotes. Empty context must result in zero LLM calls.', evidence=[]),
    dict(id='Q10', category='unsupported_near', query='Which AES encryption key length does HTCPCP-TEA mandate?', expected='Unknown + clarification even if topic-related chunks are found. Do not invent a key length or cite unrelated security text as proof.', evidence=[]),
]

def norm(s):
    return ' '.join(s.split())

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--model-cache', required=True)
    parser.add_argument('--manifest', default=str(ROOT / 'artifacts/hw21/index.json'))
    parser.add_argument('--questions-only', action='store_true')
    args = parser.parse_args()
    manifest = Path(args.manifest).resolve()
    os.environ['RAG_DATA_DIR'] = str(manifest.parent)
    os.environ['RAG_MODEL_CACHE'] = args.model_cache
    HERE.mkdir(parents=True, exist_ok=True)
    (HERE / 'questions.json').write_text(json.dumps(QUESTIONS, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    if args.questions_only:
        return
    from rag.pipeline import SearchSession
    session = SearchSession(manifest)
    try:
        rows = []
        for q in QUESTIONS:
            result = session.search(q['query'], candidates=20, top_k=5, rerank=True, rewrite=True, min_similarity=.49, min_rerank_score=-3)
            relevant = [h for h in result['results'] if any(h['source'] == e['source'] and norm(e['text']) in norm(h['text']) for e in q['evidence'])]
            rows.append(dict(id=q['id'], relevant_chunk_ids=[h['chunk_id'] for h in relevant], retrieval=result))
            print(q['id'], 'chunks=', len(result['results']), 'expected_evidence=', len(relevant), flush=True)
        report = dict(kind='real local retrieval; no generation API', manifest=str(manifest), corpus_sha256=session.manifest['corpus_sha256'], candidates=20, top_k=5, min_similarity=.49, min_rerank_score=-3, rows=rows)
        (HERE / 'retrieval.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
        if any(q['evidence'] and not r['relevant_chunk_ids'] for q, r in zip(QUESTIONS, rows)):
            raise SystemExit('Expected evidence missing: inspect retrieval before paid runs')
    finally:
        session.close()

if __name__ == '__main__':
    main()
