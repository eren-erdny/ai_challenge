import unittest
from types import SimpleNamespace
from unittest.mock import Mock

from rag.pipeline import SearchSession
from rag.retrieval import rewrite_query
from rag.server import validate


class RetrievalTests(unittest.TestCase):
    def session(self):
        session = SearchSession.__new__(SearchSession)
        session.manifest = {'collections': {'structured': {'name': 'fixture', 'dimension': 2}}}
        session.embedder = Mock()
        session.embedder.embed.return_value = [[1, 0]]
        points = [SimpleNamespace(payload={'text': name}, score=score) for name, score in [('good', .8), ('bad', .4), ('boundary', .6)]]
        session.client = Mock()
        session.client.query_points.return_value.points = points
        session.reranker = Mock()
        session.reranker.rank.side_effect = lambda query, hits: list(reversed(hits))
        return session

    def test_filter_before_rerank_and_limit_uses_cosine(self):
        session = self.session()
        result = session.search('question', candidates=3, top_k=1, min_similarity=.6)
        self.assertEqual(result['candidate_count'], 3)
        self.assertEqual(result['retained_count'], 2)
        self.assertEqual(result['results'][0]['text'], 'boundary')
        self.assertEqual([h['text'] for h in session.reranker.rank.call_args.args[1]], ['good', 'boundary'])
        self.assertEqual(session.client.query_points.call_args.kwargs['limit'], 3)

    def test_empty_evidence_does_not_fallback_or_call_ranker(self):
        session = self.session()
        result = session.search('question', min_similarity=.9)
        self.assertTrue(result['no_evidence'])
        self.assertEqual(result['results'], [])
        session.reranker.rank.assert_not_called()

    def test_rewrite_original_preserved_and_both_models_use_search_query(self):
        session = self.session()
        original = 'Please, tell me, may HTTP 431 NOT be cached? Answer briefly.'
        result = session.search(original, rewrite=True)
        self.assertEqual(result['query'], original)
        self.assertEqual(result['search_query'], 'may HTTP 431 NOT be cached?')
        session.embedder.embed.assert_called_once_with([result['search_query']], query=True)
        self.assertEqual(session.reranker.rank.call_args.args[0], result['search_query'])
        self.assertEqual(rewrite_query('Пожалуйста, расскажи мне, почему 429 нельзя кешировать? Ответь кратко.'), 'почему 429 нельзя кешировать?')

    def test_baseline_keeps_unfiltered_vector_order(self):
        session = self.session()
        result = session.search('question', candidates=3, top_k=2, rerank=False)
        self.assertEqual([h['text'] for h in result['results']], ['good', 'bad'])
        session.reranker.rank.assert_not_called()

    def test_invalid_options_rejected_at_http_boundary(self):
        for options in ({'min_similarity': float('nan')}, {'min_similarity': 1.1}, {'min_similarity': True}, {'rewrite': 'yes'}, {'candidates': True}, {'rerank': False, 'min_rerank_score': 0}, {'min_rerank_score': float('inf')}):
            with self.assertRaises(ValueError):
                validate({'query': 'question', **options})
        session = self.session()
        with self.assertRaises(ValueError):
            session.search('Please, question', rewrite=True, vector=[1, 0])

    def test_rerank_logit_cutoff_after_ranking_without_cosine_confusion(self):
        session = self.session()
        session.reranker.rank.side_effect = lambda _, hits: [dict(h, rerank_score=10 if h['text'] == 'bad' else -2) for h in hits]
        result = session.search('question', min_rerank_score=0)
        self.assertEqual([h['text'] for h in result['results']], ['bad'])
        self.assertEqual(result['retained_count'], 1)
        self.assertEqual(result['filtered_count'], 2)
        # Compatibility: the legacy Go HTTP client serializes omitted rerank as null.
        validate({'query': 'question', 'rerank': None})
