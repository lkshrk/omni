# Concepts

Omni has one durable configuration for tools, dotfiles and agent declarations.
APM resolves and deploys agent packages and owns their runtime state.

| Surface | Owner | Durable state |
| --- | --- | --- |
| Tools | Omni/providers | `settings.json`, provider state |
| Dotfiles | Omni | `settings.json`, dotfiles repository |
| Agent declarations | Omni | `settings.d/agents.json`, group `agents` lists |
| Agent packages, MCP, plugins, marketplaces | APM | `~/.apm/apm.yml` (rendered by Omni), `apm.lock.yaml`, `marketplaces.json` |

`omni agents sync` is the integration boundary. When the settings declare agent
state it renders `~/.apm/apm.yml` for the current host, otherwise it copies the
host template; then it invokes APM in the global workspace.

## Hosts

Hosts select Omni settings, tool configuration and, through their groups, the
agent entries rendered for them. APM still resolves which clients are active.

## Groups

Groups organize Omni tools and dotfiles, and list agent entries by name in
`agents`.

## The three stores

Omni settings, provider state, and APM state are separate. Omni declares agent
state; APM's workspace holds what it resolved and deployed.
