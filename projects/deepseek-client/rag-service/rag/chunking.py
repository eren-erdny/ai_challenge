import hashlib
import json
import re

from .documents import sections


class TokenCounter:
    def __init__(self, model='nomic-ai/nomic-embed-text-v1.5', cache_dir=None, download=False):
        from transformers import AutoTokenizer
        from huggingface_hub import snapshot_download
        location = model if download else snapshot_download(model, cache_dir=cache_dir, local_files_only=True)
        self.tokenizer = AutoTokenizer.from_pretrained(location, cache_dir=cache_dir, trust_remote_code=False,
                                                       local_files_only=not download)
        if not self.tokenizer.is_fast:
            raise ValueError('A fast tokenizer with offset mapping is required')

    def offsets(self, text):
        return self.tokenizer(text, add_special_tokens=False, return_offsets_mapping=True,
                              truncation=False)['offset_mapping']

    def count(self, text):
        return len(self.offsets(text))


def windows(text, counter, size, overlap, prefer_boundaries=False):
    """Token-limited slices of original text, preserving code whitespace."""
    offsets = counter.offsets(text)
    start = 0
    while start < len(offsets):
        end = min(start + size, len(offsets))
        char_start = 0 if start == 0 else offsets[start][0]
        char_end = len(text) if end == len(offsets) else offsets[end][0]
        if prefer_boundaries and end < len(offsets):
            boundaries = [m.end() + char_start for m in re.finditer(r'\n\s*\n|[.!?]\s+', text[char_start:char_end])]
            choices = [i for i in range(start + max(overlap + 1, size // 2), end + 1)
                       if any(offsets[i - 1][1] <= b <= offsets[i][0] for b in boundaries)]
            if choices:
                end = choices[-1]
                char_end = offsets[end][0]
        part = text[char_start:char_end]
        # A slice beginning inside a word may tokenize differently in isolation.
        while counter.count(part) > size and end > start + 1:
            end -= 1
            char_end = offsets[end][0]
            part = text[char_start:char_end]
        if counter.count(part) > size:
            raise ValueError('Cannot fit a token span within the configured chunk size')
        if part.strip():
            yield char_start, char_end, part
        if end == len(offsets):
            break
        start = max(start + 1, end - overlap)


def chunk_documents(docs, strategy, counter, size=400, overlap=60):
    if size < 8 or not 0 <= overlap < size:
        raise ValueError('Require size >= 8 and 0 <= overlap < size')
    if strategy not in {'fixed', 'structured'}:
        raise ValueError(f'Unknown strategy: {strategy}')
    result = []
    for doc in docs:
        spans = sections(doc)
        units = [(0, len(doc.text), '')] if strategy == 'fixed' else spans
        for first, last, label in units:
            for a, b, text in windows(doc.text[first:last], counter, size, overlap, strategy == 'structured'):
                begin, end = first + a, first + b
                section = label or ' | '.join(s for x, y, s in spans if x < end and y > begin)
                identity = json.dumps([doc.source, doc.page, strategy, begin, end, text], ensure_ascii=False)
                result.append({
                    'chunk_id': hashlib.sha256(identity.encode()).hexdigest(),
                    'source': doc.source, 'file': doc.source, 'title': doc.title,
                    'section': section, 'page': doc.page, 'strategy': strategy,
                    'start_char': begin, 'end_char': end, 'text': text,
                    'tokens': counter.count(text),
                })
    if not result:
        raise ValueError('Chunking produced no text')
    return result
