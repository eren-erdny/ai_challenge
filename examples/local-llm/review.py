"""Recheck saved local LLM evidence offline; no network requests."""
import ast
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys

CASES = [('', True), ('abc', True), ('()[]{}', True), ('([{}])', True),
         ('a+(b*[c-{d}])', True), ('([)]', False), ('(', False), (')', False),
         ('}{', False), ('(()', False), ('())', False), ('{[()]}x[{}]', True)]

def review(path):
    raw = path.read_bytes()
    report = json.loads(raw.decode('utf-8-sig'))
    assert report['endpoint'] == 'http://127.0.0.1:11434/v1'
    assert report['reasoning_effort'] == 'none'
    answers = report['answers']
    assert [a['id'] for a in answers] == ['simple', 'reasoning', 'code']
    for answer in answers:
        c = answer['completion']
        assert c['FinishReason'] == 'stop' and c['Content'].strip()
        assert c['UsageKnown'] and not c['UsageIncomplete']
        assert c['Model'] == report['model'] and not c['ToolCalls']
    assert answers[0]['completion']['Content'].strip() == 'Париж'
    math = answers[1]['completion']['Content']
    assert all(str(n) in math for n in (240, 70, 310, 31, 279))
    match = re.search(r'```(?:python)?\s*\n(.*?)```', answers[2]['completion']['Content'], re.S)
    assert match, 'Python code block missing'
    code = match.group(1)
    tree = ast.parse(code)
    assert len(tree.body) == 1 and isinstance(tree.body[0], ast.FunctionDef)
    assert tree.body[0].name == 'brackets_balanced'
    # A narrow, offline execution boundary for this specific generated algorithm.
    allowed = (ast.Module, ast.FunctionDef, ast.arguments, ast.arg, ast.Assign,
               ast.List, ast.Dict, ast.Constant, ast.For, ast.If, ast.Compare,
               ast.Name, ast.Load, ast.Store, ast.Expr, ast.Call, ast.Attribute,
               ast.Return, ast.UnaryOp, ast.Not, ast.BoolOp, ast.Or, ast.And,
               ast.In, ast.NotIn, ast.NotEq, ast.Eq, ast.Subscript)
    for node in ast.walk(tree):
        assert isinstance(node, allowed), f'Unreviewed syntax: {type(node).__name__}'
        if isinstance(node, ast.Attribute):
            assert isinstance(node.value, ast.Name) and node.value.id == 'stack'
            assert node.attr in ('pop', 'append')
        if isinstance(node, ast.Call):
            assert (isinstance(node.func, ast.Name) and node.func.id == 'len') or isinstance(node.func, ast.Attribute)
    program = code + '\nchecks=' + repr(CASES) + '\nfor s, expected in checks:\n assert brackets_balanced(s) is expected, (s, expected)\n'
    result = subprocess.run([sys.executable, '-I', '-c', program], capture_output=True, text=True, timeout=5)
    assert result.returncode == 0, result.stderr
    models = report['running_models']['models']
    local = next(m for m in models if m['name'] == report['model'])
    assert local['size_vram'] > 0 and local['size_vram'] == local['size']
    summary = {'answers_sha256': hashlib.sha256(raw).hexdigest(), 'successful_answers': len(answers),
               'simple_correct': True, 'math_correct': True, 'code_passed': len(CASES),
               'gpu_residency': 'full', 'runtime_context': local['context_length'],
               'prompt_tokens': sum(a['completion']['PromptTokens'] for a in answers),
               'completion_tokens': sum(a['completion']['CompletionTokens'] for a in answers),
               'cloud_generation_calls': 0}
    path.with_name('review.json').write_text(json.dumps(summary, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    print(json.dumps(summary, ensure_ascii=False))

if __name__ == '__main__':
    for item in sys.argv[1:] or [str(Path(__file__).parent / 'verification-20261010' / 'answers.json')]:
        review(Path(item))
