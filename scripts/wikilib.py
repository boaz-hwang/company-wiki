"""wiki 공용 유틸. 표준 라이브러리만 쓴다 (PyYAML 없음).

frontmatter 파서는 이 위키가 쓰는 부분집합만 지원한다:
- `key: value` 스칼라 (따옴표 있거나 없거나)
- `key: [a, b, c]` 인라인 리스트
- `key: { a: 1, b: 2 }` 인라인 매핑 (한 단계)
- `key:` 다음 줄부터 `  - item` 블록 리스트 (스칼라 또는 `- k: v` 매핑)
"""
from __future__ import annotations

import hashlib
import json
import re
import os
from pathlib import Path

from settings import WIKI_ROOT, REPO_ROOT, EXTRA_ROOTS, SOURCES, STATE_ROOT
TYPE_FOLDERS = {"concept": "concepts", "entity": "entities", "policy": "policies"}
PAGE_TYPES = set(TYPE_FOLDERS)
RELATION_TYPES = {"part_of", "depends_on", "implements", "applies_to", "supersedes", "contradicts", "related_to"}
# 대상 페이지가 바뀌면 관계를 건 페이지도 재검토해야 하는 관계. 예: policy가 concept에 depends_on.
IMPACT_TYPES = {"depends_on", "implements", "applies_to"}
# 검증 기록(인용 해시)은 지식이 아니지만 팀이 공유해야 하므로 위키 저장소의 숨김 경로에 둔다.
VERIFICATION_DIR = ".verification"
SESSION_KINDS = {"claude", "codex"}


def resolve_resource(path: str) -> Path:
    """`/저장소경로` 또는 `/원천별칭/파일` 을 실제 파일 경로로 바꾼다."""
    rel = path.lstrip("/")
    if ".." in Path(rel).parts:
        raise ValueError(f"Source path escapes its root: {path}")
    head, _, tail = rel.partition("/")
    prefix = os.environ.get("WIKI_RESOURCE_PREFIX")
    if head in EXTRA_ROOTS:
        root, relative = EXTRA_ROOTS[head], tail
    elif prefix and rel.startswith(prefix.rstrip("/") + "/"):
        root, relative = WIKI_ROOT, rel[len(prefix.rstrip("/")) + 1:]
    else:
        root, relative = REPO_ROOT, rel
    target = (root / relative).resolve()
    if not target.is_relative_to(root.resolve()):
        raise ValueError(f"Source path escapes its root: {path}")
    return target

SKIP_FILES = {"index.md"}

WIKILINK_RE = re.compile(r"\[\[([^\]|#]+)(?:#[^\]|]*)?(?:\|[^\]]*)?\]\]")
CITATION_RE = re.compile(r"\^\[(/[^\]:]+?):(\d+)(?:-(\d+))?\]")
FM_RE = re.compile(r"\A---\n(.*?)\n---\n?", re.S)
MAP_ITEM_RE = re.compile(r"[A-Za-z_][\w-]*:(?:\s|$)")


def _scalar(s: str):
    s = s.strip()
    if not s:
        return ""
    if (s[0] == s[-1]) and s[0] in "\"'" and len(s) >= 2:
        return s[1:-1]
    if s.lower() in ("true", "false"):
        return s.lower() == "true"
    if s == "[]":
        return []
    if s == "{}":
        return {}
    return s


def _split_top(s: str, sep: str = ","):
    out, depth, cur, quote = [], 0, "", None
    for ch in s:
        if quote:
            cur += ch
            if ch == quote:
                quote = None
            continue
        if ch in "\"'":
            quote = ch
            cur += ch
        elif ch in "[{":
            depth += 1
            cur += ch
        elif ch in "]}":
            depth -= 1
            cur += ch
        elif ch == sep and depth == 0:
            out.append(cur)
            cur = ""
        else:
            cur += ch
    if cur.strip():
        out.append(cur)
    return out


def _inline(s: str):
    s = s.strip()
    if s.startswith("[") and s.endswith("]"):
        inner = s[1:-1].strip()
        return [_inline(x) for x in _split_top(inner)] if inner else []
    if s.startswith("{") and s.endswith("}"):
        inner = s[1:-1].strip()
        d = {}
        for part in _split_top(inner):
            if ":" in part:
                k, v = part.split(":", 1)
                d[k.strip()] = _inline(v)
        return d
    return _scalar(s)


def parse_frontmatter(text: str) -> tuple[dict | None, str]:
    text = text.removeprefix("\ufeff").replace("\r\n", "\n")
    m = FM_RE.match(text)
    if not m:
        return None, text
    block, body = m.group(1), text[m.end():]
    lines = block.split("\n")
    data: dict = {}
    i = 0
    while i < len(lines):
        line = lines[i]
        if not line.strip() or line.lstrip().startswith("#"):
            i += 1
            continue
        if line.startswith(" "):
            i += 1
            continue
        if ":" not in line:
            i += 1
            continue
        key, rest = line.split(":", 1)
        key = key.strip()
        rest = rest.split(" #")[0] if not rest.strip().startswith(("\"", "'")) else rest
        rest = rest.strip()
        if rest:
            data[key] = _inline(rest)
            i += 1
            continue
        # block list or nested mapping
        items = []
        j = i + 1
        cur_map = None
        while j < len(lines) and (lines[j].startswith("  ") or not lines[j].strip()):
            l = lines[j]
            if not l.strip():
                j += 1
                continue
            stripped = l.strip()
            if stripped.startswith("- "):
                content = stripped[2:].strip()
                # `- key: value` starts a mapping; citations and URLs (`^[/a.md:1]`, `https://`) stay scalars.
                if MAP_ITEM_RE.match(content):
                    k, v = content.split(":", 1)
                    cur_map = {k.strip(): _inline(v)}
                    items.append(cur_map)
                else:
                    cur_map = None
                    items.append(_inline(content))
            elif cur_map is not None and ":" in stripped:
                k, v = stripped.split(":", 1)
                cur_map[k.strip()] = _inline(v)
            j += 1
        data[key] = items
        i = j
    return data, body


def dump_frontmatter(data: dict) -> str:
    def q(v):
        if isinstance(v, bool):
            return "true" if v else "false"
        if isinstance(v, (int, float)):
            return str(v)
        v = str(v)
        if v == "" or re.search(r"[:#\[\]{}\"'\n]|^\s|\s$|^[-?&*!|>%@`]", v) or v.lower() in ("true", "false", "null", "yes", "no"):
            return '"' + v.replace("\\", "\\\\").replace('"', '\\"') + '"'
        return v

    out = ["---"]
    for k, v in data.items():
        if isinstance(v, list):
            if not v:
                out.append(f"{k}: []")
            elif any(isinstance(x, dict) for x in v):
                # related mixes slugs and typed relations; both fit a block list.
                out.append(f"{k}:")
                for item in v:
                    if not isinstance(item, dict):
                        out.append(f"  - {q(item)}")
                        continue
                    first = True
                    for ik, iv in item.items():
                        prefix = "  - " if first else "    "
                        out.append(f"{prefix}{ik}: {q(iv)}")
                        first = False
            else:
                out.append(f"{k}: [" + ", ".join(q(x) for x in v) + "]")
        elif isinstance(v, dict):
            out.append(f"{k}: {{ " + ", ".join(f"{ik}: {q(iv)}" for ik, iv in v.items()) + " }")
        else:
            out.append(f"{k}: {q(v)}")
    out.append("---")
    return "\n".join(out) + "\n"


def iter_pages():
    for p in sorted(WIKI_ROOT.rglob("*.md")):
        parts = p.relative_to(WIKI_ROOT).parts
        if p.name in SKIP_FILES or parts[0] not in TYPE_FOLDERS.values():
            continue
        yield p


def slug_of(p: Path) -> str:
    return p.relative_to(WIKI_ROOT).with_suffix("").as_posix()


def build_resolver(pages):
    """Obsidian 과 같은 순서로 위키링크를 해석한다.
    1) wiki 루트 기준 정확한 경로  2) `wiki/` 접두 제거 후 경로  3) 고유한 basename  4) 고유한 경로 suffix.
    성공하면 slug, 실패하면 None 을 돌려준다."""
    slugs = {slug_of(p) for p in pages} | {"index", "SCHEMA"}
    by_base: dict[str, list[str]] = {}
    for s in slugs:
        by_base.setdefault(s.rsplit("/", 1)[-1], []).append(s)

    def resolve(target: str):
        t = target.strip()
        if t.endswith(".md"):
            t = t[:-3]
        if t in slugs:
            return t
        if t.startswith("wiki/") and t[5:] in slugs:
            return t[5:]
        if "/" not in t:
            c = by_base.get(t, [])
            return c[0] if len(c) == 1 else None
        c = [s for s in slugs if s.endswith("/" + t) or s == t]
        return c[0] if len(c) == 1 else None

    return resolve


def is_session(path: str) -> bool:
    """세션 원천은 보존 기간이 지나면 정리될 수 있다."""
    head = path.lstrip("/").partition("/")[0]
    if head not in EXTRA_ROOTS:
        return False
    return SOURCES.get(head, {}).get("kind") in SESSION_KINDS or head in {"claude-sessions", "codex-sessions"}


def relations(fm: dict) -> list[dict]:
    """related 항목. `concepts/x` 같은 슬러그, 또는 {target, type, evidence} 타입 관계를 모두 받는다."""
    out = []
    for r in fm.get("related") or []:
        if isinstance(r, str) and r.strip():
            out.append({"target": r.strip()})
        elif isinstance(r, dict):
            out.append(r)
    return out


def span_key(path: str, a: int, b: int) -> str:
    return f"{path}:{a}-{b}"


def cited_spans(text: str) -> list[tuple[str, int, int]]:
    """본문과 frontmatter(관계 근거)의 줄 인용을 모두 모은다."""
    out = []
    for m in CITATION_RE.finditer(text):
        a = int(m.group(2))
        out.append((m.group(1), a, int(m.group(3) or a)))
    return out


def _source_lines(path: str) -> list[bytes] | None:
    try:
        f = resolve_resource(path)
        if not f.is_file():
            return None
        return f.read_bytes().splitlines(keepends=True)
    except (OSError, ValueError):
        return None


def span_hash(path: str, a: int, b: int) -> str | None:
    """인용한 줄의 내용 해시. 파일이 없거나 범위를 벗어나면 None."""
    lines = _source_lines(path)
    if lines is None or not 1 <= a <= b <= len(lines):
        return None
    return hashlib.sha256(b"".join(lines[a - 1:b])).hexdigest()


def relocate(path: str, a: int, b: int, recorded: str) -> tuple[int, int] | None:
    """기록한 내용이 같은 길이로 다른 줄에 있으면 그 범위. 여러 곳이면 원래 위치에 가장 가까운 곳."""
    lines = _source_lines(path)
    if lines is None:
        return None
    size = b - a + 1
    hits = [i for i in range(1, len(lines) - size + 2)
            if i != a and hashlib.sha256(b"".join(lines[i - 1:i - 1 + size])).hexdigest() == recorded]
    if not hits:
        return None
    i = min(hits, key=lambda x: abs(x - a))
    return i, i + size - 1


def page_spans(text: str) -> dict[str, str | None]:
    return {span_key(p, a, b): span_hash(p, a, b) for p, a, b in cited_spans(text)}


def registry_path(wiki: Path | None = None) -> Path:
    return (wiki or WIKI_ROOT) / VERIFICATION_DIR / "registry.json"


def load_registry(wiki: Path | None = None, state: Path | None = None) -> dict:
    # preview는 반영 후 레지스트리를 임시 파일로 넘겨 검사한다.
    override = os.environ.get("WIKI_REGISTRY")
    if override and wiki is None:
        p = Path(override)
    else:
        p = registry_path(wiki)
        legacy = (state or STATE_ROOT) / "registry.json"  # 이전 버전은 작업 공간에 기록했다
        if not p.exists() and legacy.exists():
            p = legacy
    if not p.exists():
        return {"version": 1, "pages": {}}
    return json.loads(p.read_text(encoding="utf-8-sig"))


def freshness(slug: str, text: str, registry: dict) -> dict[str, str]:
    """인용별 상태: fresh / moved / changed / expired / missing / unrecorded."""
    recorded = registry.get("pages", {}).get(slug, {}).get("citations", {})
    out = {}
    for key, current in page_spans(text).items():
        path, lines = key.rsplit(":", 1)
        a, b = (int(x) for x in lines.split("-"))
        if key in recorded and current != recorded[key] and relocate(path, a, b, recorded[key]):
            out[key] = "moved"
        elif current is None:
            out[key] = "expired" if is_session(path) else "missing"
        elif key not in recorded:
            out[key] = "unrecorded"
        else:
            out[key] = "fresh" if recorded[key] == current else "changed"
    return out


def moved_to(key: str, slug: str, registry: dict) -> str | None:
    """moved 인용의 새 위치 `경로:줄-줄`."""
    recorded = registry.get("pages", {}).get(slug, {}).get("citations", {}).get(key)
    path, lines = key.rsplit(":", 1)
    a, b = (int(x) for x in lines.split("-"))
    hit = recorded and relocate(path, a, b, recorded)
    return span_key(path, *hit) if hit else None


def dependents(pages: dict[str, dict], roots: set[str]) -> dict[str, tuple[str, str]]:
    """roots가 바뀌었을 때 IMPACT_TYPES 관계를 따라 영향받는 페이지. 페이지 → (관계, 기대는 페이지)."""
    resolve = build_resolver([WIKI_ROOT / f"{slug}.md" for slug in pages])
    incoming: dict[str, list[tuple[str, str]]] = {}
    for slug, fm in pages.items():
        for r in relations(fm):
            target = resolve(str(r.get("target", "")))
            if target and r.get("type") in IMPACT_TYPES:
                incoming.setdefault(target, []).append((slug, r["type"]))
    out: dict[str, tuple[str, str]] = {}
    queue = list(roots)
    while queue:
        page = queue.pop(0)
        for source, kind in incoming.get(page, []):
            if source not in roots and source not in out:
                out[source] = (kind, page)
                queue.append(source)
    return out
