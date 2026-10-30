# Installation

## Requirements

Omni is a Go CLI/TUI application.

- Release archives do not require Go.
- `go install` requires the Go version declared in `go.mod`.
- Package managers that Omni should use must be available on `PATH`.
- Skill packages backed by Git require system `git` on `PATH`. HTTP
  well-known catalogs and local directory sources do not require Git.
- Node, `npx`, and `bunx` are not prerequisites for agent skills.
- Dotfile sync requires GNU Stow. Agent packages use Omni's private APM copy;
  Omni provisions it automatically with `uv`, or Python 3.10 or newer when uv is unavailable.
  Homebrew and Linux packages install `stow`, `git` and `uv` as dependencies.

## Dependency Handling By Channel

| Install channel | Dependencies |
| --- | --- |
| Homebrew | The generated formula depends on `stow`, `git` and `uv`. |
| Linux packages | The `.deb`, `.rpm`, `.apk`, and Arch packages declare `stow`, `git` and `uv`. Use the distro package manager so dependencies are resolved. |
| Release archives | Archives contain only the `omni` binary. Install `stow`, Git, and preferably uv separately. |
| `go install` | Go installs only the `omni` binary. Install `stow`, Git, and preferably uv separately. |

Low-level package tools such as `dpkg -i` and `rpm -i` may report a missing
`stow` dependency instead of fetching it. Prefer commands that resolve
dependencies, such as `apt install ./omni.deb`, `dnf install ./omni.rpm`,
`zypper install ./omni.rpm`, `apk add ./omni.apk`, or `pacman -U <package>`.

## Platform Support

| Platform | Status | Notes |
| --- | --- | --- |
| macOS | supported | Homebrew is the expected system package manager. User services use launchd where available. |
| Linux | supported | Native managers include `apt`, `apk`, `dnf`, `zypper`, and `pacman`; `aur` covers AUR packages through `yay` or `paru`. User services use systemd where available. |
| Windows | not documented | Windows support is not part of the current tested contract. |

Supported built-in concrete providers:

| Package area | Concrete providers |
| --- | --- |
| System packages | `apt`, `apk`, `dnf`, `zypper`, `pacman`, `brew` |
| Node packages | `bun`, `pnpm`, `npm` |
| Python packages | `uv`, `pip` |
| Rust binary crates | `cargo` |

## Install on Linux

Download the latest release binary into `~/.local/bin`:

```sh
curl -fsSL https://raw.githubusercontent.com/lkshrk/omni/main/scripts/linux-install.sh | bash
```

Set `DIR` to choose a different destination (for example `DIR=/usr/local/bin`).
Make sure the destination is on your `PATH`.

## Install With Homebrew

```sh
brew tap lkshrk/tap
brew trust --tap lkshrk/tap
brew install omni
```

Homebrew 6 requires trust for non-official taps before it will load their
formulae. If you prefer a narrower entry, trust only Omni:

```sh
brew trust --formula lkshrk/tap/omni
```

## Install From Releases

GitHub releases publish macOS/Linux archives and Linux packages, including
`.deb`, `.rpm`, `.apk`, and Arch Linux packages.

Download the artifact for your platform from:

```text
https://github.com/lkshrk/omni/releases
```

## Upgrade

Use the same channel you installed from:

```sh
curl -fsSL https://raw.githubusercontent.com/lkshrk/omni/main/scripts/linux-install.sh | bash
brew upgrade omni
```

Then verify the local state:

```sh
omni doctor
omni reconcile
```

## Shell Completions

Omni uses Cobra shell completions:

```sh
omni completion zsh > "${fpath[1]}/_omni"
omni completion bash > ~/.local/share/bash-completion/completions/omni
omni completion fish > ~/.config/fish/completions/omni.fish
```

Restart the shell after installing completions.
