# nmtui

A terminal UI for managing Wi-Fi with NetworkManager, built with
[bubbletea](https://github.com/charmbracelet/bubbletea).

## Features

- List nearby networks with signal strength, security, and channel
- Scan on demand (`r`)
- Connect to networks — reuses saved profiles automatically, only prompts for
  a password when the network has no saved profile yet
- Disconnect (`d`), forget saved networks (`f`)
- Saved networks view (`F`): manage every Wi-Fi profile, including ones out
  of range — connect, toggle autoconnect, change the password, forget
- Toggle Wi-Fi radio on/off (`t`)
- Live status bar: radio state, active connection, IP, and interface
- Filter the network list (`/`) and sort it (`o`): signal, name, channel, or
  security
- Network details (`i`): BSSID, channel, frequency, rate, mode and security
- Speed test the active connection (`s`): ping + 10s download + 10s upload
  against Cloudflare, with live progress; `S` runs a quick 5s + 5s test
  (stdlib only, no extra dependencies)
- Connect to hidden networks by SSID (`h`)
- Multiple Wi-Fi devices: switch the managed interface (`D`)
- Mouse wheel scrolling

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
./nmtui [flags]
```

### Flags

| Flag | Description |
| --- | --- |
| `-i, --interface <name>` | Wi-Fi interface to manage (e.g. `wlan0`) |
| `-v, --version` | Print version information and exit |
| `-h, --help` | Show help information and exit |

### Keybindings

| Key | Action |
| --- | --- |
| `↑/k` `↓/j` | navigate networks |
| `enter` | connect |
| `r` | rescan |
| `t` | toggle Wi-Fi on/off |
| `d` | disconnect current network |
| `f` | forget selected network's saved profile |
| `i` | show network details |
| `o` | cycle sort order (signal → name → channel → security) |
| `F` | saved networks view |
| `h` | connect to a hidden network by SSID |
| `D` | switch Wi-Fi device (multi-NIC) |
| `/` | filter networks |
| `s` | speed test active connection |
| `S` | quick speed test (5s down + 5s up) |
| `ctrl+r` | show/hide password (at the password prompt) |
| `esc` | clear active filter / dismiss message / cancel a pending action |
| `q` / `ctrl+c` | quit |

In the saved networks view:

| Key | Action |
| --- | --- |
| `↑/k` `↓/j` | navigate profiles |
| `enter` | connect to the selected profile |
| `a` | toggle autoconnect |
| `e` | change the saved password |
| `f` | forget the profile |
| `r` | reload the profile list |
| `esc` / `F` | back to the network list |

## Notes

- If a saved profile has a stale password, connect will fail — press `f` to
  forget the network, then `enter` to reconnect with the correct password.
- Hidden networks appear as `(hidden network)`. Press `h` to join one by
  SSID; the profile is then saved normally and shows up in the list.

## Layout

```
main.go              entry point
internal/nm/         nmcli wrapper (exec, terse-mode parsing, actions)
internal/speedtest/  stdlib HTTP speed test (ping + timed download/upload)
internal/ui/         bubbletea model, keybindings, styles
```
