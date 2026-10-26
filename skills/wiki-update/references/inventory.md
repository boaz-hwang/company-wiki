# 조사 범위 기록

변경 절차 폴더(~/company-wiki/workspace/wiki-changes)의 <batch>/inventory.json에 조사 경계를 남긴다. 실제 반영 기록은 위키의 Git 이력과 applied.jsonl이다.

- version: 1
- period: from/through, 시간대 포함 ISO 시각
- basis: last_apply/last_full_review. 각각 객체가 아니라 근거 경로·시각·범위를 담은 문자열 하나. 최초는 `"unknown: <전체 조사 이유>"`
- sources: wiki.toml의 활성 원천별 id(설정 별칭)·kind·location·status·reason·items
- 원천 status: scanned / partial / unavailable / not_applicable. 접근 실패는 unavailable. 빈 scanned는 실제 확인한 0건
- items: id·reference·status(reviewed/unread)·reason. reviewed이면 classification(new/update/conflict/duplicate/out_of_scope)과 comparison(기존 페이지와 대조 결과) 필수

예: wiki.toml에 documents(`documents=docs`)와 claude(`claude=...`, 별칭 claude-sessions)를 등록한 경우.

```json
{
  "version": 1,
  "period": {"from": "2026-09-01T00:00:00+09:00", "through": "2026-09-30T23:59:59+09:00"},
  "basis": {
    "last_apply": "2026-08-returns/applied.jsonl 2026-08-28T17:10:00+09:00, policies/returns 반영",
    "last_full_review": "2026-08-returns/inventory.json, 2026-08-01~2026-08-31 전체 원천"
  },
  "sources": [
    {
      "id": "documents", "kind": "documents", "location": "docs", "status": "scanned",
      "reason": "기간 안에 바뀐 문서를 모두 읽고 기존 페이지와 대조",
      "items": [
        {"id": "policy-change", "reference": "/documents/policy-change.md", "status": "reviewed", "reason": "반품 기간 변경 공지",
         "classification": "update", "comparison": "policies/returns의 14일을 2월 신규 주문 30일로 바꾸는 후보"},
        {"id": "faq", "reference": "/documents/faq.md", "status": "reviewed", "reason": "기존 기준을 다시 적은 문서",
         "classification": "duplicate", "comparison": "policies/returns와 같은 내용"}
      ]
    },
    {
      "id": "claude-sessions", "kind": "claude", "location": "~/.claude/projects/-path-to-project", "status": "scanned",
      "reason": "기간 안의 세션을 모두 읽음. 승인 근거 외 새 결정 없음", "items": []
    }
  ]
}
```

첫 실행이면 basis를 `"last_apply": "unknown: 반영 기록 없음"`, `"last_full_review": "unknown: 첫 실행이라 위키 생성 전 원천 전체 조사"`처럼 쓴다.

현재 검토 대화처럼 승인 근거로만 등록한 원천은 not_applicable과 이유를 기록할 수 있다. 같은 kind의 서로 다른 폴더도 원천별로 기록한다.

`company-wiki inventory ~/company-wiki/workspace/wiki-changes/<batch>/inventory.json --require-complete`

종료 0은 기록상 완료, 2는 유효하지만 미완료, 1은 형식 오류다. 모든 활성 원천과 검토 항목이 완료되어야 through가 반환된다. 선언된 기록의 일관성을 검사할 뿐 실제 원천 누락·읽기·의미 대조를 보증하지 않는다. 다음 실행은 전체 조사 경계와 미반영·보류 항목을 함께 이어받는다.
