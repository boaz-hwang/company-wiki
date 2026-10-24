---
wiki_format: "2"
generated: { by: process:build_index.py, at: 2026-10-24T14:34:52Z }
---

# Fictional Store Wiki

조직이 현재 채택한 지식 2개. 이 파일은 `scripts/build_index.py`가 생성한다. 질문으로 찾고, 없으면 폴더별 목록에서 별칭과 관계를 따라간다. 규칙은 [[SCHEMA]].

## 질문으로 찾기

- How long does a customer have to request a return? → [[policies/customer-service/return-window|Return window]]

## 개념 (concepts) — 무엇을 의미하는가 · 1

- [[concepts/change-of-mind-return|Change-of-mind return]] — A return requested because the customer no longer wants the item, as opposed to a defect; the made-to-order exclusion applies only to this kind. · 담당 store-operations
  - 별칭: change of mind
  - 관계: [[policies/customer-service/return-window|Return window]]

## 정책 (policies) — 어떻게 판단하고 행동하는가 · 1

### customer-service

- [[policies/customer-service/return-window|Return window]] — A customer can request a return within 14 days of delivery; made-to-order items are excluded from change-of-mind returns. · 담당 store-operations
  - 별칭: returns policy, refund window
  - 관계: applies_to → [[concepts/change-of-mind-return|Change-of-mind return]]

## 관계 목록

| 출발 | 관계 | 대상 | 근거 |
|---|---|---|---|
| [[policies/customer-service/return-window]] | applies_to | [[concepts/change-of-mind-return]] | `^[/documents/returns.md:3]` |
