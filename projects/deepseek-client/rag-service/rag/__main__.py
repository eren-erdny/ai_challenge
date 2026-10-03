import argparse
import json
import sys

from .models import ROOT, DATA, CACHE, OllamaEmbedder, Reranker
from .pipeline import SearchSession, evaluate, index, prepare


def main():
    if hasattr(sys.stdout, 'reconfigure'):
        sys.stdout.reconfigure(encoding='utf-8')
        sys.stderr.reconfigure(encoding='utf-8')
    parser = argparse.ArgumentParser(description='Local Ollama + Qdrant retrieval with multilingual reranking')
    sub = parser.add_subparsers(dest='command', required=True)
    sub.add_parser('doctor', help='Check Ollama, embedding and reranker inference')
    sub.add_parser('download-models', help='Download tokenizer and reranker once; other commands run offline')
    serve = sub.add_parser('serve', help='Serve local retrieval; stop before rebuilding a Local index')
    serve.add_argument('--port', type=int, default=8765)
    serve.add_argument('--manifest', default=str(DATA / 'index.json'))
    for name in ('prepare', 'index'):
        cmd = sub.add_parser(name)
        cmd.add_argument('source', help='UTF-8 document file or directory')
        cmd.add_argument('--add-source', action='append', default=[], help='Additional file/directory in the same knowledge base')
        cmd.add_argument('--size', type=int, default=400)
        cmd.add_argument('--overlap', type=int, default=60)
        cmd.add_argument('--output', default=str(DATA / ('chunks.json' if name == 'prepare' else 'index.json')))
    search = sub.add_parser('search')
    search.add_argument('query')
    search.add_argument('--strategy', choices=['fixed', 'structured'], default='structured')
    search.add_argument('--no-rerank', action='store_true')
    search.add_argument('--output')
    compare = sub.add_parser('evaluate')
    compare.add_argument('questions')
    compare.add_argument('--output', default=str(ROOT / 'reports' / 'comparison.json'))
    for cmd in (search, compare):
        cmd.add_argument('--manifest', default=str(DATA / 'index.json'))
        cmd.add_argument('--candidates', type=int, default=20)
        cmd.add_argument('--top-k', type=int, default=5)
    args = parser.parse_args()
    if args.command == 'serve':
        from .server import serve
        serve(args.manifest, args.port)
        return
    if args.command == 'download-models':
        from .chunking import TokenCounter
        TokenCounter(cache_dir=str(CACHE), download=True)
        Reranker(download=True)
        result = {'model_cache': str(CACHE), 'downloaded': True}
    elif args.command in {'prepare', 'index'}:
        fn = prepare if args.command == 'prepare' else index
        sources = [args.source, *args.add_source] if args.add_source else args.source
        result = fn(sources, args.output, args.size, args.overlap)
        result = {k: v for k, v in result.items() if k != 'chunks'}
    elif args.command == 'search':
        session = SearchSession(args.manifest, rerank=not args.no_rerank)
        try:
            result = session.search(args.query, args.strategy, args.candidates, args.top_k, not args.no_rerank)
            if args.output:
                from .pipeline import save_json
                save_json(args.output, result)
        finally:
            session.close()
    elif args.command == 'evaluate':
        report = evaluate(args.manifest, args.questions, args.output, args.candidates, args.top_k)
        result = {'summary': report['summary'], 'report': args.output}
    else:
        from .chunking import TokenCounter
        counter = TokenCounter(cache_dir=str(CACHE))
        embedder = OllamaEmbedder()
        identity = embedder.identity()
        vector = embedder.embed(['Проверка локальных эмбеддингов'])[0]
        ranked = Reranker().rank('Где хранится индекс?', [
            {'text': 'Индекс хранится на локальном диске.'}, {'text': 'Завтра будет солнечно.'}])
        result = {'embedding': identity, 'dimension': len(vector),
                  'tokenizer_tokens': counter.count('Пример текста'), 'reranker_results': ranked}
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    try:
        main()
    except (Exception,) as exc:
        print(f'ERROR: {type(exc).__name__}: {exc}', file=sys.stderr)
        sys.exit(1)
