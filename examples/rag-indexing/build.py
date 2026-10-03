"""Homework 21: real local indexing, portable JSON exports and strategy comparison."""
import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import re
import sys

HERE = Path(__file__).resolve().parent
PROJECT = HERE.parents[1]
SOURCES = {
    2324: 'Hyper Text Coffee Pot Control Protocol (HTCPCP/1.0)',
    6585: 'Additional HTTP Status Codes',
    7168: 'The Hyper Text Coffee Pot Control Protocol for Tea Efflux Appliances',
}


def write_json(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')


def normalize_sources():
    """Preserve original files; remove pagination noise and mark numbered headings."""
    manifest = []
    for number, title in SOURCES.items():
        original = HERE / 'sources' / f'rfc{number}.txt'
        raw = original.read_bytes()
        text = raw.decode('utf-8-sig').replace('\r\n', '\n')
        pages = sorted(set(int(n) for n in re.findall(r'\[Page (\d+)\]', text)))
        if pages != list(range(1, max(pages) + 1)):
            raise ValueError(f'Noncontiguous original page markers: {original}')
        body = []
        removed = {'page_footers': 0, 'running_headers': 0, 'contents_lines': 0}
        in_contents = False
        for line in text.replace('\f', '').splitlines():
            if re.search(r'\[Page \d+\]\s*$', line):
                removed['page_footers'] += 1
                continue
            if re.match(rf'^RFC {number}\s+', line):
                removed['running_headers'] += 1
                continue
            if line.strip() == 'Table of Contents':
                in_contents = True
                removed['contents_lines'] += 1
                continue
            if in_contents and re.match(r'^1\.\s+\S', line):
                in_contents = False
            if in_contents:
                removed['contents_lines'] += 1
                continue
            heading = re.match(r'^(\d+(?:\.\d+)*)(?:\.)?\s+(.+)$', line)
            appendix = re.match(r'^Appendix ([A-Z])\.\s+(.+)$', line)
            if heading:
                depth = min(6, heading[1].count('.') + 2)
                line = '#' * depth + ' ' + heading[1] + '. ' + heading[2]
            elif appendix:
                line = '## ' + line
            body.append(line)
        normalized = '# RFC ' + str(number) + ': ' + title + '\n\n' + '\n'.join(body).strip() + '\n'
        normalized = re.sub(r'\n{3,}', '\n\n', normalized)
        dest = HERE / 'corpus' / f'rfc{number}.md'
        dest.write_text(normalized, encoding='utf-8')
        manifest.append({
            'rfc': number, 'title': title,
            'source_url': f'https://www.rfc-editor.org/rfc/rfc{number}.txt',
            'original': str(original.relative_to(HERE)),
            'indexed_file': str(dest.relative_to(HERE)),
            'original_pages': len(pages), 'original_sha256': hashlib.sha256(raw).hexdigest(),
            'indexed_sha256': hashlib.sha256(dest.read_bytes()).hexdigest(),
            'characters': len(normalized), 'words': len(normalized.split()), 'removed': removed,
        })
    total = sum(s['original_pages'] for s in manifest)
    if total < 20:
        raise ValueError('Corpus must cover at least twenty original pages')
    result = {'sources': manifest, 'total_original_pages': total,
              'total_words': sum(s['words'] for s in manifest),
              'note': 'Original page markers establish volume. Indexed copies preserve copyright notices and section content; only pagination and table-of-contents duplicates are removed.'}
    write_json(HERE / 'sources.json', result)
    return result


def cosine(query, vector):
    return sum(a*b for a, b in zip(query, vector)) / math.sqrt(sum(a*a for a in query) * sum(b*b for b in vector))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, default=PROJECT / 'artifacts' / 'hw21')
    parser.add_argument('--model-cache', type=Path, default=None)
    parser.add_argument('--prepare-only', action='store_true')
    args = parser.parse_args()
    sources = normalize_sources()
    if args.prepare_only:
        print(json.dumps(sources, ensure_ascii=False, indent=2))
        return
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=True)
    if (out / 'index.json').exists():
        raise ValueError('Output already contains an index; use a fresh output directory to preserve evidence')
    os.environ['RAG_DATA_DIR'] = str(out)
    if args.model_cache:
        os.environ['RAG_MODEL_CACHE'] = str(args.model_cache.resolve())
    os.environ['OLLAMA_URL'] = 'http://127.0.0.1:11434'
    os.environ['HF_HUB_OFFLINE'] = '1'
    os.environ.pop('QDRANT_URL', None)
    sys.path.insert(0, str(PROJECT / 'projects' / 'deepseek-client' / 'rag-service'))
    from rag.pipeline import index, evaluate, SearchSession
    from qdrant_store import open_store
    print(f"Corpus: {sources['total_original_pages']} original pages; indexing both strategies locally", flush=True)
    manifest = index(HERE / 'corpus', out / 'index.json', size=400, overlap=60)
    print('Both local collections built; exporting embeddings and metadata', flush=True)
    client = open_store()
    checks = {}
    try:
        for strategy, collection in manifest['collections'].items():
            records, offset = [], None
            while True:
                points, offset = client.scroll(collection['name'], limit=100,
                                              with_payload=True, with_vectors=True, offset=offset)
                for point in points:
                    payload = dict(point.payload)
                    for key in ('source', 'title', 'file', 'section', 'chunk_id', 'text'):
                        if not payload.get(key):
                            raise ValueError(f'Missing metadata {key}')
                    vector = point.vector
                    if len(vector) != collection['dimension'] or not all(math.isfinite(v) for v in vector) or sum(v*v for v in vector) <= 0:
                        raise ValueError('Invalid persisted vector')
                    payload['embedding'] = vector
                    records.append(payload)
                if offset is None:
                    break
            if len(records) != manifest['stats'][strategy]['chunks']:
                raise ValueError('Export count differs from index count')
            if len({c['chunk_id'] for c in records}) != len(records):
                raise ValueError('Duplicate chunk ID')
            portable = {'format': 'rag-json-v1', 'strategy': strategy, 'metric': 'cosine',
                        'embedding_model': manifest['embedding'], 'dimension': collection['dimension'],
                        'corpus_sha256': manifest['corpus_sha256'], 'chunks': records}
            path = out / f'{strategy}-index.json'
            write_json(path, portable)
            checks[strategy] = {'chunks': len(records), 'dimension': collection['dimension'],
                                'metadata_complete': True, 'vectors_finite_nonzero': True,
                                'unique_ids': True, 'json_bytes': path.stat().st_size,
                                'cross_section_chunks': sum(' | ' in c['section'] for c in records)}
    finally:
        client.close()
    # Reopen the on-disk index and verify portable JSON's cosine ranking against Qdrant.
    print('Reopening the index and verifying portable JSON search', flush=True)
    session = SearchSession(out / 'index.json', rerank=False)
    try:
        query = 'Which HTTP status code indicates too many requests and rate limiting?'
        vector = session.embedder.embed([query], query=True)[0]
        for strategy in manifest['collections']:
            saved = json.loads((out / f'{strategy}-index.json').read_text(encoding='utf-8'))
            scores = sorted([(cosine(vector, c['embedding']), c['chunk_id']) for c in saved['chunks']], reverse=True)
            hits = session.search(query, strategy, candidates=5, top_k=5, rerank=False, vector=vector)['results']
            if {c['chunk_id'] for c in hits} != {chunk_id for _, chunk_id in scores[:5]}:
                raise ValueError('Portable JSON and reopened Qdrant rankings differ')
            checks[strategy]['json_search_matches_qdrant_top5'] = True
            checks[strategy]['reopened_top_source'] = hits[0]['source']
    finally:
        session.close()
    print('Comparing retrieval on predefined source/text evidence', flush=True)
    evaluation = evaluate(out / 'index.json', HERE / 'questions.json', out / 'retrieval-comparison.json', candidates=20, top_k=5)
    result = {'corpus': sources, 'chunk_size': 400, 'overlap': 60,
              'chunk_statistics': manifest['stats'], 'index_seconds': manifest['index_seconds'],
              'verification': checks, 'retrieval': evaluation['summary'],
              'embedding': manifest['embedding'], 'paid_llm_calls': 0}
    write_json(HERE / 'results.json', result)
    (HERE / 'retrieval-comparison.md').write_text((out / 'retrieval-comparison.md').read_text(encoding='utf-8'), encoding='utf-8')
    write_json(out / 'verification.json', checks)
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
