# Design: agents and APM

Status: decisions agreed (2026-10-11); implementation not started. Epic: XXX-346.

## Problem

Agent state is declared in three places that omni only partly understands:

- the host template `~/.config/omni/apm.yml` (a dotfile, copied whole over the live manifest by sync);
- local APM packages it depends on (for example `~/Dev/dotfiles/apm/ai-plugins/apm.yml`), which hold most
  real dependencies, MCP servers and LSP servers;
- `# apm marketplace add …` comments in the template for marketplaces, since `apm.yml` cannot express them.

Omni never edits manifest fields; it copies files and shells out to `apm`. The result:

| Symptom | Issue | Cause |
|---|---|---|
| update fails for nested dependencies | XXX-324 | `apm update <spec>` only resolves root-manifest specs; omni does not know where a nested dependency is declared |
| unclear versions | XXX-325 | rows mix manifest ref, lock version and commit; `resolved_ref` and `depth` are not read |
| marketplaces mishandled | XXX-330 | comments are add-only and outside apm's model |
| MCP reaches unwanted clients, duplicate tables | XXX-341, XXX-342 | apm has no per-entry MCP targets and merges into client configs it does not own |
| no agents step in onboarding | XXX-335 | there is no omni-side model to onboard into |
| agent configs drift | XXX-343 | apm rewrites client config files that dotfiles also ship |

## Current state (omni at settings schema v26, apm 0.34.0)

- Settings hold only `agents.ignored[]`; any other `agents` key is rejected as retired
  (`internal/config/agents_block.go`).
- Sync: template (or live manifest) → whole-file write to `~/.apm/apm.yml`, guarded by a hash of the last
  synced manifest (`--force-template` overrides) → `apm marketplace add` for missing comment entries →
  `apm install -g --trust-transitive-mcp` → snapshot hash (`internal/app/agents_sync_all.go`).
- Rows come from manifest and lock (`internal/app/agents_status.go`); nested dependencies appear through
  `resolved_by` but without their ref.
- Update/uninstall pass a spec to `apm update|uninstall -g`; pinned and local rows are refused.
- MCP/LSP rows are read-only; editing them means editing apm.yml by hand.
- Writes outside `~/.apm`: hook path rewriting in `~/.claude/settings.json` and `~/.claude/apm-hooks.json`.
  apm itself writes `~/.claude.json`, `~/.codex/config.toml`, hooks and skills.

## Goals

1. One declaration per dependency, MCP server, LSP server and marketplace, in a place omni can read and
   edit.
2. Every agents action (add, update, pin, remove, retarget) is an edit of that declaration followed by
   sync, so CLI, TUI and onboarding share one code path.
3. Status shows declared ref, installed version and available update for every row, nested or not.
4. Omni controls which client receives which MCP server, and never leaves a client config invalid.
5. Declarations are portable across hosts (no absolute paths, `~` and `${env:…}` only).

## Non-goals

- Replacing apm's resolver, lockfile or deployment.
- Managing client settings that apm does not touch.

## Target model

Desired agent state lives in omni settings, in `settings.d/agents.json` (the fragment already exists in
`extractFragments`). Omni renders the apm manifest from it on every sync and treats `~/.apm/apm.yml` as
generated output.

```jsonc
{
  "agents": {
    "targets": ["claude", "codex"],
    "packages": [
      { "source": "lkshrk/linear-ai", "ref": "v2.0.0" },
      { "source": "anthropics/claude-plugins-official", "path": "plugins/code-simplifier", "targets": ["claude"] },
      { "source": "obra/superpowers", "ref": "b36e0829…", "targets": ["claude"] }
    ],
    "mcp": [
      { "name": "litellm-tools", "transport": "http", "url": "https://api.ai.h-cloud.lan/mcp/",
        "targets": ["claude", "codex"],
        "client": { "claude": { "headersHelper": "~/.config/env/bin/litellm-mcp-headers" },
                    "codex":  { "http_headers_helper": "~/.config/env/bin/litellm-mcp-headers", "required": true } } }
    ],
    "lsp": [ { "name": "gopls", "command": "gopls", "extensionToLanguage": { ".go": "go" } } ],
    "marketplaces": [ { "name": "caveman", "source": "JuliusBrussee/caveman" } ],
    "ignored": [ /* unchanged */ ]
  }
}
```

Groups list agent entries by name, exactly as they list tools (`{"name": "ai-plugins", "tools": [...], "agents": ["linear-ai", "litellm-tools"]}`);
hosts select groups. The rendered manifest contains the union for the current host. This replaces the
per-host template files and `docs/concepts.md`'s "groups do not assign agent resources" rule.

### Sync

1. Merge settings layers → `agents` DTO.
2. Reconcile marketplaces: add missing, remove ones omni added that are no longer declared.
3. Render `~/.apm/apm.yml` from the DTO (packages and LSP only), compare with the last rendered
   hash; on drift offer to adopt or overwrite (D6).
4. `apm install -g --trust-transitive-mcp`.
5. Write MCP entries per client from the DTO (D3); MCP is not part of the rendered manifest.
6. Validate written client configs (TOML/JSON parse); on failure restore the previous file and report.
7. Snapshot.

### Actions

| Action | Edit | Then |
|---|---|---|
| add | append to `packages` | sync |
| update | move `ref` per D5 (tag → newest tag, commit → newest commit) | sync |
| pin / unpin | set or clear `ref` | sync |
| remove | delete the entry | sync (apm prunes) |
| retarget | set `targets` | sync |

## Decisions

- **D1 — Local APM packages (decided).** Flatten: everything is declared in omni settings. Agent
  resources become a resource kind like tools and dots: declared in the `settings.d/agents.json` fragment,
  bundled by groups (`"agents": [...]` next to `"tools"`), assigned to hosts through groups. Local `path:`
  packages such as `apm/ai-plugins` are retired after migration.
- **D2 — Where the settings live (decided by D1).** `settings.d/agents.json` for declarations, groups for
  bundling, hosts for selection; no separate template file per host.
- **D3 — MCP ownership (decided).** Omni writes MCP. MCP stays out of the rendered `apm.yml`; omni writes
  each declared server into the clients listed in its `targets`, with the per-client fields from `client`,
  edits only entries it owns (names recorded in omni state), leaves client-owned entries alone, and parses
  the file after writing (restore on failure). Removing a declaration removes only omni-owned entries.
- **D4 — Migration (decided).** Repurpose `omni agents migrate`: read the host template, follow local
  `path:` packages, parse marketplace comments, and write `settings.d/agents.json` plus group `agents`
  entries. Preview by default, `--write` applies. The pre-APM snapshot migration is dropped.
- **D5 — Update semantics (decided).** Keep the ref style: a tag ref moves to the newest semver tag (a
  major bump is flagged and needs confirmation); a commit ref moves to the newest commit on the default
  branch; an unpinned entry is refreshed by sync. `update all` applies the same rule per entry.
- **D6 — Hand edits to `~/.apm/apm.yml` (decided).** Offer to adopt. Sync compares the live manifest
  with the last rendered hash; on drift it shows the diff and offers to adopt new entries into
  `agents.json` or to overwrite. Non-interactive runs refuse unless `--adopt` or `--force` is given.

## Rollout

1. Read side: model `resolved_ref` and `depth`; show declared vs installed (XXX-325).
2. Settings schema v26 with the `agents` block; renderer; migration.
3. Actions on the new model (XXX-324); TUI and CLI share them.
4. MCP per client (XXX-341, XXX-342), config validation.
5. Marketplaces (XXX-330); onboarding step (XXX-335).
6. Retire the host template path and `omni-apm@*` packages in dotfiles.
