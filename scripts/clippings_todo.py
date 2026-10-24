#!/usr/bin/env python3
"""아직 위키에 정리하지 않은 Clippings 목록 (오래된 것부터).

처리됨 = 정식 페이지가 `/Clippings/<파일>:줄` 을 인용한다.
건너뜀 = 상태 폴더의 excluded.jsonl 에 사용자 제외 결정 `{"path": "<Clippings 기준 상대 경로>", ...}` 이 있다.
그 외는 미처리. 원본 클리핑 파일은 절대 수정하지 않는다.

실행: python3 scripts/clippings_todo.py [--all]
"""
from __future__ import annotations

import json
import os
import sys
from datetime import datetime
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from wikilib import EXTRA_ROOTS, STATE_ROOT, cited_spans, iter_pages, parse_frontmatter  # noqa: E402

CLIP_DIR = EXTRA_ROOTS.get("Clippings")


def processed() -> set[str]:
    done = set()
    for p in iter_pages():
        for path, _, _ in cited_spans(p.read_text(encoding="utf-8-sig")):
            if path.startswith("/Clippings/"):
                done.add(path[len("/Clippings/"):])
    return done


def skipped() -> set[str]:
    p = STATE_ROOT / "excluded.jsonl"
    if not p.exists():
        return set()
    rows = [json.loads(x) for x in p.read_text(encoding="utf-8-sig").splitlines() if x.strip()]
    return {str(r["path"]) for r in rows if r.get("path")}


def pending() -> list[Path]:
    if CLIP_DIR is None:
        raise FileNotFoundError("Configure sources.Clippings in wiki.toml first")
    if not CLIP_DIR.is_dir():
        raise FileNotFoundError(f"Clippings 디렉터리 없음: {CLIP_DIR}")
    def fail(error):
        raise error
    # Path.rglob can suppress traversal errors. Incomplete enumeration is not zero work.
    candidates = []
    for directory, _, names in os.walk(CLIP_DIR, onerror=fail):
        for name in names:
            if name.endswith('.md'):
                p = Path(directory) / name
                with p.open('rb') as stream:
                    stream.read(1)
                candidates.append(p)
    done, skip = processed(), skipped()
    files = [p for p in candidates if p.relative_to(CLIP_DIR).as_posix() not in done | skip]
    return sorted(files, key=lambda p: p.stat().st_mtime)


def main() -> int:
    show_all = "--all" in sys.argv
    try:
        todo = pending()
    except OSError as error:
        print(f"[ERROR] Clippings 조사 불가: {error}", file=sys.stderr)
        return 2
    print(f"Clippings: {CLIP_DIR}")
    print(f"미처리 {len(todo)}건 · 처리 {len(processed())}건 · 건너뜀 {len(skipped())}건")
    for p in todo if show_all else todo[:20]:
        fm, _ = parse_frontmatter(p.read_text(encoding="utf-8", errors="replace"))
        fm = fm or {}
        title = str(fm.get("title") or p.stem)
        src = str(fm.get("source") or "")
        saved = datetime.fromtimestamp(p.stat().st_mtime).strftime("%Y-%m-%d")
        words = len(p.read_text(encoding="utf-8", errors="replace").split())
        print(f"- [{saved}] {title}  ({words}w)  {src}\n    /Clippings/{p.relative_to(CLIP_DIR).as_posix()}")
    if not show_all and len(todo) > 20:
        print(f"... 외 {len(todo) - 20}건 (--all)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
