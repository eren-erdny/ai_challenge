"""Persistent private stdio worker owned by the Go knowledge-base manager."""
import json
import sys

from .models import DATA, Reranker
from .pipeline import SearchSession
from .server import validate
from .retrieval import extra_options


def main():
    session = None
    sys.stdin.reconfigure(encoding='utf-8')
    sys.stdout.reconfigure(encoding='utf-8')
    try:
        for line in sys.stdin:
            try:
                value = json.loads(line)
                query, strategy, candidates, top_k, rerank = validate(value)
                if session is None:
                    session = SearchSession(DATA / 'index.json', rerank=False)
                if session.embedder.identity() != session.manifest['embedding']:
                    raise ValueError('Embedding model changed; update the knowledge base')
                if rerank and session.reranker is None:
                    session.reranker = Reranker()
                result = session.search(query, strategy=strategy, candidates=candidates, top_k=top_k, rerank=rerank, **extra_options(value))
                print(json.dumps({'result': result}, ensure_ascii=False, allow_nan=False), flush=True)
            except Exception as exc:
                print(json.dumps({'error': str(exc)}, ensure_ascii=False), flush=True)
    finally:
        if session is not None:
            session.close()


if __name__ == '__main__':
    main()
