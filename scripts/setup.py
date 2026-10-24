"""Prepare a local wiki workspace without replacing existing files."""
from pathlib import Path
import os
import re
import subprocess
import sys
from skill_install import install_skill

ROOT = Path(__file__).resolve().parents[1]

def main():
    config = Path(os.environ.get('WIKI_CONFIG', ROOT / 'wiki.toml')).expanduser().resolve()
    config.parent.mkdir(parents=True, exist_ok=True)
    if not config.exists():
        config.write_bytes((ROOT / 'wiki.example.toml').read_bytes())
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
    print(f'Workspace ready: {WIKI_ROOT} (state: {STATE_ROOT})\nConfigure sources in {config}; follow the README starter prompt.')

if __name__ == '__main__':
    main()
