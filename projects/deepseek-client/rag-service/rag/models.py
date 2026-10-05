import math
import os
from pathlib import Path

import httpx

ROOT = Path(__file__).resolve().parents[1]
DATA = Path(os.environ.get('RAG_DATA_DIR', ROOT / 'data')).resolve()
CACHE = Path(os.environ.get('RAG_MODEL_CACHE', DATA / 'models')).resolve()
os.environ.setdefault('HF_HUB_DISABLE_TELEMETRY', '1')
os.environ.setdefault('HF_HUB_DISABLE_XET', '1')
RERANK_MODEL = 'cross-encoder/mmarco-mMiniLMv2-L12-H384-v1'


class OllamaEmbedder:
    def __init__(self, url=None, model=None):
        self.url = (url or os.environ.get('OLLAMA_URL', 'http://localhost:11434')).rstrip('/')
        self.model = model or os.environ.get('RAG_EMBEDDING_MODEL', 'nomic-embed-text:latest')
        self.digest = None

    def identity(self):
        with httpx.Client(timeout=30, trust_env=False) as client:
            response = client.get(self.url + '/api/tags')
            response.raise_for_status()
        entry = next((m for m in response.json()['models'] if m['name'] == self.model), None)
        if entry is None:
            raise ValueError(f'Ollama model not installed: {self.model}')
        self.digest = entry['digest']
        return {'model': self.model, 'digest': self.digest}

    def embed(self, texts, query=False):
        prefix = 'search_query: ' if query else 'search_document: '
        vectors = []
        with httpx.Client(timeout=180, trust_env=False) as client:
            for first in range(0, len(texts), 16):
                batch = texts[first:first + 16]
                response = client.post(self.url + '/api/embed', json={
                    'model': self.model, 'input': [prefix + text for text in batch],
                    'truncate': False, 'keep_alive': '10m',
                })
                response.raise_for_status()
                values = response.json()['embeddings']
                if len(values) != len(batch):
                    raise ValueError('Ollama returned an incorrect embedding count')
                vectors.extend(values)
        if not vectors or not vectors[0]:
            raise ValueError('No embeddings returned')
        dimension = len(vectors[0])
        if any(len(v) != dimension or not all(math.isfinite(x) for x in v)
               or sum(x*x for x in v) == 0 for v in vectors):
            raise ValueError('Invalid embedding vector')
        return vectors


class Reranker:
    """CPU cross-encoder with sliding windows, so passage tails are not discarded."""
    def __init__(self, model=RERANK_MODEL, download=False):
        import torch
        from transformers import AutoModelForSequenceClassification, AutoTokenizer
        from huggingface_hub import snapshot_download
        torch.set_num_threads(min(8, os.cpu_count() or 1))
        self.torch = torch
        self.name = model
        # A concrete local directory also prevents optional Transformers metadata requests.
        location = model if download else snapshot_download(model, cache_dir=str(CACHE), local_files_only=True)
        self.tokenizer = AutoTokenizer.from_pretrained(location, cache_dir=str(CACHE), trust_remote_code=False,
                                                       local_files_only=not download)
        self.model = AutoModelForSequenceClassification.from_pretrained(
            location, cache_dir=str(CACHE), trust_remote_code=False, use_safetensors=True,
            local_files_only=not download).eval()

    def rank(self, query, hits):
        if not hits:
            return []
        tokenizer = self.tokenizer
        qids = tokenizer.encode(query, add_special_tokens=False)
        if len(qids) > 128:
            raise ValueError('Query exceeds 128 reranker tokens; shorten the query')
        budget = 512 - len(qids) - tokenizer.num_special_tokens_to_add(pair=True)
        features, owners = [], []
        for index, hit in enumerate(hits):
            ids = tokenizer.encode(hit['text'], add_special_tokens=False, verbose=False)
            for start in range(0, max(1, len(ids)), budget - 64):
                features.append(tokenizer.prepare_for_model(
                    qids, pair_ids=ids[start:start + budget], add_special_tokens=True,
                    return_attention_mask=True, truncation=False))
                owners.append(index)
                if start + budget >= len(ids):
                    break
        scores = [-float('inf')] * len(hits)
        with self.torch.inference_mode():
            for start in range(0, len(features), 8):
                batch = tokenizer.pad(features[start:start + 8], padding=True, return_tensors='pt')
                logits = self.model(**batch).logits.reshape(-1).tolist()
                for owner, score in zip(owners[start:start + 8], logits, strict=True):
                    if not math.isfinite(score):
                        raise ValueError('Reranker returned a non-finite score')
                    scores[owner] = max(scores[owner], score)
        result = [dict(hit, rerank_score=score) for hit, score in zip(hits, scores, strict=True)]
        return sorted(result, key=lambda hit: hit['rerank_score'], reverse=True)
