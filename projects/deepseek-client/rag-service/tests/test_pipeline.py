import json
from pathlib import Path
import re
import tempfile
import unittest
from unittest.mock import patch

from qdrant_client import QdrantClient
from rag.chunking import chunk_documents
from rag.documents import Document, load_documents, normalize, sections
from rag.pipeline import SearchSession, index, metrics, prepare


class WordCounter:
    def offsets(self, text):
        return [m.span() for m in re.finditer(r'\S+', text)]

    def count(self, text):
        return len(self.offsets(text))


class PipelineTests(unittest.TestCase):
    def test_multiple_sources_with_same_filename_keep_distinct_citations(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            first, second = root / 'a', root / 'b'
            first.mkdir(); second.mkdir()
            (first / 'manual.txt').write_text('First source evidence.', encoding='utf-8')
            (second / 'manual.txt').write_text('Second source evidence.', encoding='utf-8')
            with patch('rag.pipeline.TokenCounter', return_value=WordCounter()):
                result = prepare([str(first), str(second)], root / 'prepared.json', 20, 3)
            self.assertEqual(result['source_roots'], [str(first.resolve()), str(second.resolve())])
            self.assertEqual({c['source'] for c in result['chunks']['fixed']}, {'001-a/manual.txt', '002-b/manual.txt'})

    def test_code_normalization_preserves_literals(self):
        code = 'def f():\r\n    return "  x  "\r\n'
        self.assertEqual(normalize(code, code=True), 'def f():\n    return "  x  "\n')

    def test_markdown_fenced_heading_is_not_a_section(self):
        doc = Document('a.md', 'a', '# First\nText\n```python\n# not a section\n```\n## Second\nMore', '.md')
        spans = sections(doc)
        self.assertEqual([s[2] for s in spans], ['First', 'First / Second'])
        self.assertEqual(''.join(doc.text[a:b] for a, b, _ in spans), doc.text)

    def test_python_preamble_decorators_and_tail_are_retained(self):
        text = 'import os\n\n@decorator\ndef f():\n    return 1\n\nVALUE = 2\n'
        doc = Document('a.py', 'a', text, '.py')
        spans = sections(doc)
        self.assertEqual(''.join(text[a:b] for a, b, _ in spans), text)
        function = next(text[a:b] for a, b, label in spans if label.endswith(': f'))
        self.assertTrue(function.startswith('@decorator'))
        self.assertIn('VALUE = 2', ''.join(text[a:b] for a, b, label in spans if label == 'Module'))

    def test_fixed_overlap_and_no_missing_words(self):
        text = ' '.join(f'word{i}' for i in range(39))
        doc = Document('a.txt', 'a', text, '.txt')
        chunks = chunk_documents([doc], 'fixed', WordCounter(), 10, 3)
        self.assertEqual(chunks[0]['text'].split()[-3:], chunks[1]['text'].split()[:3])
        self.assertEqual(set(text.split()), {w for c in chunks for w in c['text'].split()})
        self.assertTrue(all(c['tokens'] <= 10 for c in chunks))
        self.assertEqual(chunks, chunk_documents([doc], 'fixed', WordCounter(), 10, 3))
        self.assertEqual(len(chunks), len({c['chunk_id'] for c in chunks}))
        for c in chunks:
            self.assertEqual(text[c['start_char']:c['end_char']], c['text'])

    def test_structured_never_mixes_headers(self):
        doc = Document('a.md', 'a', '# Alpha\n' + 'one '*22 + '\n# Beta\n' + 'two '*22, '.md')
        chunks = chunk_documents([doc], 'structured', WordCounter(), 10, 3)
        self.assertTrue(all(not ('one' in c['text'] and 'two' in c['text']) for c in chunks))
        self.assertEqual({c['section'] for c in chunks}, {'Alpha', 'Beta'})

    def test_bad_overlap_is_rejected(self):
        with self.assertRaises(ValueError):
            chunk_documents([], 'fixed', WordCounter(), 10, 10)

    def test_real_nomic_tokenizer_respects_limit_on_russian(self):
        from rag.chunking import TokenCounter
        from rag.models import CACHE
        try:
            counter = TokenCounter(cache_dir=str(CACHE))
        except OSError:
            self.skipTest('Download tokenizer with python -m rag download-models first')
        text = 'Переранжирование документов сохраняет результаты поиска. ' * 30
        chunks = chunk_documents([Document('ru.txt', 'ru', text, '.txt')], 'fixed', counter, 40, 6)
        self.assertTrue(all(counter.count(c['text']) <= 40 for c in chunks))

    def test_loader_ignores_secrets_and_reports_unsupported(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'a.txt').write_text('Hello', encoding='utf-8')
            (root / '.env').write_text('secret', encoding='utf-8')
            (root / 'x.bin').write_bytes(b'\0')
            docs, skipped = load_documents(root)
            self.assertEqual([d.source for d in docs], ['a.txt'])
            self.assertEqual(skipped, ['x.bin'])

    def test_pdf_text_pages_and_ocr_failure(self):
        from pypdf import PdfWriter
        from pypdf.generic import DictionaryObject, NameObject, DecodedStreamObject
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'sample.pdf'
            writer = PdfWriter()
            page = writer.add_blank_page(width=300, height=300)
            font = DictionaryObject({NameObject('/Type'): NameObject('/Font'),
                                     NameObject('/Subtype'): NameObject('/Type1'),
                                     NameObject('/BaseFont'): NameObject('/Helvetica')})
            page[NameObject('/Resources')] = DictionaryObject({NameObject('/Font'):
                DictionaryObject({NameObject('/F1'): writer._add_object(font)})})
            stream = DecodedStreamObject()
            stream.set_data(b'BT /F1 12 Tf 20 200 Td (PDF text is preserved.) Tj ET')
            page[NameObject('/Contents')] = writer._add_object(stream)
            writer.write(path)
            docs, _ = load_documents(path)
            self.assertIn('PDF text is preserved.', docs[0].text)
            self.assertEqual(docs[0].page, 1)
            blank = PdfWriter()
            blank.add_blank_page(width=300, height=300)
            blank.write(path)
            with self.assertRaisesRegex(ValueError, 'OCR'):
                load_documents(path)

    def test_metrics_require_source_and_evidence(self):
        evidence = [{'source': 'a.md', 'text': 'answer here'}]
        wrong = {'source': 'b.md', 'text': 'answer here'}
        right = {'source': 'a.md', 'text': 'Answer\n here is correct'}
        self.assertEqual(metrics([wrong], evidence)['hit'], 0)
        self.assertEqual(metrics([wrong, right], evidence)['reciprocal_rank'], .5)

    def test_failed_build_preserves_manifest_and_old_collection(self):
        class BrokenEmbedder:
            def identity(self):
                return {'model': 'test', 'digest': 'test'}
            def embed(self, texts):
                self.calls = getattr(self, 'calls', 0) + 1
                if self.calls > 1:
                    raise ValueError('simulated second-strategy failure')
                return [[1., 0., 0.] for _ in texts]
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / 'docs'
            source.mkdir()
            (source / 'a.txt').write_text('word '*25, encoding='utf-8')
            manifest = root / 'index.json'
            manifest.write_text('{"old": true}', encoding='utf-8')
            database = root / 'db'
            with patch('rag.pipeline.TokenCounter', return_value=WordCounter()), \
                 patch('rag.pipeline.OllamaEmbedder', BrokenEmbedder), \
                 patch('rag.pipeline.open_store', side_effect=lambda: QdrantClient(path=str(database))):
                with self.assertRaisesRegex(ValueError, 'simulated'):
                    index(source, manifest, 10, 3)
            self.assertEqual(json.loads(manifest.read_text()), {'old': True})
            client = QdrantClient(path=str(database))
            self.assertEqual(client.get_collections().collections, [])
            client.close()

    def test_index_reopen_search_and_changed_model(self):
        class Embedder:
            digest = 'fixture-v1'
            def __init__(self, **_):
                pass
            def identity(self):
                return {'model': 'fixture', 'digest': self.digest}
            def embed(self, texts, query=False):
                return [[1., 0., 0.] for _ in texts]
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / 'docs'
            source.mkdir()
            (source / 'operations.md').write_text('# Backups\nCopies are retained for fourteen days.', encoding='utf-8')
            manifest, database = root / 'index.json', root / 'db'
            with patch('rag.pipeline.TokenCounter', return_value=WordCounter()), \
                 patch('rag.pipeline.OllamaEmbedder', Embedder), \
                 patch('rag.pipeline.open_store', side_effect=lambda: QdrantClient(path=str(database))):
                index(source, manifest, 20, 3)
                session = SearchSession(manifest, rerank=False)
                try:
                    hits = session.search('Retention?', rerank=False)['results']
                    self.assertEqual(hits[0]['source'], 'operations.md')
                    self.assertIn('fourteen days', hits[0]['text'])
                    self.assertIn('vector_score', hits[0])
                    self.assertNotIn('rerank_score', hits[0])
                finally:
                    session.close()
                Embedder.digest = 'fixture-v2'
                with self.assertRaisesRegex(ValueError, 'Embedding model changed'):
                    SearchSession(manifest, rerank=False)


if __name__ == '__main__':
    unittest.main()
