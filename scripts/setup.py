"""Prepare a local wiki workspace without replacing existing files.

  python3 scripts/setup.py                                   새 작업 공간이면 원천을 체크박스로 고른다 (터미널)
  python3 scripts/setup.py --source documents=~/docs --source claude=~/.claude/projects/X
                                                             에이전트·스크립트용. 기존 설정에는 원천을 추가한다
  python3 scripts/setup.py --list-kinds                      등록할 수 있는 원천 종류
"""
from pathlib import Path
import argparse
import os
import re
import subprocess
import sys
import tomllib
from skill_install import install_skill
from source_kinds import KINDS, WIKI_BLOCK, choose_alias, default_path, parse_spec, source_block

ROOT = Path(__file__).resolve().parents[1]


def resolve(path, base):
    p = Path(path).expanduser()
    return p if p.is_absolute() else base / p


def source_blocks(specs, taken, base):
    blocks = []
    for kind, alias, path in specs:
        if not resolve(path, base).exists():
            raise ValueError(f'원천 경로가 없습니다: {kind} {path}')
        alias = alias or choose_alias(kind, path, taken)
        if alias in taken:
            raise ValueError(f'이미 등록된 원천 별칭입니다: {alias}')
        taken.add(alias)
        blocks.append(source_block(alias, kind, path))
    return blocks


def ask_sources(base):
    from checkbox import checkbox
    kinds = checkbox('등록할 원천을 고르세요', [(k, v[0]) for k, v in KINDS.items()])
    specs = []
    for kind in kinds:
        default = default_path(kind, base)
        while True:
            answer = input(f'{kind} 경로' + (f' [{default}]' if default else '') + ' (여러 개면 쉼표): ').strip() or default or ''
            paths = [x.strip() for x in answer.split(',') if x.strip()]
            missing = [x for x in paths if not resolve(x, base).exists()]
            if paths and not missing:
                break
            print('없는 경로: ' + ', '.join(missing) if missing else '경로를 입력하세요.')
        specs += [(kind, None, x) for x in paths]
    return specs


def kinds_text():
    return '\n'.join(f'  {k:<11} {v[0]}' for k, v in KINDS.items())


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--source', action='append', default=[], metavar='KIND[:ALIAS]=PATH', help='등록할 원천. 여러 번 쓸 수 있다')
    parser.add_argument('--list-kinds', action='store_true', help='원천 종류를 보여 주고 끝낸다')
    args = parser.parse_args(argv)
    if args.list_kinds:
        print(kinds_text())
        return 0
    config = Path(os.environ.get('WIKI_CONFIG', ROOT / 'wiki.toml')).expanduser().resolve()
    config.parent.mkdir(parents=True, exist_ok=True)
    try:
        specs = [parse_spec(x) for x in args.source]
        if not config.exists():
            if not specs:
                if not (sys.stdin.isatty() and sys.stdout.isatty()):
                    print('등록할 원천을 --source kind=path 로 지정하세요. 에이전트는 사용자에게 원천 종류와 경로를 먼저 묻는다.\n'
                          f'종류:\n{kinds_text()}\n예: python3 -B scripts/setup.py --source documents=../docs --source slack=../exports/slack',
                          file=sys.stderr)
                    return 2
                specs = ask_sources(config.parent)
            config.write_text(WIKI_BLOCK + ''.join('\n' + b for b in source_blocks(specs, set(), config.parent)), encoding='utf-8')
        elif specs:
            old = config.read_text(encoding='utf-8-sig')
            taken = set(tomllib.loads(old).get('sources', {}))
            blocks = source_blocks(specs, taken, config.parent)
            config.write_text(old.rstrip('\n') + '\n' + ''.join('\n' + b for b in blocks), encoding='utf-8')
    except (ValueError, KeyboardInterrupt) as error:
        print(f'ERROR: {error}' if isinstance(error, ValueError) else '취소했습니다.', file=sys.stderr)
        return 1
    from settings import WIKI_ROOT, REPO_ROOT, STATE_ROOT, WORKSPACE
    from wikilib import TYPE_FOLDERS
    for name, root in (('wiki.path', WIKI_ROOT), ('wiki.state', STATE_ROOT)):
        if not root.is_relative_to(REPO_ROOT) or root == REPO_ROOT:
            raise ValueError(f'{name} must be a subdirectory of the workspace')
    if STATE_ROOT.is_relative_to(WIKI_ROOT) or WIKI_ROOT.is_relative_to(STATE_ROOT):
        raise ValueError('wiki.state must be separate from wiki.path; drafts and records are not adopted knowledge')
    WIKI_ROOT.mkdir(parents=True, exist_ok=True)
    for category in TYPE_FOLDERS.values():
        (WIKI_ROOT / category).mkdir(exist_ok=True)
    (STATE_ROOT / 'reviews').mkdir(parents=True, exist_ok=True)
    schema = WIKI_ROOT / 'SCHEMA.md'
    if not schema.exists():
        schema.write_text((ROOT / 'SCHEMA.md').read_text(encoding='utf-8-sig'), encoding='utf-8')
    if not (WIKI_ROOT / 'index.md').exists():
        subprocess.run([sys.executable, '-B', str(ROOT/'scripts/build_index.py')], check=True)
    for provider in ('.claude', '.agents'):
        for skill in sorted((ROOT / 'skills').iterdir()):
            if not (skill / 'SKILL.md').is_file(): continue
            dest = WORKSPACE / provider / 'skills' / skill.name
            install_skill(skill, dest)
    if WORKSPACE != ROOT:
        pointer = f"\n<!-- company-wiki -->\nWiki rules: {WIKI_ROOT / 'SCHEMA.md'}. Read {WIKI_ROOT / 'index.md'} first. Drafts and records: {STATE_ROOT}.\nToolkit and skills: {ROOT}. Use WIKI_CONFIG={config} with its scripts.\n<!-- /company-wiki -->\n"
        target = WORKSPACE / 'AGENTS.md'
        old = target.read_text(encoding='utf-8-sig') if target.exists() else ''
        pattern = r'\n?<!-- (?:llm|boaz|company)-wiki -->.*?<!-- /(?:llm|boaz|company)-wiki -->\n?'
        if re.search(pattern, old, flags=re.S):
            updated = re.sub(pattern, lambda _: pointer, old, count=1, flags=re.S)
        else:
            updated = old + pointer
        if updated != old:
            target.write_text(updated, encoding='utf-8')
    from settings import EXTRA_ROOTS, SOURCES
    print(f'Workspace ready: {WIKI_ROOT} (state: {STATE_ROOT})')
    for alias, entry in SOURCES.items():
        root = EXTRA_ROOTS.get(alias)
        state = '' if root is None or root.exists() else '  ← 경로 없음'
        print(f"  source {alias:<16} {entry['kind']:<11} {entry['path']}{state}")
    if not SOURCES:
        print('  등록된 원천이 없습니다. --source kind=path 로 추가하세요.')
    print(f'Config: {config}. Follow the README starter prompt.')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
