"""Bounded retrieval settings and conservative, local query rewriting."""
import math
import re


def rewrite_query(query):
    # Remove only presentation requests at the edges. Keep facts, negations,
    # identifiers and the original language; never invent a semantic expansion.
    cleaned = query.strip()
    prefix = r'^(?:(?:please|пожалуйста)[, :]+|(?:can you tell me|could you explain|tell me|объясни мне|расскажи мне)[, :]+)'
    while True:
        shorter = re.sub(prefix, '', cleaned, flags=re.I).strip()
        if shorter == cleaned:
            break
        cleaned = shorter
    cleaned = re.sub(r'((?:[.!?]\s*|[,;]\s*))(?:answer briefly|keep the answer short|ответь кратко|напиши кратко)[.!?]*$', r'\1', cleaned, flags=re.I).strip()
    return cleaned or query.strip()


def validate_options(candidates, top_k, rerank, min_similarity, rewrite, min_rerank_score=None):
    if type(candidates) is not int or type(top_k) is not int or not 1 <= top_k <= 20 or not top_k <= candidates <= 100:
        raise ValueError('Require 1 <= top_k <= 20 and top_k <= candidates <= 100')
    if type(rerank) is not bool or type(rewrite) is not bool:
        raise ValueError('rerank and rewrite must be booleans')
    if min_similarity is not None and (type(min_similarity) not in (int, float) or not math.isfinite(min_similarity) or not -1 <= min_similarity <= 1):
        raise ValueError('min_similarity must be a finite cosine threshold in [-1, 1] or null')

    if min_rerank_score is not None and (not rerank or type(min_rerank_score) not in (int, float) or not math.isfinite(min_rerank_score)):
        raise ValueError('min_rerank_score requires rerank=true and a finite raw logit threshold')


def extra_options(value):
    threshold, rewrite = value.get('min_similarity'), value.get('rewrite', False)
    rerank = value.get('rerank')
    validate_options(value.get('candidates', 20), value.get('top_k', 5), True if rerank is None else rerank, threshold, rewrite, value.get('min_rerank_score'))
    return {'min_similarity': threshold, 'rewrite': rewrite, 'min_rerank_score': value.get('min_rerank_score')}
