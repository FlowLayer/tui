# FlowLayer TUI

Terminal client for observing and operating a running [FlowLayer](https://flowlayer.tech/) server.

- **Website**: [flowlayer.tech](https://flowlayer.tech/)
- **FlowLayer server**: [github.com/FlowLayer/flowlayer](https://github.com/FlowLayer/flowlayer)
- **Protocol spec**: [PROTOCOL.md](https://github.com/FlowLayer/flowlayer/blob/main/PROTOCOL.md)

A running FlowLayer server is required. This repository is the external client binary.

The TUI connects over WebSocket and provides a deterministic local view (snapshot + live events + replay) of distributed service interactions during development.

Connection modes:

- Manual mode: pass `-addr` and `-token` explicitly.
- Config mode (recommended): pass `-config <path>` and let the TUI read `session.bind` and `session.token` from the server config.

---

## Installation

Download the prebuilt binary from [GitHub Releases](https://github.com/FlowLayer/tui/releases).

Run the TUI:

```bash
./flowlayer-tui -addr 127.0.0.1:6999
```

With auth token:

```bash
./flowlayer-tui -addr 127.0.0.1:6999 -token <bearer-token>
```

Recommended when `session.bind` and `session.token` are configured on the server:

```bash
./flowlayer-tui -config /path/to/flowlayer.jsonc
```

---

## Development

Run the TUI from source (development mode):

```bash
go run . -addr 127.0.0.1:6999
```

With auth token:

```bash
go run . -addr 127.0.0.1:6999 -token <bearer-token>
```

Recommended config-driven mode:

```bash
go run . -config /path/to/flowlayer.jsonc
```

---

## What This TUI Is

- A thin client over the FlowLayer WebSocket protocol (`/ws`)
- A local observability client for distributed-system development
- A server observer for service status and logs
- A keyboard-first operator surface for start/stop/restart actions

What it is not:

- Not an orchestrator
- Not a source of truth
- Not a long-term log storage layer
- Not a complete historical log archive

All business truth stays in the FlowLayer server.

---

## Relationship with FlowLayer Server

This repository contains only the terminal client (TUI). It is a read-only observer and command sender.

The [FlowLayer server](https://github.com/FlowLayer/flowlayer) is the source of truth for service state, log storage, and lifecycle management. The TUI connects to a running server instance via the [WebSocket protocol](https://github.com/FlowLayer/flowlayer/blob/main/PROTOCOL.md) and does not embed any orchestration logic.

The TUI keeps local UI state only (selection, filters, busy hints, footer messages, connection label). Service truth comes from server messages:

- `snapshot` for the current full view
- `service_status` for live state changes
- `log` for live logs

Command results are used for user feedback, not for inventing service state.

---

## Architecture

Connection and message flow:

1. TUI connects to `ws://<addr>/ws` (optional bearer token).
2. Server sends `hello`.
3. Server sends `snapshot`.
4. TUI consumes live events (`service_status`, `log`).
5. TUI sends commands over the same WebSocket (`start_service`, `stop_service`, `restart_service`, `start_all`, `stop_all`, `get_logs`).

There is no HTTP/SSE control path in the current TUI.

---

See [PROTOCOL.md](https://github.com/FlowLayer/flowlayer/blob/main/PROTOCOL.md) for log streaming, replay semantics, reconnect behavior, and the full message contract.

---

## Observability Positioning

The TUI is not only a log viewer and not only a control UI.

- It is a local dev-first observability client.
- It gives a deterministic view built from runtime snapshot + live events + replay.
- It helps understand interactions between services in a local distributed system.
- It does not replace a full observability stack.

---

## Keybindings

Global:

- `q`: quit
- `tab`: switch focus between services panel and logs panel
- `/`: start filter edit on the focused panel
- `esc`: leave filter edit mode

Navigation:

- `up` / `down`: move service selection (left panel) or scroll logs (right panel)

Actions:

- `s`: start or restart selected service (depends on current service status)
- `x`: stop selected service
- On `all logs` selection:
  - `s` sends `start_all`
  - `x` sends `stop_all`

---

## Non-Goals

The TUI deliberately does not provide:

- orchestration or business-rule ownership
- inferred lifecycle reconstruction beyond runtime events
- guaranteed durable retention inside the TUI process
- multi-runtime coordination

---

## License

MIT