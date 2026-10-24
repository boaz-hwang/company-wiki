#!/usr/bin/env python3
"""Versioned wiki proposals, human decisions and checked publication (stdlib only).

Optional operations tool. The default approval path is a Git pull request approved by the page owner;
this tool records session-approved changes in the workspace (wiki.state), never inside the wiki."""
from __future__ import annotations

import argparse
import difflib
from platform_io import file_lock
import hashlib
import json
import os
import re
import subprocess
import sys
import tempfile
from contextlib import contextmanager
from datetime import datetime, timezone
from pathlib import Path

from dump_session import messages as session_messages
from settings import REVIEWER, CONFIG_PATH
from wikilib import (CITATION_RE, EXTRA_ROOTS, PAGE_TYPES, REPO_ROOT, STATE_ROOT, WIKI_ROOT,
                     VERIFICATION_DIR, WIKILINK_RE, cited_spans, load_registry, parse_frontmatter, relations,
                     span_key)

KINDS = {"fact", "interpretation", "naming", "relationship", "confidence", "scope"}
PAGE = re.compile(r"(?:concepts|entities|policies)/(?:[a-z0-9][a-z0-9-]*/)*[a-z0-9][a-z0-9-]*\.md\Z")
ID = re.compile(r"[a-z0-9][a-z0-9-]*\Z")
SCRIPTS = Path(__file__).resolve().parent


def now():
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


def digest(value):
    if value is None:
        return None
    if not isinstance(value, bytes):
        value = json.dumps(value, ensure_ascii=False, sort_keys=True).encode()
    return hashlib.sha256(value).hexdigest()


def read_bytes(path):
    return path.read_bytes() if path.exists() else None


def atomic(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    if data is None:
        path.unlink(missing_ok=True)
        return
    fd, temp = tempfile.mkstemp(prefix=".review-", dir=path.parent)
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temp, path)
    finally:
        Path(temp).unlink(missing_ok=True)


def json_bytes(value):
    return (json.dumps(value, ensure_ascii=False, indent=2) + "\n").encode()


def safe(root, rel):
    rel = Path(rel)
    if rel.is_absolute() or ".." in rel.parts:
        raise ValueError(f"허용되지 않은 경로: {rel}")
    path = root / rel
    for part in [path, *path.parents]:
        if part == root:
            break
        if part.is_symlink():
            raise ValueError(f"심링크 경로는 변경하지 않음: {path}")
    if not path.resolve().is_relative_to(root.resolve()):
        raise ValueError(f"루트 밖 경로: {path}")
    return path


class Review:
    def __init__(self, wiki=WIKI_ROOT, repo=REPO_ROOT, state=STATE_ROOT):
        self.wiki, self.repo, self.state = Path(wiki).resolve(), Path(repo).resolve(), Path(state).resolve()
        if self.state == self.wiki or self.state.is_relative_to(self.wiki) or self.wiki.is_relative_to(self.state):
            raise ValueError("상태 폴더(wiki.state)는 위키 폴더와 분리한다")
        self.root = self.state / "reviews"
        self.journal = self.root / "apply-journal.json"

    def batch(self, batch):
        if not ID.fullmatch(batch):
            raise ValueError("batch는 영문 소문자·숫자·하이픈")
        return safe(self.root, batch)

    def topic(self, batch, topic):
        if not ID.fullmatch(topic):
            raise ValueError("topic은 영문 소문자·숫자·하이픈")
        return safe(self.batch(batch), "topics/" + topic)

    @contextmanager
    def lock(self):
        self.root.mkdir(parents=True, exist_ok=True)
        with file_lock(self.root / ".lock"):
            yield

    def events(self, batch, name):
        p = self.batch(batch) / name
        return [json.loads(x) for x in p.read_text(encoding='utf-8-sig').splitlines()] if p.exists() else []

    def event(self, batch, name, value):
        p = self.batch(batch) / name
        atomic(p, (read_bytes(p) or b"") + json.dumps(value, ensure_ascii=False).encode() + b"\n")

    def current(self, batch, topic):
        revisions = sorted(self.topic(batch, topic).glob("r[0-9]*"))
        if not revisions:
            raise ValueError(f"검토안 없음: {topic}")
        p = revisions[-1]
        m = json.loads((p / "manifest.json").read_text(encoding='utf-8-sig'))
        card = json.loads((p / "card.json").read_text(encoding='utf-8-sig'))
        fingerprint = {"card": card, "files": m["files"], "dependencies": m["dependencies"]}
        if digest(fingerprint) != m["revision"]:
            raise ValueError(f"봉인된 검토안 변경: {topic}. prepare로 새 버전을 만드세요.")
        if any(x.is_file() and not x.name.endswith(".md.txt") for x in (p / "after").rglob("*")):
            raise ValueError(f"초안 사본은 .md.txt만 허용: {topic}")
        actual = {x.relative_to(p / "after").as_posix().removesuffix(".txt") for x in (p / "after").rglob("*") if x.is_file()}
        if actual != set(m["files"]):
            raise ValueError(f"선언 밖 초안 파일: {topic}")
        for rel, hashes in m["files"].items():
            if not PAGE.fullmatch(rel):
                raise ValueError(f"페이지 경로 오류: {rel}")
            if digest(read_bytes(safe(p / "after", rel + ".txt"))) != hashes["after"]:
                raise ValueError(f"승인 버전과 다른 초안: {topic}/{rel}")
            if digest(read_bytes(safe(p / "before", rel + ".txt"))) != hashes["before"]:
                raise ValueError(f"변경 전 사본 변조: {topic}/{rel}")
        payloads = {rel: (read_bytes(safe(p / "before", rel + ".txt")), safe(p / "after", rel + ".txt").read_bytes())
                    for rel in m["files"]}
        if (p / "review.md").read_bytes().decode("utf-8") != self.render(topic, m, card, payloads):
            raise ValueError(f"표시된 검토 문서 변경: {topic}")
        return p, m, card

    def resource(self, name):
        head, _, tail = name.lstrip("/").partition("/")
        return safe(EXTRA_ROOTS[head], tail) if head in EXTRA_ROOTS else safe(self.repo, name.lstrip("/"))

    def dependency(self, ref):
        match = CITATION_RE.fullmatch(ref)
        if match:
            name, first, last = match.groups()
            path = self.resource(name)
            lines = path.read_bytes().splitlines(keepends=True)
            a, b = int(first), int(last or first)
            if not 1 <= a <= b <= len(lines):
                raise ValueError(f"근거 범위 오류: {ref}")
            # Session files keep growing. Bind the cited records, not later appends.
            return digest(b"".join(lines[a-1:b]))
        if not ref.startswith("/"):
            raise ValueError(f"의존 문서는 /경로 또는 줄 인용으로 지정: {ref}")
        p = self.resource(ref)
        if not p.is_file():
            raise ValueError(f"근거 파일 없음: {ref}")
        if p.suffix == ".jsonl":
            raise ValueError(f"세션 근거는 사용자/근거 레코드의 줄 인용 필요: {ref}")
        return digest(p.read_bytes())

    def pages(self):
        return {p.relative_to(self.wiki).as_posix(): p.read_bytes()
                for p in self.wiki.rglob("*.md")
                if PAGE.fullmatch(p.relative_to(self.wiki).as_posix())}

    def prepare(self, batch, topic, card, draft):
        with self.lock():
            if self.journal.exists():
                raise ValueError("중단된 반영이 있습니다. recover를 먼저 실행하세요.")
            for key in ("title", "summary", "judgments", "files"):
                if not card.get(key):
                    raise ValueError(f"검토 카드 필수 항목: {key}")
            for item in card["judgments"]:
                if item.get("kind") not in KINDS:
                    raise ValueError("판단 유형은 fact/interpretation/naming/relationship/confidence/scope")
                for key in ("before", "proposed", "reason", "evidence"):
                    if key not in item:
                        raise ValueError(f"판단에 {key} 필요")
            names = card["files"]
            if len(set(names)) != len(names) or not all(PAGE.fullmatch(x) for x in names):
                raise ValueError("files에는 중복 없는 정식 페이지 상대경로만 허용")
            if any(p.is_file() and not p.name.endswith(".md.txt") for p in draft.rglob("*")):
                raise ValueError("draft 파일은 Obsidian 링크 충돌 방지를 위해 .md.txt로 저장")
            if set(names) != {p.relative_to(draft).as_posix().removesuffix(".txt") for p in draft.rglob("*") if p.is_file()}:
                raise ValueError("draft-dir 파일 목록과 card.files가 다름")
            for other in (self.batch(batch) / "topics").glob("*"):
                if other.name == topic:
                    continue
                _, om, _ = self.current(batch, other.name)
                if set(names) & set(om["files"]) and not self.applied(batch, other.name, om["revision"]):
                    raise ValueError(f"같은 페이지를 수정하는 {other.name}과 한 승인 묶음으로 합치세요")
            files, payloads, deps = {}, {}, set(card.get("dependencies", []))
            for item in card["judgments"]:
                deps.update(item["evidence"])
            live = self.pages()
            for rel in names:
                old = read_bytes(safe(self.wiki, rel))
                new = safe(draft, rel + ".txt").read_bytes()
                if old == new:
                    raise ValueError(f"변경 없는 파일은 제외: {rel}")
                fm, text = parse_frontmatter(new.decode())
                if not fm or fm.get("type") not in PAGE_TYPES or not all(fm.get(k) for k in ("title", "description", "owner")):
                    raise ValueError(f"frontmatter 오류(type·title·description·owner): {rel}")
                files[rel] = {"before": digest(old), "after": digest(new)}
                payloads[rel] = (old, new)
                # Relation evidence lives in frontmatter, so read citations from the whole page.
                deps.update(m.group(0) for m in CITATION_RE.finditer(new.decode()))
                # Include existing linked context even when it has no inline citation.
                for link in self.links(fm, text):
                    target = self.resolve_link(link, live)
                    if target and target not in names:
                        deps.add("/" + (self.wiki / target).relative_to(self.repo).as_posix())
            dependencies = {}
            for ref in sorted(deps):
                cite = CITATION_RE.fullmatch(ref)
                resource = self.resource(cite[1] if cite else ref)
                local = resource.relative_to(self.wiki).as_posix() if resource.is_relative_to(self.wiki) else None
                if local in payloads:
                    # A jointly proposed page is already bound by its after hash.
                    if cite:
                        a, b = int(cite[2]), int(cite[3] or cite[2])
                        if not 1 <= a <= b <= len(payloads[local][1].splitlines()):
                            raise ValueError(f"초안 근거 범위 오류: {ref}")
                else:
                    dependencies[ref] = self.dependency(ref)
            fingerprint = {"card": card, "files": files, "dependencies": dependencies}
            m = {**fingerprint, "revision": digest(fingerprint), "created": now()}
            m.pop("card")
            parent = self.topic(batch, topic)
            parent.mkdir(parents=True, exist_ok=True)
            target = parent / f"r{len(list(parent.glob('r[0-9]*'))) + 1:04d}"
            with tempfile.TemporaryDirectory(prefix=".prepare-", dir=parent) as tmp:
                folder = Path(tmp) / "revision"
                folder.mkdir()
                for rel, (old, new) in payloads.items():
                    if old is not None:
                        atomic(safe(folder / "before", rel + ".txt"), old)
                    atomic(safe(folder / "after", rel + ".txt"), new)
                atomic(folder / "manifest.json", json_bytes(m))
                atomic(folder / "card.json", json_bytes(card))
                atomic(folder / "review.md", self.render(topic, m, card, payloads).encode())
                os.replace(folder, target)
            return {"topic": topic, "revision": m["revision"], "review": str(target / "review.md")}

    @staticmethod
    def render(topic, m, card, payloads):
        out = [f"# {card['title']}", "", card["summary"], "", f"검토 버전: `{m['revision']}`", "",
               "승인 / 수정 요청 / 보류 / 제외 중 선택. 승인 범위는 아래 변경 전후와 판단 내용입니다.", ""]
        for i, judgment in enumerate(card["judgments"], 1):
            out += [f"## {i}. {judgment['kind']}", "", f"- 이전: {judgment['before']}",
                    f"- 제안: {judgment['proposed']}", f"- 이유: {judgment['reason']}",
                    "- 근거: " + (" · ".join(judgment["evidence"]) or "추가 확인 필요"), ""]
        if card.get("link_constraints"):
            out += ["## 적용 범위 검사", "", "```json", json.dumps(card["link_constraints"], ensure_ascii=False, indent=2), "```", ""]
        for rel, (old, new) in payloads.items():
            diff = "".join(difflib.unified_diff((old or b"").decode().splitlines(True), new.decode().splitlines(True), fromfile="before/"+rel, tofile="after/"+rel))
            out += [f"## {rel}", "", "```diff", diff.rstrip(), "```", ""]
        return "\n".join(out)

    def decision(self, batch, topic, revision):
        return next((x for x in reversed(self.events(batch, "decisions.jsonl"))
                     if x["topic"] == topic and x["revision"] == revision), None)

    def applied(self, batch, topic, revision):
        return any(x["topic"] == topic and x["revision"] == revision for x in self.events(batch, "applied.jsonl"))

    def user_evidence(self, response):
        ref, quote = response["evidence"], response["quote"]
        m = CITATION_RE.fullmatch(ref)
        if not m or not m[1].startswith(("/codex-sessions/", "/claude-sessions/")) or not quote.strip():
            raise ValueError("실제 사용자 세션 줄 인용과 원문 quote 필요")
        lines = self.resource(m[1]).read_text(encoding='utf-8-sig').splitlines()
        messages = []
        for line in lines[int(m[2])-1:int(m[3] or m[2])]:
            row = json.loads(line)
            messages.extend(text for role, text in session_messages(row) if role == "user")
            if row.get("type") == "event_msg" and row.get("payload", {}).get("type") == "user_message":
                messages.append(row["payload"].get("message", ""))
        if quote not in "\n".join(messages):
            raise ValueError("quote가 인용된 사용자 메시지에 없음 (모델/도구 응답은 승인 근거 아님)")
        return self.dependency(ref)

    def decide(self, batch, topic, revision, decision, response):
        with self.lock():
            _, m, _ = self.current(batch, topic)
            if revision != m["revision"]:
                raise ValueError("이전 검토 버전의 응답입니다. 현재 버전을 확인하세요.")
            if self.applied(batch, topic, revision):
                raise ValueError("이미 반영된 판단을 바꾸려면 새 검토안을 만드세요")
            if decision not in {"approved", "changes_requested", "deferred", "rejected"}:
                raise ValueError("지원하지 않는 판단")
            if response.get("by") != REVIEWER:
                raise ValueError(f"이 위키의 판단 주체는 {REVIEWER}")
            for item in response.get("judgments", []):
                if item.get("kind") not in KINDS or not item.get("value"):
                    raise ValueError("응답 판단에는 kind와 value 필요")
            if decision == "approved" and self.stale(m):
                raise ValueError("기준이 바뀐 검토안은 승인할 수 없습니다. prepare로 갱신하세요")
            event = {"topic": topic, "revision": revision, "decision": decision, "at": now(),
                     "response": response, "evidence_hash": self.user_evidence(response)}
            self.event(batch, "decisions.jsonl", event)
            return event

    def stale(self, m):
        changed = [rel for rel, hashes in m["files"].items()
                   if digest(read_bytes(safe(self.wiki, rel))) != hashes["before"]]
        for ref, sha in m["dependencies"].items():
            try:
                if self.dependency(ref) != sha:
                    changed.append(ref)
            except (OSError, ValueError):
                changed.append(ref)
        return changed

    @staticmethod
    def links(fm, text):
        return set(WIKILINK_RE.findall(text)) | {str(r.get("target", "")) for r in relations(fm or {})}

    @staticmethod
    def resolve_link(link, pages):
        slug = link.strip().removesuffix(".md").removeprefix("wiki/")
        if slug + ".md" in pages:
            return slug + ".md"
        hits = [p for p in pages if p.removesuffix(".md").endswith("/"+slug)]
        return hits[0] if len(hits) == 1 else None

    def check_constraints(self, cards, pages):
        edges = {}
        for path, data in pages.items():
            fm, text = parse_frontmatter(data.decode())
            edges[path] = {self.resolve_link(x, pages) for x in self.links(fm, text)} - {None}
        for card in cards:
            for rule in card.get("link_constraints", []):
                if (set(rule) - {"target", "inbound_from", "outbound_to"}
                        or not ({"inbound_from", "outbound_to"} & set(rule))
                        or not PAGE.fullmatch(rule.get("target", "") + ".md")):
                    raise ValueError("link_constraints에는 정식 target과 inbound_from/outbound_to 필요")
                for direction in ("inbound_from", "outbound_to"):
                    if direction in rule and (not isinstance(rule[direction], list)
                            or not all(isinstance(x, str) and PAGE.fullmatch(x + ".md") for x in rule[direction])):
                        raise ValueError("link_constraints 허용 목록은 정식 페이지 슬러그 배열")
                target = rule["target"] + ".md"
                if target not in pages:
                    raise ValueError(f"범위 검사 대상 없음: {target}")
                for name in ("inbound_from", "outbound_to"):
                    if name not in rule:
                        continue
                    allowed = {x+".md" for x in rule[name]}
                    actual = edges[target] if name == "outbound_to" else {p for p, links in edges.items() if target in links}
                    if actual - allowed:
                        raise ValueError(f"승인 범위 밖 연결: {target} {name}: {sorted(actual-allowed)}")

    def preview(self, batch, topics, require_approval=False):
        pages, selected, cards = self.pages(), [], []
        writes = {}
        for topic in topics:
            path, m, card = self.current(batch, topic)
            if require_approval and self.applied(batch, topic, m["revision"]):
                continue
            if stale := self.stale(m):
                raise ValueError(f"{topic}의 기준 변경. 이 항목만 다시 prepare/검토: {stale}")
            if require_approval:
                decision = self.decision(batch, topic, m["revision"])
                if not decision or decision["decision"] != "approved":
                    raise ValueError(f"사람 승인 없음: {topic}")
                if self.user_evidence(decision["response"]) != decision["evidence_hash"]:
                    raise ValueError(f"승인 원문 변경: {topic}")
            for rel in m["files"]:
                if rel in writes:
                    raise ValueError(f"같은 파일 중복 수정: {rel}")
                writes[rel] = safe(path / "after", rel + ".txt").read_bytes()
            cards.append(card)
            selected.append((topic, m))
        pages.update(writes)
        self.check_constraints(cards, pages)
        registry = self.registry_after(writes, {rel: m["revision"] for _, m in selected for rel in m["files"]})
        with tempfile.TemporaryDirectory(prefix="wiki-review-preview-") as tmp:
            root = Path(tmp) / "wiki"
            for rel, data in pages.items():
                atomic(root / rel, data)
            atomic(Path(tmp) / "registry.json", registry)
            env = {**os.environ, **({"WIKI_CONFIG": str(CONFIG_PATH)} if CONFIG_PATH.exists() else {}), "WIKI_ROOT": str(root), "WIKI_STATE": str(self.state), "WIKI_REGISTRY": str(Path(tmp) / "registry.json"), "WIKI_REPO_ROOT": str(self.repo), "WIKI_RESOURCE_PREFIX": self.wiki.relative_to(self.repo).as_posix(), "PYTHONDONTWRITEBYTECODE": "1"}
            output = []
            for script in ("build_index.py", "lint.py"):
                result = subprocess.run([sys.executable, "-B", str(SCRIPTS / script)], env=env, capture_output=True, text=True, encoding='utf-8')
                output.append(result.stdout + result.stderr)
                if result.returncode:
                    raise ValueError("검토안 검사 실패:\n" + "\n".join(output))
            index = (root / "index.md").read_bytes()
        return writes, selected, index, "\n".join(output), registry

    def registry_after(self, writes, revisions):
        """반영할 페이지의 인용 줄 해시를 승인 시점 기준으로 기록한다. 다른 페이지 기록은 그대로 둔다."""
        registry = load_registry(self.wiki, self.state)
        recorded = registry.setdefault("pages", {})
        for rel, data in writes.items():
            citations = {}
            for path, a, b in cited_spans(data.decode()):
                resource = self.resource(path)
                local = resource.relative_to(self.wiki).as_posix() if resource.is_relative_to(self.wiki) else None
                try:
                    if local in writes:
                        lines = writes[local].splitlines(keepends=True)
                        if not 1 <= a <= b <= len(lines):
                            continue
                        citations[span_key(path, a, b)] = digest(b"".join(lines[a-1:b]))
                    else:
                        citations[span_key(path, a, b)] = self.dependency(f"^[{path}:{a}-{b}]")
                except (OSError, ValueError):
                    continue  # lint reports the broken citation
            recorded[rel.removesuffix(".md")] = {"revision": revisions.get(rel), "recorded_at": now(), "citations": citations}
        registry["pages"] = dict(sorted(recorded.items()))
        return json_bytes(registry)

    def target(self, key):
        """journal 키: wiki/<위키 상대경로> 또는 state/<상태 폴더 상대경로>"""
        head, _, rel = key.partition("/")
        if head not in {"wiki", "state"} or not rel:
            raise ValueError(f"잘못된 반영 대상: {key}")
        return safe(self.wiki if head == "wiki" else self.state, rel)

    def apply(self, batch, topics):
        with self.lock():
            if self.journal.exists():
                raise ValueError("중단된 반영이 있습니다. recover 필요")
            baseline = self.pages()
            pages, selected, index, output, registry = self.preview(batch, topics, require_approval=True)
            if not selected:
                return "이미 반영됨. 추가 변경 없음."
            if baseline != self.pages():
                raise ValueError("검사 중 위키가 바뀌었습니다. 다시 apply하세요.")
            for topic, m in selected:
                self.current(batch, topic)
                if self.stale(m):
                    raise ValueError(f"검사 중 기준 변경: {topic}")
            writes = {"wiki/" + rel: data for rel, data in pages.items()}
            writes["wiki/index.md"] = index
            writes[f"wiki/{VERIFICATION_DIR}/registry.json"] = registry
            receipts = read_bytes(self.batch(batch) / "applied.jsonl") or b""
            for topic, m in selected:
                decision = self.decision(batch, topic, m["revision"])
                # The receipt records approval and application, not fact verification of the page.
                receipts += json.dumps({"topic": topic, "revision": m["revision"], "at": now(), "files": m["files"],
                                        "decision_evidence": decision["response"]["evidence"]}, ensure_ascii=False).encode() + b"\n"
            writes[f"state/reviews/{batch}/applied.jsonl"] = receipts
            journal = {key: {"before": read_bytes(self.target(key)).decode() if self.target(key).exists() else None,
                             "after": data.decode()} for key, data in writes.items()}
            atomic(self.journal, json_bytes(journal))
            try:
                for key, data in writes.items():
                    atomic(self.target(key), data)
            except Exception:
                self._recover()
                raise
            self.journal.unlink()
            return output + "\n반영 완료: " + ", ".join(x[0] for x in selected)

    def _recover(self):
        journal = json.loads(self.journal.read_text(encoding='utf-8-sig'))
        for key, versions in journal.items():
            current = read_bytes(self.target(key))
            candidates = [v.encode() if v is not None else None for v in versions.values()]
            if current not in candidates:
                raise ValueError(f"복구 대상에 다른 변경이 생김. 수동 대조 필요: {key}")
        for key, versions in journal.items():
            old = versions["before"]
            atomic(self.target(key), old.encode() if old is not None else None)
        self.journal.unlink()

    def recover(self):
        with self.lock():
            if self.journal.exists():
                self._recover()
                return "중단된 반영을 변경 전 상태로 복구했습니다."
            return "복구할 반영 없음."

    def status(self, batch=None):
        if batch is None:
            return {p.name: self.status(p.name) for p in sorted(self.root.glob("*")) if p.is_dir() and ID.fullmatch(p.name)}
        out = []
        for topic in sorted((self.batch(batch) / "topics").glob("*")):
            _, m, card = self.current(batch, topic.name)
            decision = self.decision(batch, topic.name, m["revision"])
            applied = self.applied(batch, topic.name, m["revision"])
            out.append({"topic": topic.name, "title": card["title"], "revision": m["revision"],
                        "state": "applied" if applied else (decision or {}).get("decision", "pending"),
                        "changed_basis": [] if applied else self.stale(m)})
        return out


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    p = sub.add_parser("prepare")
    p.add_argument("batch"); p.add_argument("topic")
    p.add_argument("--card", type=Path, required=True); p.add_argument("--draft-dir", type=Path, required=True)
    p = sub.add_parser("decide")
    p.add_argument("batch"); p.add_argument("topic"); p.add_argument("--revision", required=True)
    p.add_argument("--decision", required=True, choices=["approved", "changes_requested", "deferred", "rejected"])
    p.add_argument("--response", type=Path, required=True)
    for command in ("preview", "apply"):
        p = sub.add_parser(command); p.add_argument("batch"); p.add_argument("topics", nargs="+")
    p = sub.add_parser("status"); p.add_argument("batch", nargs="?")
    sub.add_parser("recover")
    args = parser.parse_args(); review = Review()
    try:
        if args.command == "prepare":
            result = review.prepare(args.batch, args.topic, json.loads(args.card.read_text(encoding='utf-8-sig')), args.draft_dir)
        elif args.command == "decide":
            result = review.decide(args.batch, args.topic, args.revision, args.decision, json.loads(args.response.read_text(encoding='utf-8-sig')))
        elif args.command == "preview":
            result = review.preview(args.batch, args.topics)[3]
        elif args.command == "apply":
            result = review.apply(args.batch, args.topics)
        elif args.command == "status":
            result = review.status(args.batch)
        else:
            result = review.recover()
        print(result if isinstance(result, str) else json.dumps(result, ensure_ascii=False, indent=2))
        return 0
    except (ValueError, OSError, KeyError, TypeError) as error:
        print(f"ERROR: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
