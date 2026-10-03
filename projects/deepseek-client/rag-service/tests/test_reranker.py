import unittest
from unittest.mock import patch

from rag.models import CACHE, Reranker


class RerankerTests(unittest.TestCase):
    def test_offline_loading_and_long_passage_tail(self):
        if not (CACHE / 'models--cross-encoder--mmarco-mMiniLMv2-L12-H384-v1').exists():
            self.skipTest('Run python -m rag download-models before the real model check')
        with patch('requests.sessions.Session.request', side_effect=AssertionError('Unexpected external HTTP')):
            reranker = Reranker()
            text = 'The weather is sunny and warm. ' * 150
            text += ' Backup copies are retained for exactly fourteen days.'
            self.assertGreater(len(reranker.tokenizer.encode(text, verbose=False)), 1024)
            hits = reranker.rank('How many days are backup copies retained?', [
                {'text': 'The weather is sunny and warm.', 'source': 'noise'},
                {'text': text, 'source': 'tail'},
            ])
            self.assertEqual(hits[0]['source'], 'tail')
            self.assertGreater(hits[0]['rerank_score'], hits[1]['rerank_score'])


if __name__ == '__main__':
    unittest.main()
