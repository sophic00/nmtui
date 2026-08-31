# nmtui

A terminal UI for managing Wi-Fi with NetworkManager, built with
[bubbletea](https://github.com/charmbracelet/bubbletea).

## Features

- List nearby networks with signal strength, security, and channel
- Scan on demand (`r`)
- Connect to networks — reuses saved profiles automatically, only prompts for
  a password when the network has no saved profile yet
- Disconnect (`d`), forget saved networks (`f`)
- Toggle Wi-Fi radio on/off (`t`)
- Live status bar: radio state, active connection, and IP address
- Filter the network list (`/`)

## Requirements

- Linux with NetworkManager (`networkmanager` package on Arch)
- `nmcli` in `$PATH`
- An active session (polkit grants Wi-Fi management without root on desktop
  sessions like KDE)

## Build

```sh
go build -o nmtui .
```

### Reproducible build with Nix

`flake.nix` pins nixpkgs and the Go toolchain through `flake.lock`, so every
build uses an identical dependency set regardless of the host system:

```sh
nix build             # binary at ./result/bin/nmtui
nix flake check       # builds the package and runs the test suite in the sandbox
nix run .             # run the TUI straight from the flake
```

If you use [direnv](https://direnv.net/), `direnv allow` once — every shell in
this directory then automatically uses the pinned dev shell (Go 1.27, gopls,
make) via nix-direnv:

```sh
direnv allow
go version            # reports the toolchain from the flake, not the system
make build            # or: make check (vet + tests), make run, make install
```

## Usage

```sh
./nmtui
```

| Key | Action |
| --- | --- |
| `↑/k` `↓/j` | navigate networks |
| `enter` | connect |
| `r` | rescan |
| `t` | toggle Wi-Fi on/off |
| `d` | disconnect current network |
| `f` | forget selected network's saved profile |
| `/` | filter networks |
| `q` / `ctrl+c` | quit |

## Notes

- If a saved profile has a stale password, connect will fail — press `f` to
  forget the network, then `enter` to reconnect with the correct password.
- Hidden networks appear as `(hidden network)` and cannot be joined from the
  list; create a profile for them once via `nmcli` and they will show up
  normally afterwards.

## Layout

```
main.go              entry point
internal/nm/         nmcli wrapper (exec, terse-mode parsing, actions)
internal/ui/         bubbletea model, keybindings, styles
```
