#!/usr/bin/env python3
"""llm-wiki/index.md 를 frontmatter 에서 생성한다. 손으로 편집하지 않는다. 실행: python3 scripts/build_index.py

에이전트와 사람이 처음 읽는 입구다. 질문 → 페이지, 폴더별 핵심 설명·담당자·별칭·관계, 관계 목록 순서로 쓴다.
"""
from __future__ import annotations

import sys
from collections import defaultdict
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from settings import TITLE  # noqa: E402
from wikilib import TYPE_FOLDERS, WIKI_ROOT, build_resolver, iter_pages, parse_frontmatter, relations, slug_of  # noqa: E402

SECTIONS = {
    "concepts": "개념 (concepts) — 무엇을 의미하는가",
    "entities": "대상 (entities) — 무엇이 존재하는가",
    "policies": "정책 (policies) — 어떻게 판단하고 행동하는가",
}


def as_list(value) -> list[str]:
    if isinstance(value, list):
        return [str(x) for x in value if str(x).strip()]
    return [str(value)] if isinstance(value, str) and value.strip() else []


def main() -> int:
    paths = list(iter_pages())
    resolve = build_resolver(paths)
    pages = []
    for p in paths:
        fm, _ = parse_frontmatter(p.read_text(encoding="utf-8-sig"))
        if fm:
            pages.append((slug_of(p), fm))
    pages.sort(key=lambda x: x[0])
    titles = {s: str(fm.get("title", s)) for s, fm in pages}

    def link(slug: str) -> str:
        return f"[[{slug}|{titles.get(slug, slug)}]]"

    def target(r: dict) -> str:
        return resolve(str(r.get("target"))) or str(r.get("target"))

    now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    out = [
        "---",
        'wiki_format: "2"',
        f"generated: {{ by: process:build_index.py, at: {now} }}",
        "---",
        "",
        f"# {TITLE}",
        "",
        f"조직이 현재 채택한 지식 {len(pages)}개. 이 파일은 `scripts/build_index.py`가 생성한다. "
        "질문으로 찾고, 없으면 폴더별 목록에서 별칭과 관계를 따라간다. 규칙은 [[SCHEMA]].",
        "",
    ]

    questions = sorted((q, s) for s, fm in pages for q in as_list(fm.get("answers")))
    if questions:
        out += ["## 질문으로 찾기", ""]
        out += [f"- {q} → {link(s)}" for q, s in questions]
        out.append("")

    groups: dict[str, dict[str, list[tuple[str, dict]]]] = defaultdict(lambda: defaultdict(list))
    for s, fm in pages:
        parts = s.split("/")
        groups[parts[0]]["/".join(parts[1:-1])].append((s, fm))
    for folder in TYPE_FOLDERS.values():
        if folder not in groups:
            continue
        out += [f"## {SECTIONS[folder]} · {sum(len(v) for v in groups[folder].values())}", ""]
        for sub in sorted(groups[folder]):
            if sub:
                out += [f"### {sub}", ""]
            for s, fm in groups[folder][sub]:
                owner = f" · 담당 {fm['owner']}" if fm.get("owner") else ""
                out.append(f"- {link(s)} — {fm.get('description', '')}{owner}")
                aliases = as_list(fm.get("aliases"))
                if aliases:
                    out.append(f"  - 별칭: {', '.join(aliases)}")
                rels = [(f"{r['type']} → " if r.get("type") else "") + link(target(r)) for r in relations(fm)]
                if rels:
                    out.append(f"  - 관계: {' · '.join(rels)}")
            out.append("")

    typed = [(s, r) for s, fm in pages for r in relations(fm) if r.get("type")]
    if typed:
        out += ["## 관계 목록", "", "| 출발 | 관계 | 대상 | 근거 |", "|---|---|---|---|"]
        for s, r in typed:
            evidence = f"`{r['evidence']}`" if r.get("evidence") else ""
            # Table cells cannot hold the `|` of an aliased wikilink.
            out.append(f"| [[{s}]] | {r['type']} | [[{target(r)}]] | {evidence} |")
        out.append("")

    (WIKI_ROOT / "index.md").write_text("\n".join(out), encoding="utf-8")
    print(f"index.md: {len(pages)} pages")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
