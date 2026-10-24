#!/usr/bin/env python3
"""결정적 린트. error 가 있으면 exit 1. 실행: python3 scripts/lint.py

error   : frontmatter 없음/깨짐, 필수 키(type·title·description·owner) 누락, 잘못된 type·폴더,
          없는 페이지로의 위키링크·related, 잘못된 관계 type·근거, sources 없음,
          없는 파일·범위 밖 줄을 가리키는 인용, 작업 공간(검토 사본 등)을 근거로 인용
warning : 원문 변경, 세션 원문 만료, 고아 페이지, stale_after 경과, 플레이스홀더 문구,
          제목 중복, Git이 맡는 상태·이력 필드, index.md 가 최신이 아님
info    : 해시 기록 없는 인용(registry.py record), contradicts 관계, 200줄 초과
"""
from __future__ import annotations

import re
import sys
from collections import defaultdict
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from wikilib import (  # noqa: E402
    CITATION_RE, RELATION_TYPES, STATE_ROOT, TYPE_FOLDERS, WIKI_ROOT, WIKILINK_RE, build_resolver, cited_spans,
    freshness, is_session, iter_pages, load_registry, parse_frontmatter, relations, resolve_resource, slug_of,
)

PLACEHOLDER_RE = re.compile(r"(TODO|TBD|원문을 참조|pending review|작성 예정|\(추후\))", re.I)
REQUIRED = ("type", "title", "description", "owner")
# 채택 여부와 이력은 기본 브랜치와 Git이 관리한다.
GIT_MANAGED = ("status", "adopted", "verified", "verification_history", "generated", "supersedes", "contested")


def main() -> int:
    if not WIKI_ROOT.is_dir():
        print(f"[ERROR] Wiki directory not found: {WIKI_ROOT}; run setup.py first")
        return 1
    errors, warnings, infos = [], [], []
    pages = list(iter_pages())
    resolve = build_resolver(pages)
    registry = load_registry()
    inbound: dict[str, int] = defaultdict(int)
    titles: dict[str, list[str]] = defaultdict(list)
    now = datetime.now(timezone.utc)
    line_counts: dict[Path, int] = {}

    for p in pages:
        rel = p.relative_to(WIKI_ROOT).as_posix()
        text = p.read_text(encoding="utf-8-sig")
        fm, body = parse_frontmatter(text)
        if fm is None:
            errors.append(f"{rel}: frontmatter 없음")
            continue
        for k in REQUIRED:
            if not fm.get(k):
                errors.append(f"{rel}: 필수 키 누락 `{k}`")
        t = fm.get("type")
        if t not in TYPE_FOLDERS:
            errors.append(f"{rel}: 알 수 없는 type `{t}` (concept/entity/policy)")
        elif rel.split("/", 1)[0] != TYPE_FOLDERS[t]:
            errors.append(f"{rel}: type `{t}` 는 {TYPE_FOLDERS[t]}/ 아래에 둔다")
        extra = [k for k in GIT_MANAGED if k in fm]
        if extra:
            warnings.append(f"{rel}: {', '.join(extra)} 불필요. 채택은 기본 브랜치 반영, 이력은 Git이 맡는다")
        sources = fm.get("sources")
        if not isinstance(sources, list) or not [x for x in sources if str(x).strip()]:
            errors.append(f"{rel}: sources 없음. 판단의 근거(원문 줄 인용·결정 링크)가 필요하다")
        titles[str(fm.get("title"))].append(rel)

        for m in WIKILINK_RE.finditer(body):
            target = m.group(1).strip()
            hit = resolve(target)
            if hit is None:
                errors.append(f"{rel}: 없는 페이지로의 위키링크 `[[{target}]]`")
            else:
                inbound[hit] += 1
        for i, r in enumerate(relations(fm), 1):
            label = f"{rel}: related[{i}]"
            hit = resolve(str(r.get("target", "")))
            if hit is None or hit == "index":
                errors.append(f"{label} 없는 대상 `{r.get('target')}`")
            else:
                inbound[hit] += 1
            if "type" in r and r["type"] not in RELATION_TYPES:
                errors.append(f"{label} 알 수 없는 관계 `{r.get('type')}` ({'/'.join(sorted(RELATION_TYPES))})")
            if "evidence" in r and not CITATION_RE.fullmatch(str(r["evidence"])):
                errors.append(f"{label} evidence는 `^[/경로:줄-줄]` 형식")
            if r.get("type") == "contradicts":
                infos.append(f"{label} contradicts → {r.get('target')}")

        for path, a, b in cited_spans(text):
            f = resolve_resource(path)
            if f.resolve().is_relative_to(STATE_ROOT.resolve()):
                errors.append(f"{rel}: 작업 공간(검토 사본 등)을 정식 지식 근거로 인용함 `{path}`")
                continue
            if not f.exists():
                if is_session(path):
                    warnings.append(f"{rel}: 세션 원문 만료 `{path}`. 근거를 다시 확인할 수 없음")
                else:
                    errors.append(f"{rel}: 인용 파일 없음 `{path}`")
                continue
            if f not in line_counts:
                line_counts[f] = sum(1 for _ in f.open(encoding="utf-8", errors="replace"))
            n = line_counts[f]
            if a < 1 or b < a or b > n:
                errors.append(f"{rel}: 인용 줄 범위 오류 `{path}:{a}-{b}` (파일은 {n}줄)")
        for key, state in freshness(slug_of(p), text, registry).items():
            if state == "changed":
                warnings.append(f"{rel}: 원문 변경 `{key}`. 이 인용에 기댄 문장·관계를 재검토")
            elif state == "unrecorded":
                infos.append(f"{rel}: 해시 기록 없는 인용 `{key}`. 병합 후 registry.py record")

        if PLACEHOLDER_RE.search(body):
            warnings.append(f"{rel}: 플레이스홀더 문구 발견")
        sa = fm.get("stale_after")
        if isinstance(sa, str) and sa:
            try:
                dt = datetime.fromisoformat(sa.replace("Z", "+00:00"))
                if dt <= now:
                    warnings.append(f"{rel}: stale_after 경과 ({sa})")
            except ValueError:
                warnings.append(f"{rel}: stale_after 형식 오류 `{sa}`")
        if text.count("\n") > 200:
            infos.append(f"{rel}: {text.count(chr(10))}줄, 200줄 초과. 분할 검토")

    for p in pages:
        if inbound.get(slug_of(p), 0) == 0:
            warnings.append(f"{p.relative_to(WIKI_ROOT).as_posix()}: 고아 페이지 (들어오는 링크·관계 0)")
    for title, rels in titles.items():
        if len(rels) > 1:
            warnings.append(f"제목 중복 `{title}`: {', '.join(rels)}")

    idx = WIKI_ROOT / "index.md"
    if idx.exists():
        listed = {resolve(t) for t in WIKILINK_RE.findall(idx.read_text(encoding="utf-8-sig"))}
        missing = sorted({slug_of(p) for p in pages} - listed)
        if missing:
            warnings.append(f"index.md 에 없는 페이지 {len(missing)}개 (build_index.py 실행): {', '.join(missing[:5])}…")
    else:
        warnings.append("index.md 없음 (build_index.py 실행)")

    for label, items in (("ERROR", errors), ("WARNING", warnings), ("INFO", infos)):
        for it in items:
            print(f"[{label}] {it}")
    print(f"\n{len(pages)} pages · {len(errors)} errors · {len(warnings)} warnings · {len(infos)} infos")
    return 1 if errors else 0


if __name__ == "__main__":
    raise SystemExit(main())
