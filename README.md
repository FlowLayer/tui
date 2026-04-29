# FlowLayer TUI

**A keyboard-first terminal client for FlowLayer. Snapshot, live events, replay — your local distributed system, finally legible.**

[![Website](https://img.shields.io/badge/site-flowlayer.tech-4d8eff?style=flat-square)](https://flowlayer.tech/)
[![Releases](https://img.shields.io/github/v/release/FlowLayer/flowlayer?style=flat-square&color=4d8eff)](https://github.com/FlowLayer/flowlayer/releases)
[![Protocol](https://img.shields.io/badge/protocol-V1-4d8eff?style=flat-square)](https://github.com/FlowLayer/flowlayer/blob/main/PROTOCOL.md)
[![Made with Go](https://img.shields.io/badge/built%20with-Go-4d8eff?style=flat-square)](https://go.dev)

[Website](https://flowlayer.tech/) · [Docs](https://flowlayer.tech/explore/client-tui) · [FlowLayer server](https://github.com/FlowLayer/flowlayer) · [Releases](https://github.com/FlowLayer/flowlayer/releases) · [Distribution](https://github.com/FlowLayer/distribution) · [Protocol](https://github.com/FlowLayer/flowlayer/blob/main/PROTOCOL.md)

---

This repository is the source code of `flowlayer-client-tui`, the **official terminal client** of the [FlowLayer](https://flowlayer.tech/) runtime.

It connects to a running FlowLayer server over WebSocket, reconstructs a deterministic local view from `snapshot + live events + replay`, and gives you a calm, keyboard-driven cockpit to observe and control your services. No mouse. No drift. No lies.

```text
┌──────────────────┐    /ws + Bearer    ┌──────────────────┐
│ flowlayer-server │ ◄─────────────────► │ flowlayer-client │
│  (truth, logs)   │   snapshot + events │   -tui  (you)    │
└──────────────────┘                     └──────────────────┘
```

A running FlowLayer server is required.

---

## Why a dedicated TUI

- **Deterministic view.** Snapshot establishes the baseline; live `service_status` and `log` events extend it. The TUI never invents state.
- **Sub-100ms feedback.** Bare WebSocket, no polling, no SSE adapter, no proxy. You press a key, the server hears it.
- **Read-only by default.** It is an observer first; control actions (`s`, `x`) are explicit single-keystroke decisions, not surfaces you can misclick.
- **Config-driven.** Point it at your `flowlayer.jsonc` and it discovers `session.addr` and `session.token` for you.
- **Tiny.** A single static binary. `NO_COLOR` honored. Terminal-native rendering, no Electron, no Node.

---

## Install

Grab `flowlayer-client-tui` from the global FlowLayer release: <https://github.com/FlowLayer/flowlayer/releases>.

Package-manager recipes (`install.sh`, Homebrew, Scoop, Chocolatey, Winget) live in the [distribution repository](https://github.com/FlowLayer/distribution).

For end-user installation paths, see <https://flowlayer.tech>.

---

## Run it

**Recommended — config-driven** (the TUI reads `session.addr` and `session.token` for you):

```bash
flowlayer-client-tui -config /path/to/flowlayer.jsonc
```

**Explicit address:**

```bash
flowlayer-client-tui -addr 127.0.0.1:6999
```

**With token:**

```bash
flowlayer-client-tui -addr 127.0.0.1:6999 -token <bearer-token>
```

---

## CLI surface

| Form | Behavior |
|---|---|
| `flowlayer-client-tui` | Launches the TUI |
| `-config <path>` | Read `session.addr` (preferred) and `session.token` from JSONC |
| `-addr <host:port>` | Manual address mode |
| `-token <bearer>` | Manual token (use with `-addr`) |
| `-h`, `--help` | Print help and exit |
| `--version` | `flowlayer-client-tui 1.1.0` and exit |

Unknown flags or stray positional arguments print `Error: <message>`, the full help, and exit `2`. `NO_COLOR` disables red error coloring. `-v` is intentionally not supported.

---

## Keybindings

**Global**

| Key | Action |
|---|---|
| `q` | Quit |
| `tab` | Switch focus between services panel and logs panel |
| `/` | Filter the focused panel |
| `esc` | Close filter or modal |
| `i` | Connection info modal (address, token, status) |

**Navigation**

| Key | Action |
|---|---|
| `↑` / `↓` | Move service selection or scroll logs |
| `pgup` / `pgdn` | Page-scroll the logs panel |

When scrolling up reaches the top of the log buffer, the TUI automatically requests older entries via `get_logs` with `before_seq`. If the server is configured with `logs.dir`, history older than the in-memory ring buffer is fetched transparently from the on-disk JSONL projection.

**Actions**

| Key | Action |
|---|---|
| `s` | Start (or restart) selected service |
| `x` | Stop selected service |
| `s` / `x` (on `all logs`) | `start_all` / `stop_all` |

---

## What it is, what it isn't

| Is | Isn't |
|---|---|
| A local-dev observability cockpit | An orchestrator |
| A WebSocket V1 client | A source of truth |
| A keyboard-driven control surface | A long-term log archive |
| A deterministic snapshot/event viewer | A multi-runtime aggregator |

All business truth stays in the FlowLayer server. The TUI keeps **only UI-local state** — selection, filters, busy hints, footer messages, connection label.

---

## Architecture in five lines

1. TUI dials `ws://<addr>/ws` with optional `Authorization: Bearer …`.
2. Server emits `hello`.
3. Server emits `snapshot` (the full baseline).
4. TUI applies live `service_status` and `log` events on top.
5. TUI sends commands (`start_service`, `stop_service`, `restart_service`, `start_all`, `stop_all`, `get_logs`) over the same socket.

There is no HTTP/SSE control path. There never will be — the WebSocket is the contract. See [PROTOCOL.md](https://github.com/FlowLayer/flowlayer/blob/main/PROTOCOL.md) for log streaming, replay semantics, reconnect behavior, and the full message contract.

---

## Develop

Run from source:

```bash
go run ./cmd/flowlayer-client-tui -config /path/to/flowlayer.jsonc
go run ./cmd/flowlayer-client-tui -addr 127.0.0.1:6999 -token <bearer-token>
```

Build a local binary:

```bash
go build -o flowlayer-client-tui ./cmd/flowlayer-client-tui
```

The repository embeds an internal copy of the WebSocket client and protocol types from the server's V1 contract:

- `internal/tui` — TUI implementation (Bubble Tea model, CLI, theme, config loader, log formatting)
- `internal/wsclient` — embedded WebSocket client
- `internal/protocol` — embedded V1 envelope/types

This keeps the TUI repo autonomous — no sibling-server checkout required to build, run, or test. It is **not** a public SDK; resynchronize the embedded directories manually when the server protocol evolves.

Tests are colocated with the code (`*_test.go`) under `internal/tui/`. The codebase is intentionally small and readable — start with [`internal/tui/app.go`](internal/tui/app.go), [`internal/tui/client.go`](internal/tui/client.go), and [`internal/tui/config.go`](internal/tui/config.go).

---

## Non-goals

The TUI deliberately does not provide:

- orchestration or business-rule ownership
- inferred lifecycle reconstruction beyond runtime events
- guaranteed durable retention inside the TUI process
- multi-runtime coordination

If you need any of those — write a custom client. The protocol is documented and stable; see [BUILDING-A-CLIENT.md](https://github.com/FlowLayer/flowlayer/blob/main/BUILDING-A-CLIENT.md).

---

## License

Distributed under the terms shipped with the corresponding release artifact at <https://github.com/FlowLayer/flowlayer/releases>.
