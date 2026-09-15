# Navigation reference (on-demand)

> Not auto-loaded. Read when you need the doc map, instruction hierarchy, or agent/skill guide
> beyond the root `CLAUDE.md` index.

## Documentation map

- `README.md`, repo root: canonical protocol/config/API reference. Check before re-deriving usage
  examples.
- `doc.go`, repo root: package-level godoc (protocol format, quick start).
- `ROADMAP.md`, repo root: planned work.
- `llms-full.txt`, repo root: AI-agent reference dump of the project.
- `docs-site/docs/` + `mkdocs.yml`: mkdocs site source, served via `make docs`.
- `spec/spec-architecture-room-manager.md`: design reference `roommanager/` was built against.
- `.claude/harness/profile.md`: project profile consumed by `/pr` and `/ticket`.
- `.claude/repo-index/*.md`: deep per-area indexes, one per system-topology area, read on demand.

## Agent instruction hierarchy

Cumulative, narrowest scope wins on conflict:

1. `~/.claude/CLAUDE.md` (user's global working agreements, all projects).
2. `~/.claude/RTK.md`, `~/.claude/rules/context7.md` (user's global tool rules).
3. Repo root `CLAUDE.md` (this repo's conventions, commit format, JS client version policy).
4. Per-area `CLAUDE.md` (none exist yet in this repo; when added, they win over the root file for
   their own directory).

## Agents, workflows, skills guide

Per user's global working agreement, this repo runs a single main loop by default: solve tasks
directly, do not delegate to subagents or workflows unless explicitly asked in the moment. The
read-only advisor roster below exists for that explicit-ask case, not for routine use.

**Read-only advisors** (consult, never edit): `developer-reviewer` (correctness/tests),
`design-principles-advisor` (SOLID/GRASP/coupling), `principles-engineer` (DRY/reuse sizing),
`performance-optimizer`, `security-auditor`, `compatibility-auditor` (consumer breakage, relevant
here for `roommanager`/`syncvar` wire changes against `clients/js`), `system-architect` (placement),
`system-designer` (signatures/shapes), `spec-fidelity-auditor`, `docs-researcher`.

**Key skills:** `/pr` (PR body from `.claude/harness/profile.md`), `/ticket` (ticket body, same
profile), `/harness-init --refresh` (regenerate this navigation harness after structural change),
`code-review`, `golang-security`, `go-concurrency-patterns` (Go-specific), `humanizer`.

## Orchestration map (only when explicitly asked to orchestrate)

- **PLAN gate** (before writing code, for a non-trivial change): `system-architect` for placement,
  `system-designer` for exact shapes, `security-auditor` and `compatibility-auditor` in parallel
  when the change touches `roommanager/`, `syncvar/`, or the wire protocol, given the JS client
  mirror requirement documented in `.claude/repo-index/room.md` and
  `.claude/repo-index/syncvar.md`.
- **VERIFY gate** (after the diff exists): `developer-reviewer` and `compatibility-auditor` in
  parallel always; add `performance-optimizer` for `timing/`/`clock/` or hot-path changes, and
  `design-principles-advisor` / `principles-engineer` for larger refactors.
- Advisors run read-only and in parallel within a gate; the main loop synthesizes their findings
  and owns every edit.
