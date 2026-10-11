"""Offline report from saved real runs and hash-bound manual semantic reviews."""
import hashlib,json,statistics
from pathlib import Path
HERE=Path(__file__).resolve().parent
def read(p):return json.loads(p.read_text(encoding='utf-8-sig'))
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def norm(s):return ' '.join(s.split())
def med(v):return round(statistics.median(v),3)
def main():
 result={};signature=None
 for variant in ['before','q4','q8']:
  directory=HERE/f'{variant}-20261010';path=directory/'answers.json'
  report=read(path);rows=report['rows'];review=read(directory/'review.json')
  assert len(rows)==20 and report['cloud_generation_calls']==0
  assert review['answers_sha256']==sha(path) and len(review['rows'])==20
  local_signature=[];alloc=[];sizes=[]
  for row,checked in zip(rows,review['rows']):
   assert (row['run'],row['id'])==(checked['run'],checked['id'])
   g=row['grounding'];chunks={c['chunk_id']:c for c in row['retrieval']['results']}
   local_signature.append((row['run'],row['id'],sorted((c['chunk_id'],norm(c['text'])) for c in chunks.values())))
   assert len(checked['claims'])==len(g['claims'] if g else [])
   if g:
    quotes={q['id']:q for q in g['quotes']}
    for q in quotes.values():assert q['chunk_id'] in chunks and norm(q['text']) in norm(chunks[q['chunk_id']]['text'])
    for claim,verdict in zip(g['claims'],checked['claims']):
     assert claim['text']==verdict['text'] and claim['quote_ids']==verdict['quote_ids'] and verdict['reason']
     assert claim['quote_ids'] and all(q in quotes for q in claim['quote_ids'])
    assert checked['expected_met']==(checked['core_expected_met'] and all(c['supported'] for c in checked['claims']))
   else:assert not checked['expected_met']
   if row['id']=='Q09':assert row['calls']==0 and g['status']=='unknown' and g['local']
   if row['calls']:
    runtime=[m for m in row['runtime']['models'] if m['name']==report['model']]
    assert len(runtime)==1
    alloc.append(runtime[0]['size_vram']/2**20);sizes.append(runtime[0]['size']/2**20)
  if signature is None:signature=local_signature
  else:assert signature==local_signature,'retrieved text changed'
  api=[r['raw_answer']['Duration']/1e9 for r in rows if r['calls']]
  samples=read(directory/'resources.json');gpu=[[float(x.strip()) for x in s['gpu_csv'].split(',')] for s in samples]
  # Measure-Object emits null for an empty process set (e.g. before worker startup).
  # Peak RSS is computed from observed process sums; absent processes add zero.
  for sample in samples:
   for key in ['ollama_working_set_bytes','python_working_set_bytes']:
    if sample[key] is None:sample[key]=0
  profile=read(HERE/f'{variant}.json')['profiles']['ollama_optimized']
  for row in rows:
   if not row['calls']:continue
   requestpath=directory/f"request-{variant}-{row['run']}-{row['id']}.json"
   if not requestpath.exists():requestpath=directory/f"request-{row['run']}-{row['id']}.json"
   req=read(requestpath)
   assert req['Model']==profile['model'] and req['MaxOutputTokens']==profile['max_output_tokens'] and req['Temperature']==(0 if variant=='before' else 0.1)
   assert req['JSONOutput'] and not req['Tools'] and req['BaseURL']=='http://127.0.0.1:11434/v1'
   assert ('TASK TEMPLATE:' in '\n'.join(m['content'] for m in req['Messages']))==(variant!='before')
   assert all(m['context_length']==profile['context_window'] for m in row['runtime']['models'] if m['name']==profile['model'])
  stable=sum(len({(r['status'],r['expected_met']) for r in review['rows'] if r['id']==id})==1 for id in {r['id'] for r in rows})
  result[variant]=dict(turns=20,valid=sum(r['grounding'] is not None for r in rows),fully_supported=sum(r['expected_met'] for r in review['rows']),calls=sum(r['calls'] for r in rows),claims=sum(len(r['claims']) for r in review['rows']),supported_claims=sum(c['supported'] for r in review['rows'] for c in r['claims']),api_median_seconds=med(api),turn_median_seconds=med([r['total_seconds'] for r in rows]),output_tokens=sum(r['raw_answer']['CompletionTokens'] for r in rows),input_tokens=sum(r['raw_answer']['PromptTokens'] for r in rows),model_allocated_vram_mib=med(alloc),model_disk_mib=round(next(m['size'] for m in read(HERE/'model-inventory.json') if m['name']==report['model'])/2**20,3),gpu_total_peak_mib=max(g[0] for g in gpu),gpu_total_median_mib=med([g[0] for g in gpu]),gpu_util_median_percent=med([g[1] for g in gpu]),gpu_power_median_watts=med([g[2] for g in gpu]),ollama_named_process_rss_peak_mib=round(max(s['ollama_working_set_bytes'] for s in samples)/2**20,3),python_rss_peak_mib=round(max(s['python_working_set_bytes'] for s in samples)/2**20,3),resource_samples=len(samples),stable_questions=stable,quantization=read(directory/'model.json')['details']['quantization_level'],answers_sha256=sha(path))
 out=dict(variants=result,identical_retrieval=True,new_cloud_calls=0,limits='Known ten-question workload, two repeats, sequential warm runs; not held out. Baseline vs tuned changes several factors together. GPU total and aggregate process RSS include background/resident models. RSS filter *ollama* EXCLUDES llama-server runners: it is not total model RAM. Python filter includes all matching processes. size_vram is per-model allocated memory, not measured request peak. Q4/Q8 uses the same model family/size, no BF16 control.')
 (HERE/'comparison.json').write_text(json.dumps(out,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
 lines=['# Оптимизация локального RAG: измеренное сравнение','','| Метрика | До | Compact Q4 | Compact Q8 |','|---|---:|---:|---:|']
 for title,key in [('Полностью подтверждено / 20','fully_supported'),('Принято приложением / 20','valid'),('Медиана API, сек','api_median_seconds'),('Медиана полного хода, сек','turn_median_seconds'),('Всего output tokens','output_tokens'),('Утверждений','claims'),('Подтверждённых утверждений','supported_claims'),('VRAM модели, MiB','model_allocated_vram_mib'),('Модель на диске, MiB','model_disk_mib'),('Общая GPU VRAM: пик, MiB','gpu_total_peak_mib'),('Процессы *ollama*: пик RSS, MiB','ollama_named_process_rss_peak_mib'),('Python aggregate RSS: пик, MiB','python_rss_peak_mib'),('Повторяемость статуса/качества / 10','stable_questions')]:
  lines.append('| '+title+' | '+' | '.join(str(result[v][key]) for v in result)+' |')
 lines+=['','Индекс и найденные тексты совпали во всех вариантах. По 18 вызовов генерации на 20 ходов; два хода Q09 без доказательств обработаны локально без LLM.','',out['limits'],'','| Вопрос | До: 1 / 2 | Q4: 1 / 2 | Q8: 1 / 2 |','|---|---|---|---|']
 reviews={v:read(HERE/f'{v}-20261010/review.json')['rows'] for v in result}
 for id in sorted({r['id'] for r in rows}):
  cells=[' / '.join('полный' if r['expected_met'] else 'отказ валидации' if r['status']=='rejected' else 'частичный' for r in reviews[v] if r['id']==id) for v in result]
  lines.append('| '+id+' | '+' | '.join(cells)+' |')
 lines+=['','Ручная оценка привязана к SHA256 ответов: review.json в каждой директории. Формальная точность цитат проверяется отдельно и не заменяет оценку смысла. Новых облачных вызовов: 0.','']
 (HERE/'comparison.md').write_text('\n'.join(lines),encoding='utf-8')
 print(json.dumps(result,indent=2))
if __name__=='__main__':main()
