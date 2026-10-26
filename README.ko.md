<h1 align="center">Company Wiki</h1>

<p align="center">
  <strong>조직이 채택한 지식만 concepts·entities·policies로 관리하고, 모든 내용을 원문까지 추적할 수 있는 LLM 위키</strong>
</p>

<p align="center">
  <a href="README.md">English</a> · <strong>한국어</strong>
</p>

---

Claude Code나 Codex가 문서와 AI 세션 기록을 읽고, 조직이 실제로 채택한 지식을 찾아 연결된 Markdown으로 씁니다. 규칙과 사실마다 바로 옆에 `^[파일:줄]` 인용이 붙습니다. 변경은 페이지 담당자(owner)가 승인하고 이력은 Git이 맡습니다. `company-wiki` CLI가 필수 필드·링크·관계·인용, 그리고 채택 이후 인용한 원문 줄이 바뀌었는지를 검사합니다.

[BOAZ Wiki](https://github.com/boaz-hwang/boaz-wiki)를 바탕으로 구조를 바꾼 프로젝트입니다. BOAZ Wiki는 원문 요약·종합 설명·열린 질문·연구·검토 기록까지 위키 안에 둡니다. Company Wiki는 위키를 채택 지식으로 좁히고 나머지는 밖으로 옮겼습니다.

## 위키에 들어가는 것

```text
~/company-wiki/                컴퓨터마다 하나, 고정 위치 (Windows: %USERPROFILE%\company-wiki)
├── wiki.toml                  원천·검토자 설정
├── llm-wiki/                  위키: 채택된 지식만 (공유 Git 저장소)
│   ├── SCHEMA.md              운영 규칙
│   ├── .verification/         인용 검증 기록, 지식 아님 (Obsidian에서 숨김)
│   │   └── registry.json      채택 시점의 인용 줄 해시. 팀·CI가 공유하도록 위키와 함께 커밋
│   ├── index.md               생성되는 입구
│   ├── concepts/              무엇을 의미하는가: 개념·정의·원리·모델
│   ├── entities/              무엇이 존재하는가: 정체성·속성·안정적인 관계
│   │   ├── products/
│   │   └── organizations/
│   └── policies/              어떻게 판단하고 행동하는가: 전략·정책·규칙·판단 기준
│       ├── product/
│       └── engineering/
└── workspace/                 개인 Vault: 기존 폴더, Obsidian Vault 링크, 또는 새 폴더
    └── wiki-changes/          변경 절차 폴더, 지식 아님: 초안·검토 버전·결정·receipt·조사 기록
```

변경 절차 자료는 위키 저장소 밖에 있으므로 결정 전 자료가 실수로 위키에 커밋되지 않습니다. apply는 결정된 버전만 `llm-wiki/`에 복사합니다.

하위 폴더는 자유롭게 둡니다. 최상위 폴더가 페이지 type을 정하므로 조직이 커져도 최상위 폴더는 늘지 않습니다.

| BOAZ Wiki | Company Wiki | 이유 |
|---|---|---|
| `sources/` | 제거 | 인용은 결론 옆에 두고 원문은 원래 위치에 보존 |
| `overviews/` | 제거 | 관계는 `related`에 두고 index가 목록을 생성, 종합은 질의 때 에이전트가 수행 |
| `questions/` | 밖으로 | 미해결 질문은 채택 지식이 아님. 반복 질문은 페이지의 `answers`로 |
| `research/` | 밖으로 | 연구는 채택 전 단계. 확정 결과만 concept·policy로 승격 |
| `reviews/`, `log.md` | `workspace/wiki-changes/`·Git으로 | 검토 기록과 이력은 지식이 아님 |
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

Git과 로컬 파일에 접근하는 Claude Code 또는 Codex가 필요합니다. macOS·Linux·Windows에서 동작하며 Obsidian은 선택입니다.

`company-wiki` 실행 파일을 설치합니다. 런타임 없이 파일 하나로 동작합니다.

```bash
curl -fsSL https://raw.githubusercontent.com/boaz-hwang/company-wiki/main/install.sh | sh          # macOS, Linux
```

```powershell
irm https://raw.githubusercontent.com/boaz-hwang/company-wiki/main/install.ps1 | iex              # Windows PowerShell
```

스크립트가 OS와 CPU에 맞는 파일을 [GitHub Releases](https://github.com/boaz-hwang/company-wiki/releases)에서 받아 `~/.local/bin`에 둡니다(Windows는 `%LOCALAPPDATA%\Programs\company-wiki`에 두고 사용자 PATH에 추가). `company-wiki version`으로 확인합니다. 소스로 빌드하려면 `go install github.com/boaz-hwang/company-wiki/cmd/company-wiki@latest`.

setup은 어느 폴더에서 실행해도 항상 `~/company-wiki`(Windows: `%USERPROFILE%\company-wiki`)를 준비하며 기존 파일을 바꾸지 않습니다. 터미널에서는 원천 종류가 체크박스로 나오고, ↑↓로 이동하고 space로 선택한 뒤 enter로 넘어가 경로를 입력합니다. 없는 경로는 받지 않습니다.

```bash
company-wiki setup
```

에이전트는 터미널 질문에 답할 수 없으므로, 등록할 원천과 경로를 사용자에게 먼저 묻고 옵션으로 넘깁니다(`company-wiki setup --list-kinds`로 종류 확인). `--source`의 상대 경로는 현재 폴더 기준이며 절대 경로(홈 아래면 `~/…`)로 저장됩니다. 나중에 `--source`로 다시 실행하면 원천을 추가하며, 같은 종류·경로로 이미 등록된 원천은 건너뜁니다.

```bash
cd ~/projects && company-wiki setup --source documents=docs --source slack=exports/slack \
  --source code:server=server --source claude=~/.claude/projects/-path-to-project
```

### 온보딩

1. 위의 실행 파일을 설치합니다. 팀 위키에 합류한다면 먼저 그 저장소를 `~/company-wiki/llm-wiki`에 clone합니다. setup은 이미 있는 파일을 유지합니다.
2. `company-wiki setup --source …`를 실행합니다. `~/company-wiki` 아래에 `wiki.toml`, `llm-wiki/`(폴더·SCHEMA.md·index), `workspace/wiki-changes/`, 스킬, `AGENTS.md`가 생깁니다.
3. `workspace/`는 개인 Vault이며 질문 없이 정해집니다. `workspace` 폴더나 링크가 이미 있으면 그대로 쓰고, 없으면 `--vault <폴더>`, 그다음 Obsidian 설정 `obsidian.json`에서 찾은 Vault(열린 Vault, 없으면 최근 사용한 Vault. `wiki.toml`이 있거나 `~/company-wiki` 안에 있는 Vault는 제외)에 연결하고, 둘 다 없으면 빈 폴더를 만듭니다. Vault 안에는 `wiki-changes/`만 추가하고 기존 내용은 옮기거나 바꾸지 않습니다. 어느 경우가 적용됐는지 setup이 출력합니다. Vault를 바꾸려면 링크만 지우고(`rm ~/company-wiki/workspace`, Windows는 `rmdir`) `--vault`로 다시 실행합니다.
4. `llm-wiki/`가 아직 Git 저장소가 아니면 `git -C ~/company-wiki/llm-wiki init`으로 만들고 팀 원격 저장소를 연결합니다.
5. `~/company-wiki`를 Claude Code나 Codex로 엽니다. 명령은 어느 폴더에서나 동작합니다.

또는 아무 폴더나 코딩 에이전트로 열고 괄호를 채워 붙여 넣으세요.

```text
[조직 또는 팀]이 채택한 지식으로 LLM 위키를 만들거야.

company-wiki README(https://github.com/boaz-hwang/company-wiki)와
`company-wiki setup`이 설치하는 SCHEMA·스킬을 따라줘.

- 문서·프로젝트 경로: [...]
- Slack·Jira·Notion·Confluence: [내보낸 폴더와 대상 채널·프로젝트·스페이스, 또는 없음]
- 관련 세션·메모리: [위치 또는 확인 필요]
- 위키 Git 저장소(~/company-wiki/llm-wiki에 clone): [원격 저장소 또는 확인 필요]
- 영역별 담당자: [예: product: product-owner, 또는 확인 필요]
- 제외할 자료: [없음 또는 범위]

먼저 현재 문서와 Vault 구조를 살펴봐줘. 지정한 원천에서 우리가 채택한 것만
concepts, entities, policies에 넣고 규칙과 사실마다 원문 줄 인용을 붙여줘.
원문 요약이나 종합 설명 페이지는 만들지 마. 미해결 질문, 진행 중인 연구,
채택하지 않은 주장은 위키에 넣지 말고 따로 보고해줘.

조사 범위와 미확인을 기록하고, 변경안과 내가 판단할 내용을 한 번에 보여줘.
내가 승인한 것만 company-wiki review로 반영해 위키 저장소 main에 커밋하고(PR 없이),
이후 이 저장소의 스킬로 업데이트·검토·조회할 수 있게 설정해줘.
```

## 변경 승인

변경은 위키 저장소의 main 브랜치에 바로 커밋합니다. PR·브랜치는 쓰지 않습니다.

- **에이전트 제안 (`company-wiki review`):** `workspace/wiki-changes/`에서 초안 → `prepare → preview` → 담당자가 세션에서 답변 → `decide → apply` → main에 커밋·푸시. 결정은 Claude Code·Codex 세션의 실제 사용자 메시지를 인용해야 합니다. 버전·결정·receipt는 `workspace/wiki-changes/`에 남고, apply는 lint를 통과한 뒤 승인된 페이지와 index만 위키에 쓰고 레지스트리를 갱신하며, trailer(`Wiki-Change`·`Wiki-Revision`·`Approved-By`·`Approval-Evidence`)로 위키 커밋을 그 결정에 잇는 커밋 메시지를 출력합니다. [workflow.md](skills/wiki-review/references/workflow.md) 참고.
- **담당자 직접 편집:** 담당자는 자기 페이지를 Obsidian·IDE에서 직접 고칠 수 있습니다. lint → `company-wiki registry record <페이지>` → main에 커밋·푸시.

main에 push할 때마다 `company-wiki lint --wiki-only`를 돌리려면 [templates/wiki-check.yml](templates/wiki-check.yml)을 위키 저장소에 복사합니다. 원문이 없는 러너에서 구조만 검사하고, `card.json`·`decisions.jsonl`·`.md.txt` 초안 같은 변경 절차 자료가 위키에 커밋됐으면 실패합니다. push 뒤의 감지 장치이며, 반영 전 검사는 apply의 lint와 담당자의 lint가 맡습니다.

## 원문 변경 감지

[토스의 근거와 상태를 가진 타입 관계](https://toss.tech/article/llm_context_topic)에서 착안해, 인용한 줄 범위를 채택 시점에 해시로 기록합니다. 위키에는 원문 사본을 두지 않습니다.

```bash
company-wiki registry                          # 바뀐·이동한·만료된·미기록 인용과 영향받는 페이지
company-wiki registry --source /documents/x.md # 이 원문에 기대는 페이지
company-wiki registry --source ../docs/x.md    # 같은 조회를 등록 원천 안의 실제 파일 경로로
company-wiki registry --page concepts/x        # 이 페이지가 바뀌면 영향받는 페이지
company-wiki registry record concepts/x        # 페이지를 재확인한 뒤 갱신
```

`changed`는 그 줄을 인용한 문장·관계를 재검토하라는 뜻입니다. `moved`는 내용은 같고 원문의 줄 위치만 바뀌었다는 뜻이라 인용 번호만 고치면 됩니다. 영향받는 페이지는 `related`의 `depends_on`·`implements`·`applies_to` 관계를 거꾸로, 연쇄적으로 따라가 찾습니다. `expired`는 세션 파일이 정리돼 근거를 다시 확인할 수 없다는 뜻입니다. Claude Code는 `cleanupPeriodDays` 동안 세션을 보관하므로 보관 기간을 늘리거나 결과 문서를 근거로 바꾸세요. lint는 이것들을 경고로, 문서 원문이 없으면 오류로 보고합니다. `--source`에는 인용 형식 `/별칭/경로`나 등록 원천 폴더 안 파일의 상대·절대·`~` 경로를 씁니다. 어느 원천에도 속하지 않는 경로는 오류입니다.

에이전트는 문제를 감지하고 담당자에게 알리며 수정안을 제안할 뿐, 공식 지식을 무효화하거나 대체하지 않습니다. 담당자가 승인하기 전까지는 기존 지식이 기준이고, 에이전트는 발견한 위험을 함께 밝힙니다. 법률·보안·안전 같은 상위 제약을 어기는 실행은 하지 않습니다(SCHEMA 7절).

## 첫 구축 이후

| 하고 싶은 일 | 요청 예시 | 스킬 |
|---|---|---|
| 전체 최신화 | “지난 검토 이후 문서와 세션을 대조해서 위키 업데이트해줘.” | [wiki-update](skills/wiki-update/SKILL.md) |
| 특정 자료 반영 | “이 회의록의 결정을 위키에 넣어줘.” | [wiki-ingest](skills/wiki-ingest/SKILL.md) |
| 검토·수정·반영 | “1, 2번은 반영하고 3번은 이 표현으로 고쳐줘.” | [wiki-review](skills/wiki-review/SKILL.md) |
| 근거 있는 답 | “위키 기준으로 코드 리뷰 규칙이 뭐야?” | [wiki-query](skills/wiki-query/SKILL.md) |

setup이 바이너리에 들어 있는 스킬 4개를 `~/company-wiki/.claude/skills/`와 `.agents/skills/`에 복사합니다. 업그레이드 후 다시 실행하면 복사본을 갱신하고, 사용자가 고친 스킬은 보존합니다.

## 설정

setup이 고른 원천으로 `wiki.toml`을 만듭니다. 모든 옵션은 [wiki.example.toml](wiki.example.toml)에 있습니다. 위치는 고정입니다. 위키는 항상 `wiki.toml` 옆의 `llm-wiki/`, 변경 절차 자료는 항상 `workspace/wiki-changes/`이며 경로 설정은 없습니다(`[wiki] path`·`state`가 남은 `wiki.toml`은 거부). 원천은 경로와 kind로 등록합니다. 원천의 상대 경로는 설정 파일 기준입니다.

- **kind:** `documents`·`code`·`slack`·`jira`·`notion`·`confluence`·`claude`·`codex`. 개인 웹 클리핑 같은 개인 수집 자료는 원천이 아닙니다.
- **문서·코드:** Markdown·MDX·소스 파일의 파일/줄 인용. 다른 형식은 읽을 수 있는 텍스트로 먼저 변환합니다.
- **Slack·Jira·Notion·Confluence:** 등록 폴더에 필요한 내용만 텍스트로 내보내고 원본 URL·ID를 함께 남겨 인용합니다. 전용 커넥터는 없으며, 에이전트가 쓸 수 있는 커넥터·MCP로 원문을 찾고 update 때 원본과 다시 대조합니다.
- **세션:** Claude Code·Codex JSONL의 역할·원본 줄 번호, 실제 사용자 답변 검증.
- **메모리·Git·다른 에이전트:** 원문과 사용자 발언을 찾는 단서로 읽습니다.
- **원천 범위:** include/exclude는 에이전트에 대한 지시이며 샌드박스가 아닙니다.

CLI는 LLM을 호출하지 않습니다. 검사 통과가 내용의 정확성을 증명하지는 않으며, 그것은 담당자 승인이 맡습니다. 위키(`llm-wiki/`)는 별도 저장소에 두고 공개 범위를 먼저 정하세요. `wiki.toml`과 `workspace/`는 각자의 컴퓨터에 남습니다.

명령은 `WIKI_CONFIG=/path/to/wiki.toml`이 있으면 그것을, 없으면 현재 폴더부터 위로 찾은 `wiki.toml`을, 그것도 없으면 `~/company-wiki/wiki.toml`을 쓰므로 어느 폴더에서나 동작합니다. Windows 경로는 `wiki.toml`에 작은따옴표로 씁니다.

## 예제와 검사

[가상 반품 정책 예제](examples/README.ko.md)에 정책·개념·타입 관계·레지스트리·결정 대기 중인 변경안이 있습니다.

```bash
WIKI_CONFIG=examples/en/wiki.toml company-wiki lint
WIKI_CONFIG=examples/en/wiki.toml company-wiki registry
go test ./...      # 도구 개발
```

## 라이선스

[BOAZ Wiki](https://github.com/boaz-hwang/boaz-wiki)를 바탕으로 합니다. BOAZ Wiki는 [Open Knowledge Format](https://github.com/GoogleCloudPlatform/open-knowledge-format)과 [OpenWiki](https://github.com/langchain-ai/openwiki)에서 아이디어를 얻었고 코드는 포함하지 않습니다. 코드·스킬·문서·가상 예제는 [MIT License](LICENSE)로 제공됩니다.
