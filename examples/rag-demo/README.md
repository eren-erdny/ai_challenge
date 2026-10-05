# Homework 22: RAG comparison

The corpus is the complete public [RFC 7168](https://www.rfc-editor.org/rfc/rfc7168.txt), downloaded from RFC Editor. It is an informational April Fools' protocol, not an Internet Standard. Its copyright and license notice are preserved in the file.

`questions.json` contains ten questions, expected facts and source sections. Questions Q09 and Q10 deliberately have no supporting answer. The expectations were written before the model runs.

In the ordinary client, press F2 (or type `/kb`), create a base from `examples/rag-demo/corpus`, and ask a question. To compare without retrieval, use F2 → 6. To enable retrieval again, use F2 → 2 and select the base. Both modes use the same LLM; the selected knowledge base controls retrieval.

For the recorded comparison, input is scripted through the real TUI. The recording helper uses an isolated chat, no conversation history, no extra model tools, temperature zero, and a bounded response length. Every answer comes from the real DeepSeek API. A temporary status overlay labels each question and mode. The temporary helper does not change the production client.

The comparison separates content correctness from exact source/chunk citations and whether the required passage was actually retrieved. Simple phrase checks are screening signals; the answers need human review. An answer without RAG may be correct from the model's prior knowledge. This small corpus and question set do not establish general RAG quality.

## Recorded result

The live run completed all twenty requests. See [comparison.md](comparison.md) for the complete answers and review, and [comparison.json](comparison.json) for API usage and retrieved text. Automatic phrase checks scored 3/10 without RAG and 8/10 with RAG. Review found only one fully correct baseline answer plus two partial answers with false additions; RAG had eight correct answers and two retrieval misses caused by page headers/footers. Exact chunk IDs alone do not prove relevant evidence.

The verified silent OBS recording is `C:/Users/erdni/Videos/ai_challenge_hw22.mp4`. No production client source was changed for filming.
