<h1 align="center">Company Wiki</h1>

<p align="center">
  <strong>조직이 채택한 지식만 concepts·entities·policies로 관리하고, 모든 내용을 원문까지 추적할 수 있는 LLM 위키</strong>
</p>

<p align="center">
  <a href="README.md">English</a> · <strong>한국어</strong>
</p>

---

Claude Code나 Codex가 문서와 AI 세션 기록을 읽고, 조직이 실제로 채택한 지식을 찾아 연결된 Markdown으로 씁니다. 규칙과 사실마다 바로 옆에 `^[파일:줄]` 인용이 붙습니다. 변경은 페이지 담당자(owner)가 승인하고 이력은 Git이 맡습니다. Python 도구는 필수 필드·링크·관계·인용, 그리고 채택 이후 인용한 원문 줄이 바뀌었는지를 검사합니다.

[BOAZ Wiki](https://github.com/boaz-hwang/boaz-wiki)를 바탕으로 구조를 바꾼 프로젝트입니다. BOAZ Wiki는 원문 요약·종합 설명·열린 질문·연구·검토 기록까지 위키 안에 둡니다. Company Wiki는 위키를 채택 지식으로 좁히고 나머지는 밖으로 옮겼습니다.

## 위키에 들어가는 것

```text
llm-wiki/                  채택된 지식만 (별도 Git 저장소 또는 폴더)
├── SCHEMA.md              운영 규칙
├── index.md               생성되는 입구
├── concepts/              무엇을 의미하는가: 개념·정의·원리·모델
├── entities/              무엇이 존재하는가: 정체성·속성·안정적인 관계
│   ├── products/
│   └── organizations/
└── policies/              어떻게 판단하고 행동하는가: 전략·정책·규칙·판단 기준
    ├── product/
    └── engineering/

.llm-wiki/                 작업 공간, 지식 아님 (Obsidian에서 숨김)
├── registry.json          채택 시점의 인용 줄 해시
├── excluded.jsonl         사용자가 제외하기로 한 원천
└── reviews/<batch>/       초안·검토 버전·결정·receipt·조사 기록
```

하위 폴더는 자유롭게 둡니다. 최상위 폴더가 페이지 type을 정하므로 조직이 커져도 최상위 폴더는 늘지 않습니다.

| BOAZ Wiki | Company Wiki | 이유 |
|---|---|---|
| `sources/` | 제거 | 인용은 결론 옆에 두고 원문은 원래 위치에 보존 |
| `overviews/` | 제거 | 관계는 `related`에 두고 index가 목록을 생성, 종합은 질의 때 에이전트가 수행 |
| `questions/` | 밖으로 | 미해결 질문은 채택 지식이 아님. 반복 질문은 페이지의 `answers`로 |
| `research/` | 밖으로 | 연구는 채택 전 단계. 확정 결과만 concept·policy로 승격 |
| `reviews/`, `log.md` | 작업 공간·Git으로 | 검토 기록과 이력은 지식이 아님 |
| `status`, `verified` | 제거 | 기본 브랜치에 있으면 채택된 것, 이력은 Git |
| — | `policies/` 추가 | 현재 적용하는 판단·행동 기준 |

`entities/`는 CRM·인사 DB·제품 DB의 복사본이 아닙니다. 조직이 채택한 대상의 정의와 안정적인 관계만 두고, 자주 바뀌는 값은 원래 시스템을 가리킵니다.

## 문서 형식

```yaml
---
type: policy
title: 훈련 추천 정책
description: 선수 훈련 추천 시 적용하는 기준
owner: product-owner
sources:
  - ^[/documents/training.md:12-20]
related:
  - concepts/training-load
  - target: entities/products/plco      # 선택: 근거가 있는 타입 관계
    type: applies_to
    evidence: ^[/documents/training.md:15]
answers: [훈련 강도는 어떻게 정하나?]   # 선택: index의 질문 → 페이지
aliases: [훈련 가이드]                 # 선택
---
```

필수 키는 `type`·`title`·`description`·`owner`·`sources`입니다. 관계 종류: `part_of`, `depends_on`, `implements`, `applies_to`, `supersedes`, `contradicts`, `related_to`. 자세한 규칙은 [SCHEMA.md](SCHEMA.md)와 [templates/page.md.txt](templates/page.md.txt)를 보세요.

## 시작하기

Python 3.11 이상, Git, 로컬 파일에 접근하는 Claude Code 또는 Codex가 필요합니다. Python 외 패키지는 없습니다. macOS·Linux·Windows PowerShell에서 동작하며 Obsidian은 선택입니다.

```bash
git clone https://github.com/boaz-hwang/company-wiki.git
cd company-wiki
```

이 폴더를 코딩 에이전트로 열고 괄호를 채워 붙여 넣으세요.

```text
[조직 또는 팀]이 채택한 지식으로 LLM 위키를 만들거야.

이 저장소의 README, AGENTS, SCHEMA, 스킬을 따라줘.

- 문서·프로젝트 경로: [...]
- 관련 세션·메모리: [위치 또는 확인 필요]
- 위키 위치와 Git 저장소: [경로 또는 확인 필요]
- 영역별 담당자: [예: product: product-owner, 또는 확인 필요]
- 제외할 자료: [없음 또는 범위]

먼저 현재 문서와 Vault 구조를 살펴봐줘. 지정한 원천에서 우리가 채택한 것만
concepts, entities, policies에 넣고 규칙과 사실마다 원문 줄 인용을 붙여줘.
원문 요약이나 종합 설명 페이지는 만들지 마. 미해결 질문, 진행 중인 연구,
채택하지 않은 주장은 위키에 넣지 말고 따로 보고해줘.

조사 범위와 미확인을 기록하고, 변경안과 내가 판단할 내용을 한 번에 보여줘.
PR로 반영하고(여기서 승인하면 review.py로), 이후 이 저장소의 스킬로
업데이트·검토·조회할 수 있게 설정해줘.
```

## 변경 승인

- **Git PR (기본):** 작업 공간에서 초안 → 위키 저장소에 PR → 담당자 승인 → 병합 → `python3 -B scripts/registry.py record`. PR마다 lint를 돌리려면 [templates/wiki-pr-check.yml](templates/wiki-pr-check.yml)을 위키 저장소에 복사합니다.
- **세션 안 승인 (`review.py`):** `prepare → preview → decide → apply`. 결정은 Claude Code·Codex 세션의 실제 사용자 메시지를 인용해야 합니다. 버전·결정·receipt는 작업 공간에 남고, apply는 승인된 페이지와 index만 위키에 쓰고 레지스트리를 갱신합니다. [workflow.md](skills/wiki-review/references/workflow.md) 참고.

## 원문 변경 감지

[토스의 근거와 상태를 가진 타입 관계](https://toss.tech/article/llm_context_topic)에서 착안해, 인용한 줄 범위를 채택 시점에 해시로 기록합니다. 위키에는 원문 사본을 두지 않습니다.

```bash
python3 -B scripts/registry.py                          # 바뀐·만료된·미기록 인용
python3 -B scripts/registry.py --source /documents/x.md # 이 원문에 기대는 페이지
python3 -B scripts/registry.py record concepts/x        # 페이지를 재확인한 뒤 갱신
```

`changed`는 그 줄을 인용한 문장·관계를 재검토하라는 뜻입니다. `expired`는 세션 파일이 정리돼 근거를 다시 확인할 수 없다는 뜻입니다. Claude Code는 `cleanupPeriodDays` 동안 세션을 보관하므로 보관 기간을 늘리거나 결과 문서를 근거로 바꾸세요. lint는 둘 다 경고로, 문서 원문이 없으면 오류로 보고합니다.

## 첫 구축 이후

| 하고 싶은 일 | 요청 예시 | 스킬 |
|---|---|---|
| 전체 최신화 | “지난 검토 이후 문서와 세션을 대조해서 위키 업데이트해줘.” | [wiki-update](skills/wiki-update/SKILL.md) |
| 특정 자료 반영 | “이 회의록의 결정을 위키에 넣어줘.” | [wiki-ingest](skills/wiki-ingest/SKILL.md) |
| 검토·수정·반영 | “1, 2번은 반영하고 3번은 이 표현으로 고쳐줘.” | [wiki-review](skills/wiki-review/SKILL.md) |
| 근거 있는 답 | “위키 기준으로 코드 리뷰 규칙이 뭐야?” | [wiki-query](skills/wiki-query/SKILL.md) |

setup이 스킬 4개를 프로젝트의 `.claude/skills/`와 `.agents/skills/`에 설치합니다. Windows는 복사, 그 외는 심링크이며 사용자가 고친 스킬은 보존합니다.

## 설정

setup이 [wiki.example.toml](wiki.example.toml)로 `wiki.toml`을 만듭니다. `[wiki] path`는 위키 폴더(기본 `llm-wiki`), `state`는 작업 공간(기본 `.llm-wiki`)이며 서로 분리돼야 합니다. 원천은 경로와 kind로 등록합니다. 상대 경로는 설정 파일 기준이고, 다른 작업 공간은 `WIKI_CONFIG=/path/to/wiki.toml`로 고릅니다.

- **문서:** Markdown·MDX의 파일/줄 인용. 다른 형식은 읽을 수 있는 텍스트로 먼저 변환합니다.
- **세션:** Claude Code·Codex JSONL의 역할·원본 줄 번호, 실제 사용자 답변 검증.
- **메모리·Git·다른 에이전트:** 원문과 사용자 발언을 찾는 단서로 읽습니다. 전용 커넥터는 없습니다.
- **원천 범위:** include/exclude는 에이전트에 대한 지시이며 샌드박스가 아닙니다.

Python 도구는 LLM을 호출하지 않습니다. 검사 통과가 내용의 정확성을 증명하지는 않으며, 그것은 담당자 승인이 맡습니다. 이 도구 저장소에서는 로컬 `wiki.toml`, `llm-wiki/`, `.llm-wiki/`를 Git에서 제외합니다. 위키는 별도 저장소에 두고 공개 범위를 먼저 정하세요.

Windows PowerShell에서는 `python3` 대신 `python`(또는 `py -3`)을 쓰고, `wiki.toml`의 Windows 경로는 작은따옴표로 씁니다.

## 예제와 검사

[가상 반품 정책 예제](examples/README.ko.md)에 정책·개념·타입 관계·레지스트리·결정 대기 중인 변경안이 있습니다.

```bash
python3 -B -m unittest discover -s scripts -p 'test_*.py'
python3 -B scripts/lint.py
python3 -B scripts/registry.py
```

## 라이선스

[BOAZ Wiki](https://github.com/boaz-hwang/boaz-wiki)를 바탕으로 합니다. BOAZ Wiki는 [Open Knowledge Format](https://github.com/GoogleCloudPlatform/open-knowledge-format)과 [OpenWiki](https://github.com/langchain-ai/openwiki)에서 아이디어를 얻었고 코드는 포함하지 않습니다. 코드·스킬·문서·가상 예제는 [MIT License](LICENSE)로 제공됩니다.
