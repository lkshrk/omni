# Agents

APM is the sole owner of steady-state agent packages, skills, MCP, plugins,
marketplaces, dependency locks, and deployed runtime state.

`omni agents sync` is the primary integration command. The remaining agent
commands are thin APM wrappers: `add`, `remove`, `update`, `search`, `audit`,
`targets`, `outdated`, `prune`, `deps`, and `marketplace`.

Sync passes `--trust-transitive-mcp` so MCP servers declared by dependencies of
a shared package deploy; the fleet's dependencies are pinned git refs and local
packages under the operator's dotfiles.

APM parks a package's native extensions, and its other executables when the
manifest declares an `executables` block, until the package is approved, and it
has no install flag for that. Every APM command Omni runs, from the CLI and the
TUI alike, goes through one function. When a global `apm install` or `apm update`
(not `--dry-run`) leaves packages `gated_pending_approval` in the lockfile, it
runs `apm approve --all --user` and repeats the same command so they deploy. That
covers `omni agents sync`, `add` and `update`, and the TUI's install, update and
sync actions. Packages denied with `apm deny` stay denied.
The grants live in `~/.apm/config.json`, which the template copy never touches,
so later syncs deploy without asking again. A grant covers every version of its
package: updating an approved package deploys the new code without review, the
same trust the fleet already extends to its pinned dependencies.

APM state:

- `~/.apm/apm.yml` — desired dependencies.
- `~/.apm/apm.lock.yaml` — resolved dependencies.
- `~/.apm/marketplaces.json` — marketplace metadata.

MCP is host-global across enabled APM targets that support user-global MCP,
Cursor (`~/.cursor/mcp.json`) and OpenCode (`~/.config/opencode/opencode.json`)
included.

Omni has no native steady-state agent implementation or parallel skill/plugin
store. Migration creates one verified content-addressed local APM wrapper for
every selected legacy owner. Those wrappers are APM inputs, not a second
deployment store. APM owns deployment, audit, and lifecycle state.

## Host template

A host may keep its desired manifest as a template at `omni/apm.yml` under the
OS user config directory — `~/.config/omni/apm.yml` on Linux. Sync copies that
template over `~/.apm/apm.yml` before invoking APM install, so the manifest is
a dotfile-managed input rather than something Omni edits field by field.

The template is optional. With no template file, sync installs the live
manifest unchanged.

`omni agents migrate --write` creates a migration-owned template whose first
line is exactly:

```yaml
# omni:agents-migration:v1
```

It can replace a missing template or regenerate a template whose first
non-empty line is that marker. It refuses to overwrite an unmarked, symlinked,
or non-regular template.

Because the template overwrites the live manifest, sync refuses to apply it
silently over work APM or the operator did directly:

- First sync with a template and an existing live manifest warns
  `first sync with a template: verify it matches the live manifest, then
  re-run with --force-template to adopt it`. Compare the two, then adopt the
  template with `omni agents sync --force-template`.
- After adoption, Omni records the live manifest's hash under
  `agents-template-state` in its state directory. A later sync that finds the
  live manifest changed outside Omni warns that it `diverged from last sync`
  and leaves it alone until the next `--force-template`.
- `--dry-run` never materializes the template.

`omni sync` runs the same template step but has no `--force-template` flag; run
`omni agents sync --force-template` for the adoption or override step.

## Hook commands stay host-independent

APM writes user-scope hook commands into `~/.claude/settings.json` with the
home directory expanded, so a dotfiles-tracked settings file would differ per
host. After every APM install, update, uninstall, or prune, Omni rewrites the
`<home>/.claude/hooks/` prefix in `~/.claude/settings.json` and APM's
`~/.claude/apm-hooks.json` ledger to `$HOME/.claude/hooks/`. Claude Code runs
hook commands through a shell, so `$HOME` expands at invocation time. APM adopts
the rewritten entries on the next run, so syncs stay idempotent and uninstalls
still remove the right hooks. Nothing is rewritten on Windows.

MCP server paths are the one case Omni cannot make portable. APM writes absolute
script paths into `~/.codex/config.toml` (`[mcp_servers.<name>] args =
["/Users/you/.apm/apm_modules/<owner>/<pkg>/start.mjs"]`), and they stay
absolute: Codex spawns stdio MCP servers directly rather than through a shell,
and `mcp_servers.<name>.command` and `args` are passed to the process exactly as
written. `$HOME`, `~/`, and `${env:HOME}` all survive into the child's argv as
literal text and the server fails to start, so rewriting them the way hook
commands are rewritten would break every MCP server instead of making it
portable. A dotfiles-tracked `~/.codex/config.toml` needs a per-host
`[mcp_servers.*]` section.

## omp MCP

The pinned Omni APM fork is based on upstream v0.35.0 and adds the explicit
`omp` target. Shared skills remain under `~/.agents/skills`; MCP deployment
uses the regular file `~/.omp/agent/mcp.json`. A nonempty
`PI_CODING_AGENT_DIR` overrides that directory (relative to the process working
directory, without tilde expansion). Named omp profiles are not selected
automatically.

Declare `targets:` on individual `dependencies.mcp` objects to restrict a
server to selected clients. An omitted list keeps all active clients; an
empty list deploys nowhere. APM records the per-client ownership and applies
the same selection during sync, frozen replay, audit, retargeting and removal.

For JSON-producing authentication helpers, declare `extra.headersHelper`
(or `extra.http_headers_helper`) and `extra.headersHelperKeys` with the header
names to extract. The omp adapter writes runtime `!command` references using
`jq -er`; installation never executes the helper or persists its credentials.
The helper and `jq` must be available when omp connects.

Track the manifest and credential-helper source, not the generated omp MCP
file. APM preserves unrelated servers, native envelope fields,
`disabledServers`/`enabledServers`, and per-server `instructions`/`enabled`
settings. It adopts equivalent entries and refuses different unmanaged
connections; resolve intentional connection changes explicitly before sync.
Omni's MCP inventory includes omp deployment evidence and native drift.

Verified APM-owned self-defined Codex connections retain undeclared native
tool settings, startup/tool timeouts, and additional environment keys during
updates. Declared fields remain ownership-checked; registry connections keep
strict native-config checks. Existing unmanaged Codex connections require
explicit verified adoption, not `--force`.

## omp extensions and hooks

The pinned APM fork supports two generic omp executable surfaces.

- **Native extensions** are first-class package content. Packages opt in with
  `package.json` `omp.extensions` or `pi.extensions`, or a designated
  `.apm/extensions`, `.omp/extensions`, or `.pi/extensions` directory. The
  `omp` object takes precedence over `pi`; APM does not run extension code,
  builds, package scripts, or npm installation while deploying it. Extensions
  require their own `executables.allow.<package-key>.extensions: true` approval;
  hooks and canvas approval do not grant it.
- **Portable command hooks** use the existing APM hook contract through an
  APM-owned native bridge. Supported events are PreToolUse, PostToolUse,
  PostToolUseFailure, Stop, initialization-only SessionStart, shutdown
  SessionEnd, PreCompact, and PostCompact. The bridge is POSIX-only and rejects
  unsupported semantics before writes: UserPromptSubmit, permission requests,
  subagent lifecycle, asynchronous/prompt/agent handlers, lifecycle matchers,
  `ask`, unsafe edit/task payloads, and Windows command hooks.

Native content takes precedence over portable hooks from the same package. If
the extension is unapproved, it stays parked and its portable omp hooks are not
kept as a fallback; that would execute updated package code without native-code
approval. Project and user bridges are separate:
`.omp/extensions/apm-hooks-project/` and
`~/.omp/agent/extensions/apm-hooks-user/` (or the configured
`PI_CODING_AGENT_DIR`). Both names are reserved against package collisions.

Native extensions may use arbitrary omp ExtensionAPI features. The portable
bridge is intentionally narrower: SessionStart is native initialization, not
resume/clear/compact; SessionEnd is shutdown; transcript paths retain omp's
native format. Use a native extension when a hook needs unsupported native tool
shapes or lifecycle semantics.

For an Omni-managed host template, commit organization approvals in the
template or use `apm approve --user <package>` for personal approval. Do not
edit only `~/.apm/apm.yml`: Omni sync replaces it from the template. A locked
package can be installed while its extension is parked pending approval.

Generated extensions, bridge state, hooks and MCP configuration are APM-owned
runtime output. Do not put them under dots. The `omp` dots entry declares only
`config.yml`, `models.yml`, and `AGENTS.md`; declaration neither adopts nor
syncs those files.

## Package-owned MCP and LSP

A package is authoritative for every MCP or LSP child declared by its installed
`apm.yml`. Omni compares package children with top-level `dependencies.mcp` and
`dependencies.lsp` entries by kind, case-insensitive name, and a canonical
fingerprint. Same-name entries are not enough to prove equivalence.

| State | Behavior |
| --- | --- |
| One package owner, no standalone entry | The child belongs to the package. |
| One owner and an identical standalone fingerprint | The standalone entry is an exact duplicate; `omni doctor --fix` can remove it from the canonical template. |
| One owner and a different standalone fingerprint | Sync blocks. Doctor reports the differing field names without values and preserves both declarations. |
| Multiple package owners | Sync blocks as ambiguous. Doctor never chooses an owner. |
| No package owner | The standalone or unmanaged child remains independent. |

Fingerprint comparison covers the supported command, arguments, working
directory, URL, transport, environment/header names, and LSP fields. It does
not expand environment values or print secret values. Package-relative paths
are normalized only when the installed package root proves their meaning.

Ownership evidence comes from installed package manifests under
`~/.apm/apm_modules/`. On a first install, a remote package may not have local
manifest evidence yet. A package-only template may proceed, but a template that
also declares standalone MCP/LSP entries blocks before the live manifest or APM
changes. Install the package without the standalone entries first, then add
only services the package does not provide.

An installed `apm.yml` remains the normal ownership source. Omni accepts a
missing manifest as proof that a package provides no MCP/LSP children only for
the pinned APM package types `claude_skill` and `skill_bundle`, and only when
all of these facts are proven:

- one installed lock entry identifies the package, with a resolved commit and
  a non-empty deployed-file inventory;
- every deployed file is under a skills root of the pinned APM target catalog,
  `<root>/<name>/` (`.agents/skills`, `.claude/skills`,
  `.config/opencode/skills`, `.gemini/antigravity-cli/skills`, `.grok/skills`,
  `.hermes/skills`, `.kiro/skills`, `.openclaw/skills` or `agent-data/workflows`);
- each inventoried skill has its canonical regular `SKILL.md` in the installed
  module; and
- `apm.yml` and every plugin, MCP, and LSP carrier recognized by APM are
  absent.

This recognizes `sopaco/deepwiki-rs` as a manifestless `skill_bundle` and the
`shiplight` virtual package from `ShiplightAI/agent-skills-v2` as a
manifestless `claude_skill`. Both have an empty `provides` list. A standalone
`shiplight` MCP remains an independent top-level service; matching a package
name never proves ownership.

Unknown, plugin, hybrid, mixed, malformed, unreadable, symlinked, incomplete,
or changed evidence remains unavailable and keeps the existing fail-closed
sync and Doctor behavior. This proof is Omni-side only: it does not change APM,
the TUI, update checks, package versions, or row layout.

`omni doctor` reports package-owned duplicates, conflicts, ambiguities, and
unavailable ownership evidence. `omni doctor --fix --dry-run` previews exact
duplicate removals without writing. `omni doctor --fix` edits only the
canonical host template, preserves a template symlink and its target mode, and
prints `omni agents sync` as the next step. It never edits the live manifest,
lockfile, installed module manifests, package cache, or client configuration.

The fixer locks the canonical template and then the global APM workspace. It
hashes the template, live manifest, lockfile, and installed module manifests
used for classification and rechecks them before atomic replacement. If any
input, symlink component, or target identity changes, repair refuses to write.

Automatic repair is deliberately narrow. It removes exact source-byte ranges
only from unambiguous block-style `dependencies.mcp` or `dependencies.lsp`
sequences. Conflicts, multiple owners, flow-style sequences, anchors, aliases,
merged mappings, same-line sibling content, ambiguous comment ownership, and
unsafe symlink/source layouts are reported but left byte-for-byte unchanged. If
any exact candidate has an unsupported source layout, the fixer removes none.

## Migrating a pre-APM host

Migration is an explicit operator flow; the TUI only inspects. The steps are:
print the preview with `omni agents migrate --host <name>`, review it, commit
the manifest into that host's template in dotfiles, delete the native entries
listed under "Replaced by this manifest" by hand, run `omni agents sync`, then
run it a second time to confirm it is a no-op. A missing or off-pin APM build is
repaired by `omni doctor --fix`, never by opening the TUI. Ambiguous or local
bundle evidence is never guessed; Omni leaves it unchanged and asks for review.

A config that still carries an `agents` block no longer loads: every command
that reads it fails with the removed-field error naming the migrate command.
`omni doctor` is the one surface that keeps working, because it inspects the
raw file, and it reports the leftover declarations as a warning. Remove the
retired fields from `settings.json` and keep a copy before migrating.

`omni agents migrate --host <name>` is the one import surface. It unions the
agent declarations a host had before the migration, read from a snapshot when
one is given or found, with the live native Claude and Codex state; an absent
snapshot is not an error. Preview is the default; `--dry-run` is an explicit
alias. Both print a deterministic plan, write nothing, and run no APM command.

The output is the proposed `apm.yml` followed by three lists:

| Section | Contents |
| --- | --- |
| `Replaced by this manifest (delete by hand after sync):` | Live native entries the manifest takes over, with the file or CLI record they were read from. Delete these by hand after the first sync. Snapshot declarations never appear here: they come from a config copy, not a live file. |
| `Retained (not migrated):` | Items Omni will not migrate, each with its reason, such as a marketplace with no APM source or an MCP server whose definition differs across targets. |
| `Already managed by APM:` | Native entries APM already deploys, so the manifest never re-proposes them. |

A native plugin the client has installed but disabled is listed in the same
section as any other, with the same identity columns and a leading `disabled`
note in the last column, and it never counts as the reason another item is kept.

Empty sections are omitted. An entry is already managed when its name is listed
under `mcp_servers` in `~/.apm/apm.lock.yaml`, when its command, first argument,
or working directory resolves under `~/.apm/apm_modules`, or, for a plugin, when
a lockfile dependency carries its name and marketplace repository. With no
lockfile nothing is treated as managed.

```sh
omni agents migrate --host workstation
omni agents migrate --host workstation --write
omni agents sync
```

`--write` and `--dry-run` are mutually exclusive. `--write` publishes any
required wrappers, then atomically updates only the canonical host template. It
does not write `~/.apm/apm.yml` or run APM. On success it prints
`Next: omni agents sync`.

By default Omni resolves the loaded config path through its symlink and looks
for a single `.omni-apm-migration-backup-*` directory next to the real
`settings.json`. With no such directory the preview covers native state only.
Pass `--snapshot <dir>` when there is more than one, or to point at another.

Only groups active for that host are planned: the host's assigned groups plus a
group named after the host itself. Omni resolves selected package/plugin owners
from copied snapshot evidence in this order: an explicit install/source path,
a copied source path, then one selected marketplace and one matching copied
catalog root. A name alone never proves ownership. Missing or ambiguous
evidence blocks the whole migration before any wrapper or template is written.

The snapshot's v22 fields map like this:

| Snapshot field | Rendered as |
| --- | --- |
| `agents.plugins`, selected by a group's `plugins` | One `dependencies.apm` owner entry pointing at a verified content-addressed local wrapper. |
| `agents.packages`, selected by a group's `skills` | One wrapper owner entry, unless the package is proven to be a skill already owned by a selected plugin. |
| `agents.mcp_servers`, selected by a group's `mcp_servers` | An independent `dependencies.mcp` entry, or suppression when its owner path and normalized definition exactly match a selected bundle child. |
| `agents.marketplaces`, selected by a group's `marketplaces` | Trailing `# apm marketplace add <source> --name <name>` comment; apm.yml cannot express marketplaces. |
| An entry's `agents` list | `targets:` on the apm entry, with `claude-code` renamed to `claude`. The union of every entry's targets becomes the manifest's top-level `targets:`. |
| `${VAR}` in any value | `${env:VAR}`, the only placeholder form APM expands. |

Omni inventories conventional skills, hooks, agents, commands, executable
binaries, MCP, and LSP definitions beneath each proven owner root. Exact
owner/fingerprint matches collapse into the owner dependency; unrelated
standalone declarations survive. Preview records each collapse under
`Retained (not migrated):`. Different definitions, a child claimed by multiple
owners, duplicate owner identities, incomplete runtime paths, and unsupported
native behavior produce deterministic blockers that name the declaration and
field without printing sensitive values.

MCP entries deliberately carry no per-entry `targets:`. The MCP surface is
host-global, so every declared server reaches every enabled user-global MCP
target regardless of which agents originally declared it. A server observed on
only some of the manifest's targets gets a trailing
`# reach: <targets> (apm deploys to all MCP targets): <name>` comment so the
widened reach is visible before the manifest is committed.

The rendered marketplace commands are comments because APM registers
marketplaces outside the manifest. Sync reads them back: every declared
marketplace missing from `~/.apm/marketplaces.json` is registered before the
install step, so a fresh host needs no manual `apm marketplace add`. Sync only
adds — dropping a comment line never unregisters a marketplace, which stays a
manual `apm marketplace remove`.

### Verified wrappers

With `--write`, Omni snapshots each selected legacy owner into one
content-addressed local APM package under:

```text
<Omni state>/agents-migration/bundles/<sha256>/
```

The package preserves supported source metadata and runtime files while
rebasing relative paths. Migration is offline and non-executing; unsafe paths,
literal secrets, unreadable files, ambiguous ownership, or unsupported native
behavior block the whole write. Rerun `--write` after the snapshot changes;
old hashes remain available until no live manifest references them.

### Lifecycle handoff

`omni agents sync` is the only command that materializes the live manifest and
invokes APM. Migrate and sync share the template/workspace locks and recheck the
candidate before mutation, so a concurrent edit fails before the live manifest
or APM changes. Do not run `apm` directly in parallel with sync; external APM
processes do not participate in Omni's lock.

`omni doctor` runs the global `apm audit --ci`. APM 0.34 replays a global
install in a temporary root and writes user-scope hook commands with absolute
paths, so its drift check always reports hook configs (`settings.json`,
`hooks.json`) and their `apm-hooks.json` sidecar as modified. Doctor drops those
two findings only while every APM-owned sidecar entry still appears verbatim in
the live config; an edited or removed APM hook is still reported.


## Native items APM does not manage

Migration is a one-time flow; drift is the steady state after it. A host keeps
working after adoption, and a plugin installed straight through `claude` or
`codex` afterwards is invisible to the manifest. Three read-only surfaces
report that gap and one config field records the exceptions.

`omni agents drift` lists the native plugins, MCP servers and marketplaces this
host's APM manifest could cover but does not. Items the migration classifier
retains for a stated reason are not drift, nor are the recorded ignores. It
reports only, and exits 0 whether or not drift exists. `--all` also prints the
ignored entries and the retained items with their reasons.

`omni doctor` runs the same report as the `agents-drift` check. A native
install is an operator's choice, so the check never fails: drift is a warning
naming each item, and a client that cannot be read within the check's deadline
is reported as unchecked rather than as clean. With neither `claude` nor
`codex` on `PATH` the check is skipped.

`omni agents adopt --host <name>` previews what onboarding that host onto APM
would do: the host template's shape, what the manifest would gain, and which
native items would be replaced, retained, already managed or ignored, plus any
client that could not be read. It is preview-only — it writes nothing, runs no
mutating client command, and has no apply mode. Publishing a whole host's
manifest stays `omni agents migrate --host <name> --write`.

### The ignore list

A deliberate native install is recorded in `agents.ignored` in `settings.json`,
the only field the `agents` block accepts; every other shape is still rejected
by the removed-field error. Each entry names the host, target, kind, identity,
and an optional reason:

```sh
omni agents ignore --host workstation --target claude --kind plugin \
    --id demo@official --reason "local build, not published"
omni agents unignore --host workstation --target claude --kind plugin --id demo@official
```

The host is required: an exception is a statement about one machine, not about
the fleet. Ignoring an artifact that is already ignored updates its reason
instead of adding a duplicate; unignoring one that was never recorded fails,
because the caller believed it was protected.

An ignored artifact is excluded from `agents drift`, is left in place by
adoption, and is refused by the TUI's remove and adopt keys. Ignoring is not
adopting: the artifact stays native and absent from the manifest.

The Agents view carries the same inventory under `Not managed by APM`, with
`x` to ignore or unignore, `a` to adopt one artifact into the host template,
and `d` to remove it through its own client. See
[TUI](tui.md#items-apm-does-not-manage).

## APM Version

Omni requires fork version `0.35.0+omni.5`, based on upstream release v0.35.0.
The pin, `apmPackagePin` in `internal/app/apm_version.go`, is a wheel attached to
the `apm-cli-0.35.0-omni.5` release of `lkshrk/omni` plus its SHA-256. It is not
a PyPI release, and the release is neither an Omni version nor marked Latest.

```text
omni doctor --fix
```

Doctor downloads the wheel, refuses it unless its SHA-256 matches the pin, keeps
it in the cache directory (`<cache>/apm/`), and installs that file through the
first available installer (`uv tool install`, `pipx install`, then
`pip3 install --user`). A cached wheel that still matches is reused, so a
reinstall needs no network. A mismatching download is deleted and no installer
runs. `--dry-run` plans the install with the release URL and downloads nothing.
Omni checks the digest itself because `uv` and `pipx` reject the `#sha256=`
fragment that `pip` would verify. CI and the integration image hand the same
pin to `pip`, which does.

The fork adds native omp MCP deployment and per-server MCP targets. Package
dependencies retain upstream Git/path coordinates and per-package target
selection.

Upgrading APM means rerunning Omni's APM contract and integration suites before
changing `apmVersionPin` and `apmPackagePin`. To publish the new wheel, set the
fork commit in `scripts/apm-wheel.sh`, run `scripts/apm-wheel.sh build`, put the
printed digest and the new release URL into `apmPackagePin`, and run
`scripts/apm-wheel.sh publish`. The build is reproducible; `scripts/apm-wheel.sh
verify` rebuilds from the fork commit and fails unless it matches the pin. The
integration image and CI read the same source pin rather than independently
selecting a release.
