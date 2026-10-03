"""Single-owner loopback service. Models and Local Qdrant live across queries."""
import json
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path

from .models import Reranker
from .pipeline import SearchSession


class Retrieval:
    def __init__(self, manifest):
        self.path = Path(manifest)
        self.snapshot = self.path.read_bytes()
        self.session = SearchSession(manifest, rerank=False)

    def search(self, value):
        if self.path.read_bytes() != self.snapshot:
            raise RuntimeError('Index manifest changed; restart the RAG service')
        if self.session.embedder.identity() != self.session.manifest['embedding']:
            raise RuntimeError('Embedding model changed; rebuild the index')
        query, strategy, candidates, top_k, rerank = validate(value)
        if rerank and self.session.reranker is None:
            self.session.reranker = Reranker()
        return self.session.search(query, strategy, candidates, top_k, rerank)

    def close(self):
        self.session.close()


def validate(value):
    if not isinstance(value, dict) or set(value) - {'query', 'strategy', 'candidates', 'top_k', 'rerank'}:
        raise ValueError('Expected a search object with known fields')
    query = value.get('query')
    strategy = value.get('strategy', 'structured')
    candidates, top_k = value.get('candidates', 20), value.get('top_k', 5)
    rerank = value.get('rerank')
    if rerank is None:
        rerank = True
    if not isinstance(query, str) or not query.strip() or len(query.encode('utf-8')) > 4096:
        raise ValueError('Query must be nonempty and at most 4096 UTF-8 bytes')
    if strategy not in ('fixed', 'structured') or type(rerank) is not bool:
        raise ValueError('Invalid strategy or rerank setting')
    if type(candidates) is not int or type(top_k) is not int or not 1 <= top_k <= 20 or not top_k <= candidates <= 100:
        raise ValueError('Require 1 <= top_k <= 20 and top_k <= candidates <= 100')
    return query, strategy, candidates, top_k, rerank


def handler(retrieval):
    class Handler(BaseHTTPRequestHandler):
        def reply(self, status, value):
            data = json.dumps(value, ensure_ascii=False, allow_nan=False).encode('utf-8')
            self.send_response(status)
            self.send_header('Content-Type', 'application/json; charset=utf-8')
            self.send_header('Content-Length', str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def do_GET(self):
            if self.path != '/health':
                self.reply(404, {'error': 'Unknown endpoint'})
                return
            self.reply(200, {'ready': True, 'collections': retrieval.session.manifest['collections'],
                             'embedding': retrieval.session.manifest['embedding']})

        def do_POST(self):
            # Consume bounded bodies before an early response to avoid TCP resets
            # on Windows when closing a socket with unread request bytes.
            try:
                size = int(self.headers.get('Content-Length', '0'))
                if not 1 <= size <= 16384:
                    raise ValueError('Request body must be 1..16384 bytes')
                body = self.rfile.read(size)
            except ValueError as exc:
                self.reply(400, {'error': str(exc)})
                return
            # No browser CORS or form-based access; no indexing/filesystem endpoint.
            if self.headers.get('Origin') is not None:
                self.reply(403, {'error': 'Browser origins are not allowed'})
                return
            if self.path != '/search':
                self.reply(404, {'error': 'Unknown endpoint'})
                return
            if self.headers.get('Content-Type', '').split(';')[0] != 'application/json':
                self.reply(415, {'error': 'Require application/json'})
                return
            try:
                value = json.loads(body)
                validate(value)
                result = retrieval.search(value)
            except (ValueError, UnicodeError) as exc:
                self.reply(400, {'error': str(exc)})
                return
            except Exception as exc:
                self.reply(503, {'error': str(exc)})
                return
            self.reply(200, result)

        def setup(self):
            super().setup()
            self.connection.settimeout(15)

        def log_message(self, *_):
            pass  # Do not log questions or document excerpts.
    return Handler


def serve(manifest, port=8765):
    retrieval = Retrieval(manifest)
    try:
        with HTTPServer(('127.0.0.1', port), handler(retrieval)) as server:
            print(f'RAG ready: http://127.0.0.1:{port}', flush=True)
            try:
                server.serve_forever()
            except KeyboardInterrupt:
                pass
    finally:
        retrieval.close()
