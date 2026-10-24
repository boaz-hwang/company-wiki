#!/usr/bin/env python3
"""원천 레지스트리. 인용별 최신성과 원문 → 의존 페이지 역조회를 보여준다.

레지스트리(작업 공간의 registry.json)는 페이지별로 채택 시점의 인용 줄 해시를 기록한다. 원문 사본은 두지 않는다.
review apply 가 자동 기록하고, Git PR로 병합한 변경은 record 로 기록한다.

  python3 scripts/registry.py [--source /별칭/파일.md] [--all]   조회 (읽기 전용)
  python3 scripts/registry.py record                            새 인용만 기록. 바뀐 원문의 기존 해시는 유지
  python3 scripts/registry.py record concepts/x ...             지정 페이지를 재검토했을 때 현재 원문으로 갱신

상태: fresh(승인 때와 같음) · changed(원문 변경, 재검토) · expired(세션 원문 정리됨)
      missing(원문 없음) · unrecorded(review 없이 추가된 인용)
"""
from __future__ import annotations

import argparse
import json
import os
import sys
import tempfile
from collections import defaultdict
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from wikilib import STATE_ROOT, freshness, iter_pages, load_registry, page_spans, slug_of  # noqa: E402


def report() -> dict[str, dict[str, list[tuple[str, str]]]]:
    """원문 경로 → 상태 → [(페이지, 인용 범위)]"""
    registry = load_registry()
    out: dict[str, dict[str, list[tuple[str, str]]]] = defaultdict(lambda: defaultdict(list))
    for p in iter_pages():
        slug = slug_of(p)
        for key, state in freshness(slug, p.read_text(encoding="utf-8-sig"), registry).items():
            path, lines = key.rsplit(":", 1)
            out[path][state].append((slug, lines))
    return out


def record(selected: list[str]) -> list[str]:
    """선택이 없으면 기록 없는 인용만 추가한다. 이미 기록한 해시를 덮어쓰면 원문 변경 신호가 사라지기 때문이다."""
    registry = load_registry()
    recorded = registry.setdefault("pages", {})
    at = datetime.now(timezone.utc).isoformat(timespec="seconds")
    slugs = {slug_of(p): p for p in iter_pages()}
    unknown = sorted(set(selected) - set(slugs))
    if unknown:
        raise ValueError(f"없는 페이지: {', '.join(unknown)}")
    changed = []
    for slug, p in slugs.items():
        if selected and slug not in selected:
            continue
        current = {k: v for k, v in page_spans(p.read_text(encoding="utf-8-sig")).items() if v}
        old = recorded.get(slug, {}).get("citations", {})
        new = current if selected else {k: old.get(k, v) for k, v in current.items()}
        if new != old or slug not in recorded:
            recorded[slug] = {"revision": None, "recorded_at": at, "citations": new}
            changed.append(slug)
    registry["pages"] = dict(sorted(recorded.items()))
    STATE_ROOT.mkdir(parents=True, exist_ok=True)
    fd, tmp = tempfile.mkstemp(prefix=".registry-", dir=STATE_ROOT)
    with os.fdopen(fd, "w", encoding="utf-8") as stream:
        stream.write(json.dumps(registry, ensure_ascii=False, indent=2) + "\n")
    os.replace(tmp, STATE_ROOT / "registry.json")
    return changed


def main(argv=None) -> int:
    argv = sys.argv[1:] if argv is None else argv
    if argv[:1] == ["record"]:
        try:
            changed = record(argv[1:])
        except ValueError as error:
            print(f"ERROR: {error}", file=sys.stderr)
            return 1
        print(f"기록한 페이지 {len(changed)}개" + (f": {', '.join(changed)}" if changed else ""))
        return 0
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--source", help="이 원문에 기대는 페이지만 보기")
    parser.add_argument("--all", action="store_true", help="fresh 인용도 보기")
    parser.add_argument("--json", action="store_true")
    args = parser.parse_args(argv)
    data = report()
    if args.source:
        data = {k: v for k, v in data.items() if k == args.source}
    if args.json:
        print(json.dumps(data, ensure_ascii=False, indent=2))
        return 0
    attention = 0
    for path in sorted(data):
        states = data[path]
        if not args.all and set(states) == {"fresh"}:
            continue
        print(path)
        attention += any(state != "fresh" for state in states)
        for state in sorted(states):
            if state == "fresh" and not args.all:
                continue
            for slug, lines in sorted(states[state]):
                print(f"  {state:10} {slug} (줄 {lines})")
    total = sum(len(v) for s in data.values() for v in s.values())
    print(f"\n원문 {len(data)}개 · 인용 {total}개 · 확인 필요 원문 {attention}건")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
