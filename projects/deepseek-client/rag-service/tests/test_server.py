import json
from http.server import HTTPServer
import threading
import unittest
from urllib.request import Request, urlopen
from urllib.error import HTTPError
from types import SimpleNamespace

from rag.server import handler, validate


class ServerTests(unittest.TestCase):
    def test_validation(self):
        for value in ({}, {'query': ' '}, {'query': 'x', 'top_k': True},
                      {'query': 'x', 'top_k': 21}, {'query': 'x', 'candidates': 1},
                      {'query': 'x', 'strategy': 'bad'}, {'query': 'x', 'source': '/private'}):
            with self.assertRaises(ValueError):
                validate(value)
        self.assertEqual(validate({'query': 'hello', 'rerank': False})[-1], False)

    def test_http_contract_and_failures(self):
        seen = []
        def search(value):
            seen.append(value)
            if value['query'] == 'fail':
                raise RuntimeError('rebuild the index')
            return {'results': [{'source': 'a.md', 'chunk_id': 'a', 'text': 'Evidence'}]}
        retrieval = SimpleNamespace(search=search, session=SimpleNamespace(
            manifest={'collections': {}, 'embedding': {'model': 'fixture'}}))
        server = HTTPServer(('127.0.0.1', 0), handler(retrieval))
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        address = f'http://127.0.0.1:{server.server_port}'
        try:
            with urlopen(address + '/health', timeout=5) as response:
                self.assertTrue(json.load(response)['ready'])
            req = Request(address + '/search', data=b'{"query":"hello","rerank":false}',
                          headers={'Content-Type': 'application/json'})
            with urlopen(req, timeout=5) as response:
                self.assertEqual(json.load(response)['results'][0]['source'], 'a.md')
            for data, headers, status in (
                (b'{"query":"fail"}', {'Content-Type': 'application/json'}, 503),
                (b'{"query":" "}', {'Content-Type': 'application/json'}, 400),
                (b'{"query":"x"}', {'Content-Type': 'text/plain'}, 415),
                (b'{"query":"x"}', {'Content-Type': 'application/json', 'Origin': 'https://example.org'}, 403),
            ):
                with self.assertRaises(HTTPError) as error:
                    urlopen(Request(address + '/search', data=data, headers=headers), timeout=5)
                self.assertEqual(error.exception.code, status)
            self.assertEqual(len(seen), 2)
        finally:
            server.shutdown()
            server.server_close()
            thread.join()
