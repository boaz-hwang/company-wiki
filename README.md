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
~/company-wiki/                one per machine, fixed (Windows: %USERPROFILE%\company-wiki)
├── wiki.toml                  sources and reviewer
├── llm-wiki/                  the wiki: adopted knowledge only (a shared Git repository)
│   ├── SCHEMA.md              rules for this wiki
│   ├── .verification/         citation checks, not knowledge (hidden in Obsidian)
│   │   └── registry.json      cited-line hashes at adoption time, committed so the team and CI share them
│   ├── index.md               generated entry point
│   ├── concepts/              what things mean: concepts, definitions, principles, models
│   ├── entities/              what exists: identity, attributes, stable relations
│   │   ├── products/
│   │   └── organizations/
│   └── policies/              how we decide and act: strategy, rules, criteria
│       ├── product/
│       └── engineering/
└── workspace/                 your own vault: an existing folder, a link to your Obsidian vault, or a new folder
    └── wiki-changes/          change materials, not knowledge: drafts, review versions, decisions, receipts, inventory
```

Change materials live outside the wiki repository, so undecided material cannot be committed into the wiki by mistake; apply copies only the decided version into `llm-wiki/`.

Subfolders are free; the top-level folder sets the page type, so you do not add new top-level folders as the organization grows.

| BOAZ Wiki | Company Wiki | Why |
|---|---|---|
| `sources/` | removed | citations sit next to the conclusions; originals stay where they are |
| `overviews/` | removed | relations live in `related`; the index lists them and the agent synthesizes at query time |
| `questions/` | moved out | open questions are not adopted knowledge; reusable questions become `answers` on the page |
| `research/` | moved out | research is the stage before adoption; adopted results go into concepts or policies |
| `reviews/`, `log.md` | moved to `workspace/wiki-changes/` / Git | review records and history are not knowledge |
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

Run setup from any folder. It always prepares `~/company-wiki` (Windows: `%USERPROFILE%\company-wiki`) and never replaces existing files. In a terminal it shows a checklist of source kinds: move with ↑↓, toggle with space, confirm with enter, then type each path. Paths that do not exist are rejected.

```bash
company-wiki setup
```

Agents cannot answer terminal prompts, so they ask you which sources and paths to register and pass them as options (`company-wiki setup --list-kinds` shows the kinds). Relative `--source` paths are from the current folder and are saved as absolute paths (`~/…` under your home). Run with `--source` again later to add sources; a source already registered with the same kind and path is skipped.

```bash
cd ~/projects && company-wiki setup --source documents=docs --source slack=exports/slack \
  --source code:server=server --source claude=~/.claude/projects/-path-to-project
```

### Onboarding

1. Install the binary (above). To join an existing team wiki, clone its repository to `~/company-wiki/llm-wiki` first; setup keeps the files that are there.
2. Run `company-wiki setup --source …`. It creates `wiki.toml`, `llm-wiki/` (folders, SCHEMA.md, index), `workspace/wiki-changes/`, the skills, and `AGENTS.md` under `~/company-wiki`.
3. `workspace/` is your own vault, set up without questions: an existing `workspace` folder or link is kept; otherwise it links to `--vault <folder>`, else to your Obsidian vault found in Obsidian's `obsidian.json` (the open vault, else the most recently used; vaults holding a `wiki.toml` or inside `~/company-wiki` are skipped), else an empty folder is created. Only `wiki-changes/` is added inside the vault; nothing in it is moved or changed. Setup prints which case applied. To change the vault, remove just the link (`rm ~/company-wiki/workspace`, Windows `rmdir`) and rerun with `--vault`.
4. Make `llm-wiki/` a Git repository if it is not one yet (`git -C ~/company-wiki/llm-wiki init`) and add the team remote.
5. Open `~/company-wiki` in Claude Code or Codex. Commands work from any folder.

Or open any folder in your coding agent, fill in the brackets, and paste:

```text
I want to build an LLM wiki of the knowledge [organization or team] has adopted.

Follow the company-wiki README (https://github.com/boaz-hwang/company-wiki) and the
SCHEMA and skills that `company-wiki setup` installs.

- Docs and project paths: [...]
- Slack, Jira, Notion, Confluence: [export folders and spaces/projects in scope, or none]
- Related sessions and memory: [location, or check with me]
- Wiki Git repository (cloned to ~/company-wiki/llm-wiki): [remote, or check with me]
- Owners by area: [e.g. product: product-owner, or check with me]
- Material to exclude: [none, or the scope]

Look at the current docs and vault structure first. From the given sources, put
only what we have adopted into concepts, entities, and policies, with a line
citation next to each rule and fact. Do not write source summaries or overview
pages. Report open questions, research in progress, and unadopted claims
separately instead of putting them in the wiki.

Record what you investigated and what remains unverified, and show me the
proposed changes and the decisions I need to make, all at once. Apply what I
approve with company-wiki review and commit it to the wiki repository's main (no
PRs), and set things up so I can update, review, and query the wiki with this
repository's skills.
```

## Approving changes

Changes go straight to the wiki repository's main branch; there are no PRs or branches.

- **Agent proposals (`company-wiki review`):** draft in `workspace/wiki-changes/` → `prepare → preview` → the owner answers in the session → `decide → apply` → commit and push to main. The decision must quote a real user message from a Claude Code or Codex session. Versions, decisions, and receipts stay in `workspace/wiki-changes/`; apply runs lint, writes only the approved pages and the index, updates the registry, and prints a commit message whose trailers (`Wiki-Change`, `Wiki-Revision`, `Approved-By`, `Approval-Evidence`) link the wiki commit to the decision. See [workflow.md](skills/wiki-review/references/workflow.md).
- **Owner edits:** owners may edit their own pages directly in Obsidian or an IDE: lint → `company-wiki registry record <page>` → commit and push to main.

Copy [templates/wiki-check.yml](templates/wiki-check.yml) into the wiki repository to run `company-wiki lint --wiki-only` on every push to main; it checks structure without the sources, which are not on the runner, and fails if change materials such as `card.json`, `decisions.jsonl` or `.md.txt` drafts were committed into the wiki. It detects problems after a push; checking before a change lands is the job of apply's lint and the owner's lint.

## Source change detection

Inspired by [Toss's typed, evidence-backed context relations](https://toss.tech/article/llm_context_topic), each cited line range is hashed when it is adopted. The wiki keeps no copies of originals.

```bash
company-wiki registry                         # cited lines that changed, moved, expired, or are unrecorded, plus affected pages
company-wiki registry --source /documents/x.md # which pages depend on this source
company-wiki registry --source ../docs/x.md    # the same, by the file's path inside a registered source
company-wiki registry --page concepts/x        # which pages are affected if this page changes
company-wiki registry record concepts/x       # re-record after you re-checked a page
```

`changed` means re-check the sentences and relations that cite those lines. `moved` means the content is the same but sits on other lines; fix the line numbers. Affected pages are found by following `depends_on`, `implements`, and `applies_to` relations backwards, transitively. `expired` means a session file was cleaned up (Claude Code keeps sessions for `cleanupPeriodDays`), so the evidence can no longer be re-checked; raise the retention or cite a result document instead. lint reports these as warnings; a missing document is an error. `--source` takes a cited form (`/alias/path`, or a `/path` from `~/company-wiki`) or a relative, absolute, or `~` file path; each is resolved to the file, and every citation of that file matches whatever its spelling. An uncited path outside every source is an error.

The agent detects problems, tells the owner, and proposes fixes; it never invalidates or replaces adopted knowledge. Until the owner approves a change, the existing page stays the standard and the agent states the risk it found. It does not carry out actions that break legal, security, or safety constraints (SCHEMA section 7).

## After the first build

| What you want | Example request | Skill |
|---|---|---|
| Bring everything up to date | "Compare docs and sessions since the last review and update the wiki." | [wiki-update](skills/wiki-update/SKILL.md) |
| Add specific material | "Put the decisions from this meeting note into the wiki." | [wiki-ingest](skills/wiki-ingest/SKILL.md) |
| Review, revise, apply | "Apply proposals 1 and 2, and revise 3 with this wording." | [wiki-review](skills/wiki-review/SKILL.md) |
| Answers with evidence | "Based on the wiki, what is our code review rule?" | [wiki-query](skills/wiki-query/SKILL.md) |

Setup copies the 4 skills bundled in the binary into `~/company-wiki/.claude/skills/` and `.agents/skills/`. Running it again after an upgrade refreshes those copies and keeps skills you edited.

## Configuration

Setup writes `wiki.toml` with the sources you selected; [wiki.example.toml](wiki.example.toml) shows every option. The layout is fixed: the wiki is always `llm-wiki/` and change materials are always `workspace/wiki-changes/` next to `wiki.toml`; there are no path settings for them (a `wiki.toml` that still has `[wiki] path` or `state` is rejected). Register each source with its path and kind. Relative source paths resolve from the config file.

- **Kinds:** `documents`, `code`, `slack`, `jira`, `notion`, `confluence`, `claude`, `codex`. Personal collections such as web clippings are not sources.
- **Docs and code:** file/line citations for Markdown, MDX, and source files. Convert other formats to readable text first.
- **Slack, Jira, Notion, Confluence:** export only the needed content as text into the registered folder, with the original URL/ID, and cite that. There are no dedicated connectors; the agent finds originals through whatever connectors or MCP it has and re-checks them during update.
- **Sessions:** roles and original line numbers in Claude Code and Codex JSONL, and verification of actual user replies.
- **Memory, Git, other agents:** read as leads to originals and user statements.
- **Source scope:** include/exclude are instructions for the agent, not a sandbox.

The CLI does not call an LLM. A passing check does not prove the content is right; the owner's approval does that. Keep the wiki (`llm-wiki/`) in its own repository and decide its visibility first; `wiki.toml` and `workspace/` stay on your machine.

Commands use the nearest `wiki.toml` from the current folder upward, else `~/company-wiki/wiki.toml`, so they work from any folder; to check another wiki.toml (such as the example), run the command in its folder. `WIKI_CONFIG` is no longer supported, and commands stop if it is set. On Windows, write paths in `wiki.toml` with single quotes.

## Example and checks

The [fictional returns policy example](examples/README.md) has a policy, a concept, a typed relation, a registry, and a pending change proposal.

```bash
(cd examples/en && company-wiki lint && company-wiki registry)
go test ./...      # tool development
```

## License

Based on [BOAZ Wiki](https://github.com/boaz-hwang/boaz-wiki), which drew on [Open Knowledge Format](https://github.com/GoogleCloudPlatform/open-knowledge-format) and [OpenWiki](https://github.com/langchain-ai/openwiki) for ideas but contains no code from them. Code, skills, docs, and fictional examples are under the [MIT License](LICENSE).
