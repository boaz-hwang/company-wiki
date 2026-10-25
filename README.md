<h1 align="center">Company Wiki</h1>

<p align="center">
  <strong>An LLM wiki that holds only what your organization has adopted: concepts, entities, and policies, each traceable to its source.</strong>
</p>

<p align="center">
  <strong>English</strong> · <a href="README.ko.md">한국어</a>
</p>

---

Claude Code or Codex reads your docs and AI session records, finds the knowledge your organization has actually adopted, and writes it as linked Markdown. Every rule and fact carries a `^[file:line]` citation next to it. The page owner approves each change, Git keeps the history, and the `company-wiki` CLI checks fields, links, relations, citations, and whether the cited source lines have changed since adoption.

Company Wiki is a restructured fork of [BOAZ Wiki](https://github.com/boaz-hwang/boaz-wiki). BOAZ Wiki also keeps source summaries, overviews, open questions, research notes, and review records inside the wiki. Company Wiki narrows the wiki to adopted knowledge and moves everything else out.

## What the wiki holds

```text
llm-wiki/                  adopted knowledge only (your Git repository or a folder)
├── SCHEMA.md              rules for this wiki
├── .verification/         citation checks, not knowledge (hidden in Obsidian)
│   └── registry.json      cited-line hashes at adoption time, committed so the team and CI share them
├── index.md               generated entry point
├── concepts/              what things mean: concepts, definitions, principles, models
├── entities/              what exists: identity, attributes, stable relations
│   ├── products/
│   └── organizations/
└── policies/              how we decide and act: strategy, rules, criteria
    ├── product/
    └── engineering/

.llm-wiki/                 workspace, not knowledge (hidden in Obsidian)
└── reviews/<batch>/       drafts, review versions, decisions, receipts, inventory
```

Subfolders are free; the top-level folder sets the page type, so you do not add new top-level folders as the organization grows.

| BOAZ Wiki | Company Wiki | Why |
|---|---|---|
| `sources/` | removed | citations sit next to the conclusions; originals stay where they are |
| `overviews/` | removed | relations live in `related`; the index lists them and the agent synthesizes at query time |
| `questions/` | moved out | open questions are not adopted knowledge; reusable questions become `answers` on the page |
| `research/` | moved out | research is the stage before adoption; adopted results go into concepts or policies |
| `reviews/`, `log.md` | moved to the workspace / Git | review records and history are not knowledge |
| `status`, `verified` | removed | being on the main branch means adopted; Git keeps history |
| — | `policies/` added | the criteria the organization currently applies |

`entities/` is not a copy of your CRM, HR, or product database. It holds the adopted definition of each entity and its stable relations, and points to the system of record for values that change.

## Page format

```yaml
---
type: policy
title: Training recommendation policy
description: Criteria applied when recommending training to athletes
owner: product-owner
sources:
  - ^[/documents/training.md:12-20]
related:
  - concepts/training-load
  - target: entities/products/plco      # optional typed relation, like Toss's typed links
    type: applies_to
    evidence: ^[/documents/training.md:15]
answers: [How is training intensity chosen?]   # optional, listed in the index
aliases: [training guide]                      # optional
---
```

Required: `type`, `title`, `description`, `owner`, `sources`. Relation types: `part_of`, `depends_on`, `implements`, `applies_to`, `supersedes`, `contradicts`, `related_to`. See [SCHEMA.md](SCHEMA.md) and [templates/page.md.txt](templates/page.md.txt).

## Getting started

You need Git and Claude Code or Codex with access to your local files. Runs on macOS, Linux, and Windows. Obsidian is optional.

Install the `company-wiki` binary. It is a single file with no runtime to install:

```bash
curl -fsSL https://raw.githubusercontent.com/boaz-hwang/company-wiki/main/install.sh | sh          # macOS, Linux
```

```powershell
irm https://raw.githubusercontent.com/boaz-hwang/company-wiki/main/install.ps1 | iex              # Windows PowerShell
```

The script downloads the release for your OS and CPU from [GitHub Releases](https://github.com/boaz-hwang/company-wiki/releases) into `~/.local/bin` (Windows: `%LOCALAPPDATA%\Programs\company-wiki`, added to your user PATH). Check with `company-wiki version`. To build from source instead: `go install github.com/boaz-hwang/company-wiki/cmd/company-wiki@latest`.

To set up a workspace yourself, run setup in a terminal inside the workspace folder. It shows a checklist of source kinds: move with ↑↓, toggle with space, confirm with enter, then type each path. Paths that do not exist are rejected.

```bash
cd /path/to/workspace && company-wiki setup
```

Agents cannot answer terminal prompts, so they ask you which sources and paths to register and pass them as options (`company-wiki setup --list-kinds` shows the kinds). Run with `--source` again later to add sources to an existing config.

```bash
company-wiki setup --source documents=../docs --source slack=../exports/slack \
  --source code:server=../server --source claude=~/.claude/projects/-path-to-project
```

Or open the workspace folder in your coding agent, fill in the brackets, and paste:

```text
I want to build an LLM wiki of the knowledge [organization or team] has adopted.

Follow the company-wiki README (https://github.com/boaz-hwang/company-wiki) and the
SCHEMA and skills that `company-wiki setup` installs.

- Docs and project paths: [...]
- Slack, Jira, Notion, Confluence: [export folders and spaces/projects in scope, or none]
- Related sessions and memory: [location, or check with me]
- Wiki location and Git repository: [path, or check with me]
- Owners by area: [e.g. product: product-owner, or check with me]
- Material to exclude: [none, or the scope]

Look at the current docs and vault structure first. From the given sources, put
only what we have adopted into concepts, entities, and policies, with a line
citation next to each rule and fact. Do not write source summaries or overview
pages. Report open questions, research in progress, and unadopted claims
separately instead of putting them in the wiki.

Record what you investigated and what remains unverified, and show me the
proposed changes and the decisions I need to make, all at once. Apply them
through PRs (or company-wiki review if I approve here), and set things up so I can update,
review, and query the wiki with this repository's skills.
```

## Approving changes

- **Git PR (default):** draft in the workspace → on the branch, `company-wiki registry record` updates `.verification/registry.json` → PR to the wiki repository → owner approves → merge. Owners may also edit their own pages directly in Obsidian; they still run lint and `record <page>` and commit. Copy [templates/wiki-pr-check.yml](templates/wiki-pr-check.yml) into the wiki repository to run lint on every PR.
- **In-session (`company-wiki review`):** `prepare → preview → decide → apply`. The decision must quote a real user message from a Claude Code or Codex session. Versions, decisions, and receipts stay in the workspace; apply writes only the approved pages and the index, and updates the registry. See [workflow.md](skills/wiki-review/references/workflow.md).

## Source change detection

Inspired by [Toss's typed, evidence-backed context relations](https://toss.tech/article/llm_context_topic), each cited line range is hashed when it is adopted. The wiki keeps no copies of originals.

```bash
company-wiki registry                         # cited lines that changed, moved, expired, or are unrecorded, plus affected pages
company-wiki registry --source /documents/x.md # which pages depend on this source
company-wiki registry --page concepts/x        # which pages are affected if this page changes
company-wiki registry record concepts/x       # re-record after you re-checked a page
```

`changed` means re-check the sentences and relations that cite those lines. `moved` means the content is the same but sits on other lines; fix the line numbers. Affected pages are found by following `depends_on`, `implements`, and `applies_to` relations backwards, transitively. `expired` means a session file was cleaned up (Claude Code keeps sessions for `cleanupPeriodDays`), so the evidence can no longer be re-checked; raise the retention or cite a result document instead. lint reports these as warnings; a missing document is an error.

The agent detects problems, tells the owner, and proposes fixes; it never invalidates or replaces adopted knowledge. Until the owner approves a change, the existing page stays the standard and the agent states the risk it found. It does not carry out actions that break legal, security, or safety constraints (SCHEMA section 7).

## After the first build

| What you want | Example request | Skill |
|---|---|---|
| Bring everything up to date | "Compare docs and sessions since the last review and update the wiki." | [wiki-update](skills/wiki-update/SKILL.md) |
| Add specific material | "Put the decisions from this meeting note into the wiki." | [wiki-ingest](skills/wiki-ingest/SKILL.md) |
| Review, revise, apply | "Apply proposals 1 and 2, and revise 3 with this wording." | [wiki-review](skills/wiki-review/SKILL.md) |
| Answers with evidence | "Based on the wiki, what is our code review rule?" | [wiki-query](skills/wiki-query/SKILL.md) |

Setup copies the 4 skills bundled in the binary into the project's `.claude/skills/` and `.agents/skills/`. Running it again after an upgrade refreshes those copies and keeps skills you edited.

## Configuration

Setup writes `wiki.toml` with the sources you selected; [wiki.example.toml](wiki.example.toml) shows every option. `[wiki] path` is the wiki folder (default `llm-wiki`), `state` is the workspace (default `.llm-wiki`); they must be separate. Register each source with its path and kind. Relative paths resolve from the config file. Select another workspace with `WIKI_CONFIG=/path/to/wiki.toml`.

- **Kinds:** `documents`, `code`, `slack`, `jira`, `notion`, `confluence`, `claude`, `codex`. Personal collections such as web clippings are not sources.
- **Docs and code:** file/line citations for Markdown, MDX, and source files. Convert other formats to readable text first.
- **Slack, Jira, Notion, Confluence:** export only the needed content as text into the registered folder, with the original URL/ID, and cite that. There are no dedicated connectors; the agent finds originals through whatever connectors or MCP it has and re-checks them during update.
- **Sessions:** roles and original line numbers in Claude Code and Codex JSONL, and verification of actual user replies.
- **Memory, Git, other agents:** read as leads to originals and user statements.
- **Source scope:** include/exclude are instructions for the agent, not a sandbox.

The CLI does not call an LLM. A passing check does not prove the content is right; the owner's approval does that. Local `wiki.toml`, `llm-wiki/`, and `.llm-wiki/` are git-ignored in this tool repository. Keep your wiki in its own repository and decide its visibility first.

Commands find `wiki.toml` from the current folder upward; `WIKI_CONFIG=/path/to/wiki.toml` selects another. On Windows, write paths in `wiki.toml` with single quotes.

## Example and checks

The [fictional returns policy example](examples/README.md) has a policy, a concept, a typed relation, a registry, and a pending change proposal.

```bash
WIKI_CONFIG=examples/wiki-en.toml company-wiki lint
WIKI_CONFIG=examples/wiki-en.toml company-wiki registry
go test ./...      # tool development
```

## License

Based on [BOAZ Wiki](https://github.com/boaz-hwang/boaz-wiki), which drew on [Open Knowledge Format](https://github.com/GoogleCloudPlatform/open-knowledge-format) and [OpenWiki](https://github.com/langchain-ai/openwiki) for ideas but contains no code from them. Code, skills, docs, and fictional examples are under the [MIT License](LICENSE).
