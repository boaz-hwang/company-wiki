"""Source kinds an organization already runs on, and how setup registers them."""
from pathlib import Path
import re

# kind: (label, default alias, include, exclude)
KINDS = {
    'documents': ('문서 폴더 (Markdown·MDX)', 'documents', ['**/*.md', '**/*.mdx'], ['**/.env*', '**/node_modules/**', '**/.git/**']),
    'code': ('코드 저장소', 'code', [], ['**/.env*', '**/node_modules/**', '**/.git/**']),
    'slack': ('Slack 내보내기 폴더', 'slack', ['**/*.md', '**/*.txt'], []),
    'jira': ('Jira 내보내기 폴더', 'jira', ['**/*.md', '**/*.txt'], []),
    'notion': ('Notion 내보내기 폴더', 'notion', ['**/*.md', '**/*.txt'], []),
    'confluence': ('Confluence 내보내기 폴더', 'confluence', ['**/*.md', '**/*.txt'], []),
    'claude': ('Claude Code 세션 (~/.claude/projects/<프로젝트>)', 'claude-sessions', [], []),
    'codex': ('Codex 세션 (~/.codex/sessions)', 'codex-sessions', [], []),
}
SOURCE_KINDS = set(KINDS)


def default_path(kind, project):
    """Suggested location for session sources; other kinds have no safe default."""
    if kind == 'claude':
        # Claude Code stores a project's sessions under its path with separators replaced by '-'.
        return '~/.claude/projects/' + re.sub(r'[^A-Za-z0-9]', '-', str(Path(project).resolve()))
    if kind == 'codex':
        return '~/.codex/sessions'
    return None


def parse_spec(spec):
    """`kind=path` or `kind:alias=path` → (kind, alias or None, path)."""
    head, sep, path = spec.partition('=')
    kind, _, alias = head.partition(':')
    if not sep or not path.strip():
        raise ValueError(f'--source must be kind=path or kind:alias=path: {spec}')
    if kind not in KINDS:
        raise ValueError(f"Unsupported source kind: {kind} ({', '.join(KINDS)})")
    if alias and not re.fullmatch(r'[A-Za-z0-9_-]+', alias):
        raise ValueError(f'Alias must use letters, digits, - or _: {alias}')
    return kind, alias or None, path.strip()


def choose_alias(kind, path, taken):
    base = KINDS[kind][1]
    if base not in taken:
        return base
    name = re.sub(r'[^a-z0-9]+', '-', Path(path).expanduser().name.lower()).strip('-') or 'source'
    alias, n = f'{kind}-{name}', 2
    while alias in taken:
        alias, n = f'{kind}-{name}-{n}', n + 1
    return alias


def toml_list(values):
    return '[' + ', '.join(f'"{v}"' for v in values) + ']'


def source_block(alias, kind, path):
    if "'" in path or '\n' in path:
        raise ValueError(f'Path cannot contain quotes or newlines: {path}')
    _, _, include, exclude = KINDS[kind]
    lines = [f'[sources.{alias}]', f'kind = "{kind}"', f"path = '{path}'"]  # literal string keeps Windows backslashes
    if include:
        lines.append(f'include = {toml_list(include)}')
    if exclude:
        lines.append(f'exclude = {toml_list(exclude)}')
    return '\n'.join(lines) + '\n'


WIKI_BLOCK = '''# Company Wiki workspace. Paths are relative to this file, or absolute / ~/ paths.
[wiki]
title = "Company Wiki"
# Adopted knowledge only: concepts/, entities/, policies/. Citation hashes live in <path>/.verification/.
path = "llm-wiki"
# Workspace outside the wiki: drafts, reviews, decisions.
state = ".llm-wiki"
reviewer = "human:owner"
language = "ko"
'''
