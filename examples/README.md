# Building a fictional store wiki

**English** · [한국어](README.ko.md)

The sources are two short, fictional notes about a store's returns policy. English versions are in [`raw-en/`](raw-en/) and the Korean originals are in [`raw/`](raw/). Both say the same thing line for line, so citations use the same line numbers.

## Try it yourself

In the README start prompt, set the project name to "Fictional store" and the docs folder to `examples/raw-en` (or `examples/raw` for Korean). For the first step, name only `returns.md` as the target. Set project history, past sessions, and memory to none.

The agent should not write a summary page for the note. It should put the adopted rule into a policy page (with a line citation next to each rule), put the definition it relies on into a concept page, and ask you who owns them. Then ask:

> Make a change proposal to add examples/raw-en/policy-change.md to the wiki. Also show the conditions that differ from the existing policy.

Applying the new policy's 30 days to every order would be a wrong interpretation. The 14-day/30-day split by order date and the made-to-order exception must be kept. Two policies that differ in timing and conditions are not a contradiction: the existing page is updated and Git keeps the old version.

Question after applying:

> What is the return window for an order placed in January and for one placed in February? Give the sources too.

The expected answer is 14 days and 30 days, the exception condition, and a citation to the source for each.

## What the output looks like

[`wiki-en/`](wiki-en/) holds only adopted knowledge. [`wiki-en-workspace/`](wiki-en-workspace/) is the workspace (normally the hidden `.llm-wiki/`), kept visible here so you can browse it.

| Path | What it is |
|---|---|
| [`wiki-en/policies/customer-service/return-window.md`](wiki-en/policies/customer-service/return-window.md) | Policy: 14 days from delivery, made-to-order exclusion, each with `^[/documents/returns.md:2]`-style citations, an owner, and an `answers` question for the index |
| [`wiki-en/concepts/change-of-mind-return.md`](wiki-en/concepts/change-of-mind-return.md) | Concept the policy applies to, linked with a typed `applies_to` relation and its evidence |
| [`wiki-en/index.md`](wiki-en/index.md) | Generated entry point: questions → pages, pages by folder with owner, aliases and relations, relation table |
| [`wiki-en/SCHEMA.md`](wiki-en/SCHEMA.md) | Rules copied by setup |
| [`wiki-en/.verification/registry.json`](wiki-en/.verification/registry.json) | Hash of each cited line at adoption time. If `returns.md` changes, lint and `company-wiki registry` flag the pages that cite it |
| [`wiki-en-workspace/reviews/2026-10-08-policy-change/`](wiki-en-workspace/reviews/2026-10-08-policy-change/) | Pending change proposal for `policy-change.md`, prepared with `company-wiki review prepare` |

Inside the proposal, `working/policy-change/card.json` has the judgments (fact, scope, relationship) and `topics/policy-change/r0001/review.md` is the frozen version with its diff. The agent would ask one question, for example:

1. Update the return window from "14 days for all orders" to "14 days for orders placed up to 2026-01-31, 30 days from 2026-02-01, made-to-order exclusion unchanged"? (Recommended: apply / revise / defer / exclude)

There is no `decisions.jsonl` or `applied.jsonl` here, because a decision must quote a real user message from a session file. In a real workspace the owner answers that question in the session, `company-wiki review decide` records the answer, `apply` writes the page, and you commit and push it to the wiki repository's main.

To check the example yourself, from the repository root:

```bash
WIKI_CONFIG=examples/wiki-en.toml company-wiki lint
WIKI_CONFIG=examples/wiki-en.toml company-wiki registry --all
WIKI_CONFIG=examples/wiki-en.toml company-wiki review preview 2026-10-08-policy-change policy-change
```
