# Return window split by order date

The policy change adds a 30-day window for orders from 2026-02-01 and keeps 14 days for earlier orders and the made-to-order exclusion.

검토 버전: `b90b112d867e4d1b05ecf3e8475eaafeb805897444eb45416a13d5bb56345dd4`

승인 / 수정 요청 / 보류 / 제외 중 선택. 승인 범위는 아래 변경 전후와 판단 내용입니다.

## 1. fact

- 이전: 14 days from delivery for all orders
- 제안: 30 days from delivery for orders placed from 2026-02-01
- 이유: The policy change meeting sets the new window and its start date.
- 근거: ^[/documents/policy-change.md:2]

## 2. scope

- 이전: No order-date condition
- 제안: Orders placed up to 2026-01-31 keep 14 days; the made-to-order exclusion stays for both
- 이유: Applying 30 days to every order would widen the change beyond what the source says.
- 근거: ^[/documents/policy-change.md:3] · ^[/documents/policy-change.md:4]

## 3. relationship

- 이전: The two documents are unrelated in the wiki
- 제안: Update the existing policy page instead of marking a contradiction
- 이유: The windows differ by order date, so they do not conflict for the same order; Git keeps the old version.
- 근거: ^[/documents/returns.md:2] · ^[/documents/policy-change.md:3]

## policies/customer-service/return-window.md

```diff
--- before/policies/customer-service/return-window.md
+++ after/policies/customer-service/return-window.md
@@ -1,14 +1,15 @@
 ---
 type: policy
 title: Return window
-description: A customer can request a return within 14 days of delivery; made-to-order items are excluded from change-of-mind returns.
+description: Orders placed up to 2026-01-31 can be returned within 14 days of delivery, orders from 2026-02-01 within 30 days; made-to-order items are excluded from change-of-mind returns.
 owner: store-operations
 sources:
   - ^[/documents/returns.md:2-3]
+  - ^[/documents/policy-change.md:2-4]
 related:
   - target: concepts/change-of-mind-return
     type: applies_to
-    evidence: ^[/documents/returns.md:3]
+    evidence: ^[/documents/policy-change.md:4]
 answers: [How long does a customer have to request a return?]
 aliases: [returns policy, refund window]
 ---
@@ -16,9 +17,11 @@
 
 ## Rule
 
-A return can be requested within 14 days of delivery.^[/documents/returns.md:2] This is the policy dated 2026-01-01.^[/documents/returns.md:2]
+The window depends on when the order was placed:
+
+- Orders placed up to 2026-01-31: 14 days from delivery.^[/documents/returns.md:2] ^[/documents/policy-change.md:3]
+- Orders placed from 2026-02-01: 30 days from delivery.^[/documents/policy-change.md:2]
 
 ## Exceptions
 
-Made-to-order items cannot be returned for a change of mind.^[/documents/returns.md:3] See [[concepts/change-of-mind-return]].
-
+Made-to-order items cannot be returned for a change of mind, under both windows.^[/documents/returns.md:3] ^[/documents/policy-change.md:4] See [[concepts/change-of-mind-return]].
```
