import io
import json
import unittest
from unittest.mock import patch

from rag import worker


class WorkerTests(unittest.TestCase):
    def test_multiple_requests_share_session_and_close(self):
        closed = []
        queries = []
        class Session:
            def __init__(self, *_, **__):
                self.manifest = {'embedding': {'model': 'fixture', 'digest': 'one'}}
                self.embedder = type('Embedder', (), {'identity': lambda _: self.manifest['embedding']})()
                self.reranker = None
            def search(self, query, **settings):
                queries.append(query)
                return {'results': [{'text': query, 'source': 'example.md'}]}
            def close(self):
                closed.append(True)
        input_bytes = io.BytesIO(b'{"query":"one"}\n{"query":"two"}\n{}\n')
        output_bytes = io.BytesIO()
        input_stream = io.TextIOWrapper(input_bytes, encoding='utf-8')
        output_stream = io.TextIOWrapper(output_bytes, encoding='utf-8')
        with patch.object(worker, 'SearchSession', side_effect=Session) as session, \
             patch.object(worker, 'Reranker', return_value=object()) as ranker, \
             patch.object(worker.sys, 'stdin', input_stream), \
             patch.object(worker.sys, 'stdout', output_stream):
            worker.main()
        output_stream.flush()
        values = [json.loads(line) for line in output_bytes.getvalue().decode().splitlines()]
        self.assertEqual(session.call_count, 1)
        self.assertEqual(ranker.call_count, 1)
        self.assertEqual(queries, ['one', 'two'])
        self.assertEqual(values[0]['result']['results'][0]['text'], 'one')
        self.assertIn('error', values[-1])
        self.assertEqual(closed, [True])
