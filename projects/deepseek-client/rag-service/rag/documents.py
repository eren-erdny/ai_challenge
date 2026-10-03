import ast
from dataclasses import dataclass
from pathlib import Path
import re
import unicodedata


SUPPORTED = {'.md', '.markdown', '.txt', '.pdf', '.py', '.js', '.ts', '.tsx', '.java', '.go', '.rs', '.cs', '.cpp', '.c', '.h'}
CODE = SUPPORTED - {'.md', '.markdown', '.txt', '.pdf'}
EXCLUDED = {'.git', '.venv', 'node_modules', '__pycache__', 'data', '.codex', '.agents'}


@dataclass
class Document:
    source: str
    title: str
    text: str
    kind: str
    page: int | None = None


def normalize(text, code=False):
    text = text.replace('\r\n', '\n').replace('\r', '\n').replace('\x00', '')
    if code:
        return text  # Indentation and whitespace inside literals can be meaningful.
    text = unicodedata.normalize('NFC', text).replace('\u00ad', '').replace('\u00a0', ' ')
    # Retain Markdown hard line breaks, indentation and fenced-code whitespace.
    return text.strip('\n')


def load_documents(root):
    root = Path(root).resolve()
    if not root.exists():
        raise ValueError(f'Input does not exist: {root}')
    paths = [root] if root.is_file() else sorted(root.rglob('*'))
    docs, skipped = [], []
    for path in paths:
        if not path.is_file() or path.is_symlink():
            continue
        relative = path.relative_to(root.parent if root.is_file() else root)
        if any(part.startswith('.') or part in EXCLUDED for part in relative.parts):
            continue
        kind = path.suffix.lower()
        if kind not in SUPPORTED:
            skipped.append(relative.as_posix())
            continue
        source = relative.as_posix()
        if kind == '.pdf':
            from pypdf import PdfReader
            reader = PdfReader(path)
            for number, page in enumerate(reader.pages, 1):
                text = normalize(page.extract_text() or '')
                if not text:
                    raise ValueError(f'{source}, page {number}: no text; OCR is required before indexing')
                docs.append(Document(source, path.stem, text, kind, number))
        else:
            text = normalize(path.read_text(encoding='utf-8-sig'), code=kind in CODE)
            if text.strip():
                title = path.stem
                if kind in {'.md', '.markdown'}:
                    match = re.search(r'^#\s+(.+)$', text, re.M)
                    if match:
                        title = match[1]
                docs.append(Document(source, title, text, kind))
    if not docs:
        raise ValueError('No supported nonempty documents found')
    return docs, skipped


def sections(doc):
    """Return contiguous character spans; never discard preambles or imports."""
    text = doc.text
    lines = text.splitlines(keepends=True)
    if doc.kind in {'.md', '.markdown'}:
        cuts, stack, fence, offset = [(0, 'Preamble')], [], None, 0
        for line in lines:
            marker = re.match(r'^\s{0,3}(`{3,}|~{3,})', line)
            if marker:
                token = marker[1]
                if fence is None:
                    fence = token
                elif token[0] == fence[0] and len(token) >= len(fence):
                    fence = None
            elif fence is None:
                heading = re.match(r'^\s{0,3}(#{1,6})\s+(.+?)\s*#*\s*$', line)
                if heading:
                    level, name = len(heading[1]), heading[2]
                    while stack and stack[-1][0] >= level:
                        stack.pop()
                    stack.append((level, name))
                    label = ' / '.join(item[1] for item in stack)
                    if offset == 0:
                        cuts[0] = (0, label)
                    else:
                        cuts.append((offset, label))
            offset += len(line)
    elif doc.kind == '.py':
        try:
            tree = ast.parse(text)
        except SyntaxError as exc:
            raise ValueError(f'Cannot parse Python file {doc.source}: {exc}') from exc
        offsets, total = [0], 0
        for line in lines:
            total += len(line)
            offsets.append(total)
        cuts = [(0, 'Module')]
        for node in tree.body:
            if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)):
                first = min([node.lineno] + [x.lineno for x in node.decorator_list])
                start = offsets[first - 1]
                label = f'{type(node).__name__}: {node.name}'
                if start == 0:
                    cuts[0] = (0, label)
                else:
                    cuts.append((start, label))
                end = offsets[node.end_lineno]
                if end < len(text):
                    cuts.append((end, 'Module'))
        cuts = sorted(dict(cuts).items())
    else:
        # Plain text, PDF pages and non-Python code: paragraph structure fallback.
        cuts = [(0, 'Paragraph 1')]
        for i, match in enumerate(re.finditer(r'\n[ \t]*\n', text), 2):
            if match.end() < len(text):
                cuts.append((match.end(), f'Paragraph {i}'))
    return [(start, cuts[i + 1][0] if i + 1 < len(cuts) else len(text), label)
            for i, (start, label) in enumerate(cuts)]
