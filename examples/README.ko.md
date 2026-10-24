# 가상 상점 위키 만들기

[English](README.md) · **한국어**

README 시작 프롬프트에서 프로젝트 이름을 “가상 상점”, 문서 폴더를 `examples/raw`로 지정한다. 첫 단계는 `returns.md`만 대상으로 명시한다. 프로젝트 이력·과거 세션·메모리는 없음으로 지정한다.

에이전트는 원문 요약 페이지를 만들지 않는다. 채택된 규칙은 policy 페이지에 규칙마다 줄 인용을 붙여 넣고, 그 규칙이 기대는 정의는 concept 페이지에 넣은 뒤 담당자(owner)를 묻는다. 그 뒤 다음처럼 요청한다.

> examples/raw/policy-change.md를 위키에 반영할 변경안으로 만들어줘. 기존 정책과 달라지는 조건도 보여줘.

새 정책의 30일을 모든 주문에 적용하면 잘못된 해석이다. 주문 접수일에 따른 14일/30일 구분과 주문 제작 예외를 유지해야 한다. 시점·조건이 다른 두 정책은 모순이 아니다. 기존 페이지를 고치고 이전 버전은 Git이 보관한다.

반영 후 질문:

> 1월에 접수한 주문과 2월에 접수한 주문의 반품 가능 기간은 각각 얼마야? 근거도 알려줘.

기대 답은 14일/30일과 예외 조건, 각 원문 인용이다.

## 결과물

[`wiki-en/`](wiki-en/)에는 채택된 지식만 있다. [`wiki-en-workspace/`](wiki-en-workspace/)는 작업 공간(보통 숨김 폴더 `.llm-wiki/`)인데, 예제에서는 볼 수 있게 드러내 두었다.

| 경로 | 내용 |
|---|---|
| `wiki-en/policies/customer-service/return-window.md` | 정책: 배송 후 14일, 주문 제작 예외. 규칙마다 원문 줄 인용, 담당자, index용 `answers` 질문 |
| `wiki-en/concepts/change-of-mind-return.md` | 정책이 적용되는 개념. 근거가 붙은 `applies_to` 관계로 연결 |
| `wiki-en/index.md` | 생성된 입구: 질문 → 페이지, 폴더별 목록(담당자·별칭·관계), 관계 표 |
| `wiki-en-workspace/registry.json` | 채택 시점의 인용 줄 해시. `returns.md`가 바뀌면 lint와 `registry.py`가 그 원문을 인용한 페이지를 알린다 |
| `wiki-en-workspace/reviews/2026-10-08-policy-change/` | `policy-change.md` 변경안. `review.py prepare`로 고정했고 결정 대기 중 |

에이전트가 할 질문 예시:

1. 반품 기간을 “모든 주문 14일”에서 “2026-01-31까지 접수한 주문 14일, 2026-02-01부터 30일, 주문 제작 예외 유지”로 바꿀까요? (추천: 반영 / 수정 / 보류 / 제외)

결정은 실제 세션의 사용자 메시지를 인용해야 하므로 예제에는 decisions.jsonl·applied.jsonl이 없다. Git으로 관리하는 위키라면 이 변경을 PR로 올리고 담당자가 승인한다.

저장소 루트에서 직접 확인:

```bash
WIKI_CONFIG=examples/wiki-en.toml python3 -B scripts/lint.py
WIKI_CONFIG=examples/wiki-en.toml python3 -B scripts/registry.py --all
WIKI_CONFIG=examples/wiki-en.toml python3 -B scripts/review.py preview 2026-10-08-policy-change policy-change
```
