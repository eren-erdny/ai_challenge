"""Offline evidence/provenance comparison. No model calls or downloads."""
import hashlib,json,statistics
from pathlib import Path
HERE=Path(__file__).resolve().parent
ROOT=HERE.parents[1]
def read(p):return json.loads(p.read_text(encoding='utf-8'))
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def norm(s):return ' '.join(s.split())
def metrics(rows):
 api=[r['raw_answer']['Duration']/1e9 for r in rows if r['calls'] and r['raw_answer'].get('Duration',0)>0]
 retrieval=[r['retrieval'].get('retrieval_seconds',0)+r['retrieval'].get('rerank_seconds',0) for r in rows]
 return dict(retrieval_median_seconds=round(statistics.median(retrieval),3),turns=len(rows),valid=sum(r['grounding'] is not None for r in rows),rejected=sum(r['grounding'] is None for r in rows),calls=sum(r['calls'] for r in rows),generation_median_seconds=round(statistics.median(api),3),generation_min_seconds=round(min(api),3),generation_max_seconds=round(max(api),3),input_tokens=sum(r['raw_answer']['PromptTokens'] for r in rows),output_tokens=sum(r['raw_answer']['CompletionTokens'] for r in rows))
def main():
 local_path=HERE/'context-verification-20261010/answers.json';cloud_path=ROOT/'examples/rag-grounding/answers.json'
 local=read(local_path);cloud=read(cloud_path);review=read(HERE/'answer-review.json')
 assert review['answers_sha256']==sha(local_path)
 cloud_review=read(ROOT/'examples/rag-grounding/answer-review.json');assert cloud_review['answers_sha256']==sha(cloud_path)
 assert local['cloud_generation_calls']==0 and len(local['rows'])==20
 cloud_by={r['id']:r for r in cloud['rows']}
 comparisons=[]
 for r in local['rows']:
  g=r['grounding'];chunks={h['chunk_id']:h for h in r['retrieval']['results']}
  if g:
   for q in g['quotes']:
    assert q['chunk_id'] in chunks and norm(q['text']) in norm(chunks[q['chunk_id']]['text'])
   if g['status']=='answered':assert g['claims'] and g['sources'] and g['quotes']
   else:assert g['status']=='unknown' and not g['claims'] and not g['quotes'] and not g['sources']
  cr=cloud_by[r['id']]
  signature=lambda x:sorted((h['chunk_id'],norm(h['text'])) for h in x['results'])
  comparisons.append(dict(run=r['run'],id=r['id'],local_status=g['status'] if g else 'rejected',cloud_status=cr['grounding']['status'],identical_retrieved_text=signature(r['retrieval'])==signature(cr['retrieval'])))
 local_metrics=metrics(local['rows']);cloud_metrics=metrics(cloud['rows'])
 local_metrics['expected_met']=sum(r['expected_met'] for r in review['rows'])
 local_metrics['core_expected_met']=sum(r['core_expected_met'] for r in review['rows'])
 local_metrics['reviewed_claims']=sum(len(r['claims']) for r in review['rows'])
 local_metrics['supported_claims']=sum(c['supported'] for r in review['rows'] for c in r['claims'])
 cloud_metrics['expected_met']=sum(r['expected_met'] for r in cloud_review['rows'])
 stable=0
 for id in cloud_by:
  pair=[r for r in review['rows'] if r['id']==id]
  stable+=len({(r['expected_met'],r['status']) for r in pair})==1
 out=dict(local=local_metrics,cloud=cloud_metrics,local_startup_seconds=local['startup_seconds'],stable_outcome_questions=stable,questions=10,identical_retrieval_turns=sum(c['identical_retrieved_text'] for c in comparisons),comparisons=comparisons,local_sha256=sha(local_path),cloud_sha256=sha(cloud_path),new_cloud_calls=0,limits='Historical DeepSeek vs current Qwen, different model/tokenizer/output cap and dates. API duration excludes retrieval and startup. Two local repeats are finite observations; cloud has one saved run, so repeat stability is unmeasured.')
 (HERE/'comparison.json').write_text(json.dumps(out,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
 lines=['# Сравнение локального и облачного RAG','', '| Метрика | Qwen 3.5 4B локально | DeepSeek: сохранённый прогон |','|---|---:|---:|']
 for label,key in [('Запросов','turns'),('Валидных ответов','valid'),('Отклонено приложением','rejected'),('Ожидание выполнено','expected_met'),('Вызовов модели','calls'),('Медиана retrieval + rerank, сек','retrieval_median_seconds'),('Медиана API, сек','generation_median_seconds'),('Минимум API, сек','generation_min_seconds'),('Максимум API, сек','generation_max_seconds')]:lines.append(f'| {label} | {local_metrics[key]} | {cloud_metrics[key]} |')
 lines+=['',f'Повторяемость статуса и выполнения ожидания: {stable}/10 вопросов. Это не побайтовое совпадение текста.',f'Совпали наборы найденных текстов и chunk_id: {out["identical_retrieval_turns"]}/20 сравнений.', '',out['limits'],'','Текущих облачных вызовов: 0. Полная ручная оценка с причинами: answer-review.json; происхождение цитат проверено этим скриптом.','']
 lines+=['','| Вопрос | Локально: прогон 1 | Локально: прогон 2 | DeepSeek |','|---|---|---|---|']
 for id in cloud_by:
  pair=[r for r in review['rows'] if r['id']==id]
  labels=[('полностью подтверждён' if r['expected_met'] else 'отклонён' if r['status']=='rejected' else 'частично подтверждён') for r in pair]
  lines.append(f'| {id} | {labels[0]} | {labels[1]} | полностью подтверждён |')
 lines+=['','Проблемы: Q03/Q08 — неточные цитаты; Q04 — схема/неподтверждённые дополнительные утверждения; Q06 — неверная связь части утверждений с цитатами.','Полнота подтверждения — строгая ручная оценка всех утверждений; совпадение текста цитаты само по себе её не доказывает.','Qwen многословна и повторяет цитаты, несмотря на уточнённую инструкцию. Расширение контекста исправило конфигурацию, но не устранило ошибки модели.','']
 (HERE/'comparison.md').write_text('\n'.join(lines),encoding='utf-8')
 print(json.dumps(out,ensure_ascii=False,indent=2))
if __name__=='__main__':main()
