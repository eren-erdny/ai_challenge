"""Verify saved day-27 answers and persisted history without model requests."""
from pathlib import Path
import hashlib
import json
import sys

def review(folder):
    raw=(folder/'answers.json').read_bytes()
    report=json.loads(raw.decode('utf-8-sig'))
    meta=json.loads((folder/'application.json').read_text(encoding='utf-8-sig'))
    history=json.loads((folder/'history.json').read_text(encoding='utf-8-sig'))
    assert report['endpoint']=='http://127.0.0.1:11434/v1'
    assert report['model']=='batiai/gemma4-12b:iq3'
    assert meta['production_entrypoint']=='ordinary client with ollama_local profile'
    assert meta['history_restored_messages']==4 and meta['final_history_messages']==6
    assert meta['cloud_generation_calls']==0
    answers=report['answers']
    assert [a['id'] for a in answers]==['plan','context','restored']
    assert len(history)==6
    for index,a in enumerate(answers):
        c=a['completion']
        assert c['Content'].strip() and c['FinishReason']=='stop'
        assert c['Model']==report['model'] and c['UsageKnown'] and not c['UsageIncomplete']
        assert history[index*2]['role']=='user' and history[index*2]['content']==a['prompt']
        assert history[index*2+1]['role']=='assistant' and history[index*2+1]['content']==c['Content']
    context=answers[1]['completion']['Content'].casefold()
    assert 'север' in context and ('3' in context or 'три' in context)
    restored=answers[2]['completion']['Content'].casefold()
    assert all(term in restored for term in ['ollama','ответ','истори'])
    summary={'answers_sha256':hashlib.sha256(raw).hexdigest(),'successful_answers':3,
             'context_followup_correct':True,'history_restored_messages':4,'persisted_messages':6,
             'restored_followup_correct':True,'cloud_generation_calls':0,
             'prompt_tokens':sum(a['completion']['PromptTokens'] for a in answers),
             'completion_tokens':sum(a['completion']['CompletionTokens'] for a in answers)}
    (folder/'review.json').write_text(json.dumps(summary,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    print(json.dumps(summary,ensure_ascii=False))

if __name__=='__main__':
    for folder in sys.argv[1:] or [str(Path(__file__).parent/'profile-verification-20261010')]:
        review(Path(folder))
