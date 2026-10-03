import hashlib
import json
import os
from pathlib import Path
import statistics
import time
import uuid

from qdrant_client import models
from qdrant_store import open_store
from .chunking import TokenCounter, chunk_documents
from .documents import load_documents
from .retrieval import rewrite_query, validate_options
from .models import CACHE, ROOT, DATA, OllamaEmbedder, Reranker


def save_json(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_suffix(path.suffix + '.tmp')
    temporary.write_text(json.dumps(value, ensure_ascii=False, indent=2), encoding='utf-8')
    os.replace(temporary, path)


def prepare(source, output, size=400, overlap=60):
    if isinstance(source, list):
        if not source:
            raise ValueError('No document sources supplied')
        docs, skipped = [], []
        for number, root in enumerate(source, 1):
            items, ignored = load_documents(root)
            prefix = f'{number:03d}-{Path(root).name}'
            for item in items:
                item.source = prefix + '/' + item.source
            docs.extend(items)
            skipped.extend(prefix + '/' + name for name in ignored)
    else:
        docs, skipped = load_documents(source)
    counter = TokenCounter(cache_dir=str(CACHE))
    chunks, stats = {}, {}
    for strategy in ('fixed', 'structured'):
        started = time.perf_counter()
        items = chunk_documents(docs, strategy, counter, size, overlap)
        chunks[strategy] = items
        lengths = [c['tokens'] for c in items]
        stats[strategy] = {
            'chunks': len(items), 'min_tokens': min(lengths), 'max_tokens': max(lengths),
            'mean_tokens': statistics.mean(lengths), 'total_tokens_with_overlap': sum(lengths),
            'chunking_seconds': time.perf_counter() - started,
        }
    corpus_hash = hashlib.sha256(json.dumps(
        [(d.source, d.page, d.text) for d in docs], ensure_ascii=False).encode()).hexdigest()
    result = {
        'source_root': str(Path(source[0] if isinstance(source, list) else source).resolve()),
        'source_roots': [str(Path(p).resolve()) for p in source] if isinstance(source, list) else [str(Path(source).resolve())],
        'corpus_sha256': corpus_hash,
        'files': len({d.source for d in docs}), 'text_units': len(docs),
        'pdf_pages': sum(d.page is not None for d in docs),
        'characters': sum(len(d.text) for d in docs), 'skipped_files': skipped,
        'tokenizer': 'nomic-ai/nomic-embed-text-v1.5', 'size': size, 'overlap': overlap,
        'stats': stats, 'chunks': chunks,
    }
    save_json(output, result)
    return result


def index(source, manifest, size=400, overlap=60):
    manifest = Path(manifest)
    prepared = prepare(source, manifest.with_name(manifest.stem + '-chunks.json'), size, overlap)
    embedder = OllamaEmbedder()
    identity = embedder.identity()
    client = open_store()
    created = []
    collections, timings = {}, {}
    try:
        for strategy, chunks in prepared['chunks'].items():
            started = time.perf_counter()
            # Identical representation for both strategies; metadata does not bias comparison.
            vectors = embedder.embed([c['text'] for c in chunks])
            dimension = len(vectors[0])
            name = f'chunks_{strategy}_{uuid.uuid4().hex[:12]}'
            client.create_collection(name, vectors_config=models.VectorParams(
                size=dimension, distance=models.Distance.COSINE))
            created.append(name)
            for start in range(0, len(chunks), 64):
                points = [models.PointStruct(
                    id=str(uuid.uuid5(uuid.NAMESPACE_URL, chunk['chunk_id'])),
                    vector=vector, payload=chunk,
                ) for chunk, vector in zip(chunks[start:start + 64], vectors[start:start + 64], strict=True)]
                client.upsert(name, points=points, wait=True)
            if client.count(name, exact=True).count != len(chunks):
                raise ValueError('Stored chunk count does not match input')
            collections[strategy] = {'name': name, 'dimension': dimension}
            timings[strategy] = time.perf_counter() - started
        result = {key: value for key, value in prepared.items() if key != 'chunks'}
        result.update({'embedding': identity, 'collections': collections,
                       'qdrant_path': str(DATA / 'qdrant') if not os.environ.get('QDRANT_URL') else None,
                       'index_seconds': timings, 'backend': os.environ.get('QDRANT_URL', 'local')})
        # Publish only after both collections succeed. Previous indexes stay intact.
        save_json(manifest, result)
        return result
    except Exception:
        for name in created:
            client.delete_collection(name)
        raise
    finally:
        client.close()


class SearchSession:
    def __init__(self, manifest, rerank=True):
        self.manifest = json.loads(Path(manifest).read_text(encoding='utf-8'))
        expected_backend = self.manifest['backend']
        if expected_backend != os.environ.get('QDRANT_URL', 'local'):
            raise ValueError('QDRANT_URL differs from the indexed backend')
        if expected_backend == 'local' and self.manifest.get('qdrant_path') != str(DATA / 'qdrant'):
            raise ValueError('Local database path differs from the indexed backend; rebuild the index')
        self.embedder = OllamaEmbedder(model=self.manifest['embedding']['model'])
        if self.embedder.identity() != self.manifest['embedding']:
            raise ValueError('Embedding model changed; rebuild the index')
        self.reranker = Reranker() if rerank else None
        self.client = open_store()

    def close(self):
        self.client.close()

    def search(self, query, strategy='structured', candidates=20, top_k=5, rerank=True, vector=None, min_similarity=None, rewrite=False, min_rerank_score=None):
        validate_options(candidates, top_k, rerank, min_similarity, rewrite, min_rerank_score)
        if not isinstance(query, str) or not query.strip() or len(query.encode('utf-8')) > 4096:
            raise ValueError('Query must be nonempty and at most 4096 UTF-8 bytes')
        search_query = rewrite_query(query) if rewrite else query
        if vector is not None and search_query != query:
            raise ValueError('A supplied vector must correspond to the original query; disable rewrite')
        if strategy not in self.manifest['collections']:
            raise ValueError('Unknown chunking strategy')
        if rerank and self.reranker is None:
            raise ValueError('Reranker is not loaded')
        started = time.perf_counter()
        vector = vector if vector is not None else self.embedder.embed([search_query], query=True)[0]
        collection = self.manifest['collections'][strategy]
        if len(vector) != collection['dimension']:
            raise ValueError('Query embedding dimension differs from the index')
        hits = self.client.query_points(collection['name'], query=vector, limit=candidates,
                                        with_payload=True).points
        retrieved = [dict(hit.payload, vector_score=hit.score) for hit in hits]
        retrieval_seconds = time.perf_counter() - started
        rerank_start = time.perf_counter()
        eligible = [hit for hit in retrieved if min_similarity is None or hit['vector_score'] >= min_similarity]
        ranked = self.reranker.rank(search_query, eligible) if rerank and eligible else eligible
        accepted = [hit for hit in ranked if min_rerank_score is None or hit['rerank_score'] >= min_rerank_score]
        return {'query': query, 'search_query': search_query, 'rewrite': rewrite,
                'min_similarity': min_similarity, 'score_type': 'cosine',
                'retained_count': len(accepted), 'filtered_count': len(retrieved) - len(accepted),
                'cosine_retained_count': len(eligible), 'min_rerank_score': min_rerank_score,
                'rerank_score_type': 'raw_logit',
                'no_evidence': not accepted, 'candidates': candidates,
                'strategy': strategy, 'reranking': rerank,
                'candidate_count': len(retrieved), 'top_k': top_k,
                'retrieval_seconds': retrieval_seconds,
                'rerank_seconds': time.perf_counter() - rerank_start if rerank else 0,
                'results': accepted[:top_k]}


def relevance(hit, evidence):
    def compact(text):
        return ' '.join(text.casefold().split())
    return hit['source'] == evidence['source'] and compact(evidence['text']) in compact(hit['text'])


def metrics(hits, evidence):
    relevant = [any(relevance(hit, item) for item in evidence) for hit in hits]
    first = next((i + 1 for i, yes in enumerate(relevant) if yes), None)
    return {'hit': float(first is not None), 'reciprocal_rank': 1 / first if first else 0,
            'evidence_recall': sum(any(relevance(h, item) for h in hits) for item in evidence) / len(evidence)}


def evaluate(manifest, questions, output, candidates=20, top_k=5):
    if candidates < top_k or top_k < 1:
        raise ValueError('Require candidates >= top_k >= 1')
    data = json.loads(Path(questions).read_text(encoding='utf-8'))
    if not data or any(not q.get('query', '').strip() or not q.get('evidence')
                       or any(not e.get('source') or not e.get('text', '').strip() for e in q['evidence']) for q in data):
        raise ValueError('Each evaluation query needs nonempty evidence with source and text')
    session = SearchSession(manifest)
    rows = []
    try:
        for item in data:
            started = time.perf_counter()
            vector = session.embedder.embed([item['query']], query=True)[0]
            embed_seconds = time.perf_counter() - started
            for strategy in ('fixed', 'structured'):
                raw = session.search(item['query'], strategy, candidates, candidates, False, vector)
                start = time.perf_counter()
                ranked = session.reranker.rank(item['query'], raw['results'])
                rerank_seconds = time.perf_counter() - start
                for enabled, hits in ((False, raw['results']), (True, ranked)):
                    selected = hits[:top_k]
                    rows.append({'query': item['query'], 'strategy': strategy, 'reranking': enabled,
                                 **metrics(selected, item['evidence']),
                                 'seconds': embed_seconds + raw['retrieval_seconds'] + (rerank_seconds if enabled else 0),
                                 'candidate_count': raw['candidate_count'], 'results': selected})
        summary = []
        for strategy in ('fixed', 'structured'):
            for enabled in (False, True):
                selected = [r for r in rows if r['strategy'] == strategy and r['reranking'] == enabled]
                summary.append({'strategy': strategy, 'reranking': enabled,
                                **{k: statistics.mean(r[k] for r in selected)
                                   for k in ('hit', 'reciprocal_rank', 'evidence_recall', 'seconds')}})
        report = {'manifest': str(Path(manifest).resolve()), 'index': session.manifest,
                  'reranker': session.reranker.name, 'questions': len(data),
                  'top_k': top_k, 'candidates': candidates, 'summary': summary, 'details': rows,
                  'note': 'Evidence is a source plus verbatim text. Timings exclude model loading; small fixtures do not establish real-world quality.'}
        save_json(output, report)
        lines = ['# Сравнение chunking и reranking', '',
                 f'Вопросов: {len(data)}. Кандидатов: до {candidates}. Результатов: до {top_k}.', '',
                 'Метрики: Hit — найден хотя бы один фрагмент с эталоном; MRR — обратный ранг первого такого фрагмента;',
                 'Recall — доля найденных эталонных цитат. Совпадение требует источника и полного текста цитаты.', '',
                 '| Стратегия | Reranking | Hit@k | MRR@k | Evidence Recall@k | Среднее, с |',
                 '|---|---|---:|---:|---:|---:|']
        for row in summary:
            lines.append(f"| {row['strategy']} | {row['reranking']} | {row['hit']:.3f} | {row['reciprocal_rank']:.3f} | {row['evidence_recall']:.3f} | {row['seconds']:.3f} |")
        lines += ['', '## Размеры чанков', '', '| Стратегия | Чанков | Мин. токенов | Среднее | Макс. |', '|---|---:|---:|---:|---:|']
        for strategy, stats in session.manifest['stats'].items():
            lines.append(f"| {strategy} | {stats['chunks']} | {stats['min_tokens']} | {stats['mean_tokens']:.1f} | {stats['max_tokens']} |")
        lines += ['', 'Время включает embedding вопроса, поиск и (если включён) reranking; загрузка моделей исключена.',
                  'Малый тестовый корпус проверяет работоспособность, но не доказывает качество на реальных документах.',
                  'Сырые оценки reranker — не вероятности; сравнивать их с cosine score нельзя.']
        Path(output).with_suffix('.md').write_text('\n'.join(lines) + '\n', encoding='utf-8')
        return report
    finally:
        session.close()
