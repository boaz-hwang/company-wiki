# 위키 검토 도구 사용법

`company-wiki` CLI로 실행한다. 명령은 어느 폴더에서나 ~/company-wiki/wiki.toml을 쓴다(WIKI_CONFIG나 현재 폴더부터 위로 찾은 wiki.toml이 있으면 그것). 규칙은 `SCHEMA.md`의 변경과 승인 절이다. 위키는 ~/company-wiki/llm-wiki, 변경 절차 폴더는 ~/company-wiki/workspace/wiki-changes로 고정이며 검토자는 wiki.toml을 따른다.

## 반영 경로 (main 직접 커밋)

PR·브랜치는 쓰지 않고 위키 저장소의 main에 직접 커밋·푸시한다. 채택 승인은 Git 병합이 아니라 담당자의 decide 기록이다.

- 에이전트 제안(기본): 변경 절차 폴더의 초안 → prepare → preview → 담당자가 세션에서 답변 → decide → apply → main 커밋·푸시. 아래 검토 도구 절차를 따른다.
- 담당자 직접 편집: 자기 페이지를 고친 뒤 lint → `registry record <페이지>` → main 커밋·푸시.

```bash
# 에이전트 제안: apply 성공 뒤. 메시지는 apply가 출력한 "커밋 메시지:" 아래 내용을 그대로 쓴다
git -C ~/company-wiki/llm-wiki add <반영 페이지> index.md .verification/registry.json
git -C ~/company-wiki/llm-wiki commit -F - <<'EOF'
<카드 제목>

Wiki-Change: <batch>/<topic>
Wiki-Revision: <전체 revision 해시>
Approved-By: <response.by>
Approval-Evidence: <response.evidence>
EOF
git -C ~/company-wiki/llm-wiki push origin main

# 담당자 직접 편집
company-wiki index && company-wiki lint
company-wiki registry record <바꾼 페이지>   # 재확인한 인용 해시를 .verification/registry.json에 기록
company-wiki registry --page <바꾼 페이지>   # 함께 재검토할 의존 페이지
git -C llm-wiki add <바꾼 페이지> index.md .verification/registry.json
git -C llm-wiki commit -m "<변경 요약>" && git -C llm-wiki push origin main
```

main에 있으면 채택이며 이력은 Git이 맡는다. 판단·근거는 변경 절차 폴더의 카드·decisions.jsonl에 남고, 커밋 trailer(Wiki-Change·Wiki-Revision·Approved-By·Approval-Evidence)가 위키 커밋을 그 결정에 잇는다. 여러 주제를 한 번에 반영하면 제목은 `;`로 잇고 trailer는 주제마다 반복된다. 원문 변경(changed)을 재검토해 본문은 그대로 두고 해시만 갱신할 때도 담당자 확인 뒤 `registry record <페이지>`로 기록해 main에 커밋한다. 위키 저장소에서 main push마다 `company-wiki lint --wiki-only`로 구조를 검사하려면 templates/wiki-check.yml을 쓴다. push 뒤의 감지 장치이며, 반영 전 검사는 apply의 lint와 직접 편집 때의 lint가 맡는다.

## 검토 도구 (company-wiki review)

에이전트 제안은 이 도구로 담당자 결정을 받는다. 검토 파일은 변경 절차 폴더에 남고 위키에는 승인된 페이지·index·.verification/registry.json만 쓰인다.

## 파일 배치

```text
~/company-wiki/workspace/wiki-changes/<batch>/
  working/<topic>/card.json
  working/<topic>/draft/policies/example.md.txt
  working/<topic>/draft/concepts/example.md.txt
  topics/<topic>/r0001/
    card.json
    manifest.json
    review.md
    before/<정식 페이지 경로>.txt
    after/<정식 페이지 경로>.txt
  decisions.jsonl
  applied.jsonl
```

`batch`, `topic`은 영문 소문자·숫자·하이픈 ID. `working`은 수정 가능하고 rNNNN은 보존 사본이다. draft에는 카드 files에 나열한 변경 파일만 `.txt`를 덧붙여 둔다. 예: files의 `policies/product/example.md` → `draft/policies/product/example.md.txt`. 전후 사본도 `.md.txt`로 보존해 Obsidian 정식 링크와 충돌하지 않는다. 전체 위키 복사본을 넣지 않는다. 같은 페이지가 겹치는 미반영 주제는 한 묶음으로 준비한다. batch를 달리해 겹치더라도 먼저 반영된 변경 때문에 뒤의 기준 버전이 달라져 재검토가 필요하다.

## 검토 카드 예시

```json
{
  "title": "반품 정책 적용 범위",
  "summary": "주문 시점에 따른 반품 기간을 구분한다.",
  "files": ["policies/returns.md", "entities/example.md"],
  "judgments": [
    {
      "kind": "fact",
      "before": "모든 주문 14일",
      "proposed": "2월 신규 주문은 30일",
      "reason": "사용자가 변경 정책의 적용 시점을 확인했다",
      "evidence": ["^[/codex-sessions/YYYY/MM/DD/rollout-example.jsonl:123]"]
    },
    {
      "kind": "scope",
      "before": "이전 주문 적용 여부 불명확",
      "proposed": "1월 주문은 기존 14일 유지",
      "reason": "기존 주문과 신규 주문을 구분한다",
      "evidence": ["^[/codex-sessions/YYYY/MM/DD/rollout-example.jsonl:123]"]
    }
  ],
  "dependencies": ["/documents/policy-change.md"]
}
```

예시 경로·줄은 실제 읽은 원문으로 바꾼다. `judgments`에는 fact, interpretation, naming, relationship, confidence, scope 중 관련 유형을 모두 담는다. 각각 before/proposed/reason/evidence 필수. evidence가 없는 해석은 빈 배열로 두고 불확실성을 reason에 설명한다. 변경 파일의 실제 diff가 최종 범위이며 카드 요약과 일치해야 한다.

도구가 각 초안의 줄 인용·로컬 sources·이미 존재하는 연결 페이지를 의존 근거에 자동 추가한다. 간접 근거도 dependencies로 지정한다. `/경로`는 전체 파일 버전, `^[/경로:줄-줄]`은 인용 줄 버전에 묶인다. 계속 추가되는 JSONL 세션은 반드시 줄 인용을 쓴다. 외부 URL·Slack·Jira·Notion·Confluence의 내용 버전은 자동 확인하지 않으므로 등록 원천 폴더에 내보낸 사본을 근거로 둔다.

특정 연결만 허용할 때 카드에 다음 필드를 더한다. 슬러그는 위키 루트 기준 정식 경로이며 `.md`를 붙이지 않는다. 배열에 없는 연결은 반영을 막는다. 생략한 방향은 제한하지 않는다. 빈 배열은 그 방향 연결을 모두 금지한다.

```json
{
  "link_constraints": [
    {
      "target": "policies/returns",
      "inbound_from": ["concepts/customer-service"],
      "outbound_to": ["concepts/customer-service"]
    }
  ]
}
```

이 검사는 선택한 변경과 기존 전체 페이지를 합친 상태에서 본문 위키링크 및 related 대상을 비교한다. 링크 별칭·짧은 슬러그도 정식 경로로 해석한다. 일반 텍스트로 흘러간 해석이나 향후 별도 변경의 범위는 자동 감지하지 않으므로 카드·기존 사용자 결정과 별도로 대조한다.

## 준비와 검토

```bash
company-wiki review prepare batch-id topic-id --card ~/company-wiki/workspace/wiki-changes/batch-id/working/topic-id/card.json --draft-dir ~/company-wiki/workspace/wiki-changes/batch-id/working/topic-id/draft
company-wiki review preview batch-id topic-id
company-wiki review status batch-id
```

prepare가 전체 revision 해시와 review.md 경로를 출력한다. preview는 임시 합본에서 index 생성과 lint를 자동 실행해 index·인용·링크를 검사한다. 이 구조 검사 뒤 근거·의미·범위 검토는 wiki-review가 수행한다. 정식 페이지나 index는 바꾸지 않는다. 서로 새 페이지를 참조하는 주제는 `preview batch-id topic-a topic-b`로 함께 검사한다.

사용자에게 보내는 최종 답변은 SKILL.md의 출력 계약에 따른 간결한 번호 질문 목록이다. 카드·검사 로그·상세 diff는 내부 검토 자료로 보존하고 질문에 필요한 맥락만 추린다. 실제 답을 기다리되 승인 대기를 이유로 준비·검사를 미루지 않는다.

출력 예시:

1. 현재 “모든 주문 14일”을 “2월 신규 주문만 30일, 1월 주문은 14일 유지”로 반품 정책에 반영할까요? (추천: 반영 / 수정 / 보류 / 제외)
2. 문서 A의 “출시 완료”와 회의록 B의 “출시 예정”은 같은 제품·시점의 기록인데, 어느 쪽이 맞나요?

## 사람 판단 기록

사용자의 답을 다음 response.json으로 옮긴다. 파일은 `<batch>/working/`에 둔다. 판단은 카드의 정확한 버전에만 적용한다.

```json
{
  "by": "human:owner",
  "evidence": "^[/codex-sessions/YYYY/MM/DD/rollout-example.jsonl:150]",
  "quote": "2월 신규 주문은 30일, 1월 주문은 14일로 반영해줘.",
  "judgments": [
    {"kind": "fact", "value": "2월 신규 주문은 30일"},
    {"kind": "scope", "value": "1월 주문은 기존 14일 유지"}
  ]
}
```

```bash
company-wiki review decide batch-id topic-id --revision FULL_REVISION_HASH --decision approved --response ~/company-wiki/workspace/wiki-changes/batch-id/working/response.json
```

decision: approved / changes_requested / deferred / rejected. quote는 실제 사용자 메시지의 연속된 원문이어야 한다. Claude 일반·작업 중 추가 메시지와 Codex 사용자 메시지를 지원한다. 모델·도구 출력을 인용하면 실패한다. 원본 세션 파일은 위키에 복사하지 않는다. 세션 원본 위치는 wiki.toml의 claude·codex 원천(별칭 claude-sessions·codex-sessions)이다.

“제안대로”는 사용자가 보고 답한 항목에만 매핑한다. 수정 지시가 있으면 기존 버전에 changes_requested를 기록하고 초안을 고쳐 prepare한다. 그 지시를 그대로 구현한 새 버전은 같은 사용자 발언을 근거로 approved를 기록할 수 있다. 지시에 없던 결론·연결까지 생기면 그 차이를 다시 물어야 한다. 응답 유형별 내용을 정리하는 것은 에이전트의 책임이며 스크립트가 의미 일치를 판정하지 않는다.

## 반영과 재검토

```bash
company-wiki review apply batch-id topic-id
company-wiki review status
```

apply는 최신 버전의 승인·근거·변경 전 상태를 다시 확인하고 임시 전체 lint를 통과한 뒤 반영한다. 위키에는 선택한 페이지·파생 index·.verification/registry.json(인용 줄 해시)만 쓰고, 변경 절차 폴더에는 반영 receipt(applied.jsonl)를 쓴다. 적용된 동일 버전 재실행은 추가 쓰기를 하지 않고 같은 커밋 메시지를 다시 출력한다. 반영 뒤 apply가 출력한 커밋 메시지로 반영 경로대로 main에 커밋·푸시한다.

기준이 바뀌면 오류가 해당 항목과 경로를 알려준다. 그 주제만 현재 문서에서 다시 준비해 차이를 검토한다. 다른 주제 승인은 재사용한다. 경고는 사람이 판단할 내용으로 보고하며 errors는 반영을 막는다. 소스·세션을 읽을 수 없으면 검사를 우회하지 말고 그 항목의 근거를 복원한다.

쓰기에 실패하면 이전 상태로 되돌린다. 프로세스가 중단되어 journal이 남은 경우:

```bash
company-wiki review recover
```

복구 대상에 제3의 편집 내용이 있으면 덮어쓰지 않고 중단한다. journal before/after와 현재 파일을 대조하고 사용자 작업을 보존한 뒤 재시도한다. 위키에는 변경 페이지·index·.verification/registry.json만 커밋한다. 카드·판단·receipt는 변경 절차 폴더의 기록이며 위키 저장소에 커밋하지 않는다.
