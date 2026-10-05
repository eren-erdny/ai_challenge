"""Local HW23 calibration and held-out retrieval comparison; no generation API."""
import argparse
import json
import os
from pathlib import Path
import statistics
import sys

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'projects/deepseek-client/rag-service'))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--model-cache', required=True)
    parser.add_argument('--manifest', default=str(ROOT / 'artifacts/hw21/index.json'))
    args = parser.parse_args()
    manifest = Path(args.manifest).resolve()
    os.environ['RAG_DATA_DIR'] = str(manifest.parent)
    os.environ['RAG_MODEL_CACHE'] = args.model_cache
    from rag.pipeline import SearchSession, relevance, save_json
    from rag.retrieval import rewrite_query
    session = SearchSession(manifest)
    calibration = [
        {'query': 'What URI scheme identifies coffee pots?', 'evidence': [{'source': 'rfc2324.md', 'text': '"coffee:" URI scheme'}]},
        {'query': 'What error code should a teapot return when asked to brew coffee?', 'evidence': [{'source': 'rfc2324.md', 'text': 'code "418 I\'m a teapot"'}]},
        {'query': 'How many moons orbit Jupiter?', 'evidence': []},
        {'query': 'How does photosynthesis work in an oak tree?', 'evidence': []},
    ]
    questions = json.loads((ROOT / 'examples/rag-indexing/questions.json').read_text(encoding='utf-8'))
    for i, q in enumerate(questions, 1):
        q.update(id=f'Q{i:02d}', query='Please, tell me, ' + q['query'] + ' Answer briefly.', expected='Answer the question using the quoted evidence and cite its source.')
    for query in ('What is the distance from Earth to the Moon?', 'Who won the 2022 football World Cup?', 'Which AES encryption key length does HTCPCP-TEA mandate?', 'What is the exact commercial price of a compliant HTCPCP coffee pot?'):
        questions.append({'id': f'Q{len(questions)+1:02d}', 'query': 'Please, tell me, ' + query + ' Answer briefly.', 'expected': 'The supplied documents do not specify this; say evidence is missing, without invented facts or citations.', 'evidence': []})
    save_json(Path(__file__).with_name('questions.json'), questions)
    try:
        calibration_rows = []
        for q in calibration:
            hits = session.search(q['query'], candidates=20, top_k=20, rerank=False)['results']
            calibration_rows.append(dict(q, results=hits, ranked=session.reranker.rank(q['query'], hits)))
        sweep = []
        for threshold in [i / 100 for i in range(35, 86)]:
            positive, negative = [], []
            for q in calibration_rows:
                hits = [h for h in q['results'] if h['vector_score'] >= threshold]
                if q['evidence']:
                    positive.append(float(any(relevance(h, e) for h in hits for e in q['evidence'])))
                else:
                    negative.append(float(not hits))
            sweep.append({'threshold': threshold, 'positive_recall': statistics.mean(positive), 'negative_rejection': statistics.mean(negative)})
        # Prefer recall on calibration positives, then reject negatives; choose
        # the lowest threshold on ties, without inspecting the test questions.
        choice = max(sweep, key=lambda r: (r['positive_recall'], r['negative_rejection'], -r['threshold']))
        threshold = choice['threshold']
        positive_scores = [max(h['rerank_score'] for h in q['ranked'] if any(relevance(h,e) for e in q['evidence'])) for q in calibration_rows if q['evidence']]
        negative_scores = [max(h['rerank_score'] for h in q['ranked']) for q in calibration_rows if not q['evidence']]
        if max(negative_scores) >= min(positive_scores):
            raise ValueError('Calibration does not separate positive/negative logits; choose another method')
        score_threshold = round((max(negative_scores) + min(positive_scores))/2)
        choice['min_rerank_score'] = score_threshold
        choice['score_rule'] = 'Rounded midpoint between highest negative and lowest relevant positive calibration logit'
        choice['positive_scores'] = positive_scores
        choice['negative_scores'] = negative_scores
        save_json(Path(__file__).with_name('calibration.json'), {'questions': calibration_rows, 'sweep': sweep, 'selected': choice})
        rows = []
        modes = [('baseline', False, False, None, None), ('rerank', True, False, None, None), ('cosine_only', True, False, threshold, None), ('filtered', True, False, threshold, score_threshold), ('enhanced', True, True, threshold, score_threshold)]
        for q in questions:
            for mode, rerank, rewrite, cutoff, score_cutoff in modes:
                result = session.search(q['query'], candidates=20, top_k=5, rerank=rerank, min_similarity=cutoff, rewrite=rewrite, min_rerank_score=score_cutoff)
                hits = result['results']
                relevant = [any(relevance(h, e) for e in q['evidence']) for h in hits]
                rank = next((i+1 for i, yes in enumerate(relevant) if yes), None)
                rows.append({'id': q['id'], 'mode': mode, 'answerable': bool(q['evidence']), 'hit': float(rank is not None), 'mrr': 1/rank if rank else 0, 'evidence_precision': sum(relevant)/len(hits) if hits else 0, 'rejected': not hits, **result})
        summary = []
        for mode, *_ in modes:
            positive = [r for r in rows if r['mode'] == mode and r['answerable']]
            negative = [r for r in rows if r['mode'] == mode and not r['answerable']]
            summary.append({'mode': mode, 'hit_at_5': statistics.mean(r['hit'] for r in positive), 'mrr_at_5': statistics.mean(r['mrr'] for r in positive), 'evidence_precision': statistics.mean(r['evidence_precision'] for r in positive), 'false_abstentions': sum(r['rejected'] for r in positive), 'negative_rejections': sum(r['rejected'] for r in negative)})
        save_json(Path(__file__).with_name('retrieval-comparison.json'), {'threshold': threshold, 'min_rerank_score': score_threshold, 'manifest': str(manifest), 'corpus_sha256': session.manifest['corpus_sha256'], 'reranker': session.reranker.name, 'candidates': 20, 'top_k': 5, 'calibration_count': len(calibration), 'test_count': len(questions), 'summary': summary, 'details': rows})
        print(json.dumps({'selected_threshold': threshold, 'summary': summary}, indent=2))
    finally:
        session.close()


if __name__ == '__main__':
    main()
