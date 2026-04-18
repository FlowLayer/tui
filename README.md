# FlowLayer TUI

Terminal client for observing and operating a running [FlowLayer](https://flowlayer.tech/) runtime.

- **Website**: [flowlayer.tech](https://flowlayer.tech/)
- **FlowLayer runtime**: [github.com/FlowLayer/flowlayer](https://github.com/FlowLayer/flowlayer)
- **Protocol spec**: [PROTOCOL.md](https://github.com/FlowLayer/flowlayer/blob/main/PROTOCOL.md)

A running FlowLayer instance is required. The TUI connects over WebSocket and provides a deterministic local view (snapshot + live events + replay) of distributed service interactions during development.

---

## Installation

Download the prebuilt binary from [GitHub Releases](https://github.com/FlowLayer/tui/releases).

Run the TUI:

```bash
./flowlayer-tui -addr 127.0.0.1:3000
```

With auth token:

```bash
./flowlayer-tui -addr 127.0.0.1:3000 -token <bearer-token>
```

---

## Development

Run the TUI from source (development mode):

```bash
go run . -addr 127.0.0.1:3000
```

With auth token:

```bash
go run . -addr 127.0.0.1:3000 -token <bearer-token>
```

---

## What This TUI Is

- A thin client over the FlowLayer WebSocket protocol (`/ws`)
- A local observability client for distributed-system development
- A runtime observer for service status and logs
- A keyboard-first operator surface for start/stop/restart actions

What it is not:

- Not an orchestrator
- Not a source of truth
- Not a long-term log storage layer
- Not a complete historical log archive

All business truth stays in the FlowLayer runtime.

---

## Relationship with FlowLayer

This repository contains only the terminal client (TUI). It is a read-only observer and command sender.

The [FlowLayer runtime](https://github.com/FlowLayer/flowlayer) is the source of truth for service state, log storage, and lifecycle management. The TUI connects to a running instance via the [WebSocket protocol](https://github.com/FlowLayer/flowlayer/blob/main/PROTOCOL.md) and does not embed any runtime logic.

The TUI keeps local UI state only (selection, filters, busy hints, footer messages, connection label). Service truth comes from runtime messages:

- `snapshot` for the current full view
- `service_status` for live state changes
- `log` for live logs

Command results are used for user feedback, not for inventing service state.

---

## Architecture

Connection and message flow:

1. TUI connects to `ws://<addr>/ws` (optional bearer token).
2. Runtime sends `hello`.
3. Runtime sends `snapshot`.
4. TUI consumes live events (`service_status`, `log`).
5. TUI sends commands over the same WebSocket (`start_service`, `stop_service`, `restart_service`, `start_all`, `stop_all`, `get_logs`).

There is no HTTP/SSE control path in the current TUI.

---

## Log Model

Logs are modeled around a global sequence id:

FlowLayer exposes logs as a continuous, ordered stream, not as a static dataset.

- `seq` is the ordering cursor shared by live `log` events and `get_logs` replay.
- The TUI tracks `lastSeq` as the highest sequence observed.
- Displayed logs are built from two sources:
  - live stream (`log` events)
  - historical replay (`get_logs`)
- The in-memory log list is a working set for active debugging, not a full history store.

De-duplication behavior:

- Historical batches are deduplicated by `seq`.
- Incremental appends are deduplicated with a seen-`seq` set.
- This prevents duplicate rows when replay overlaps with live delivery.

For reconnectable or long-lived clients, replay with `after_seq` is the required strategy in practice. Without `after_seq`, clients re-read large log ranges and degrade quickly as volume grows.

Implication: this is more than a passive stream viewer. The TUI merges live + replay into a continuous operator view.

---

## Log Consumption Model

The TUI is a pure consumer of the server's log contract. It does not compute, configure, or infer log limits.

What the TUI never does:

- Never computes log limits locally.
- Never reads or processes `logView` configuration.
- Never sends an implicit `limit` in `get_logs` requests.

What the TUI does:

1. Sends `get_logs` without a `limit` field.
2. Receives `effective_limit` from the server response.
3. Uses `effective_limit` to:
   - trim the visible log buffer to that size
   - control live log retention in memory

The server is the default authority for log limits. The TUI defers entirely to `effective_limit` for all retention and display decisions.

---

## Log Limit Contract

- The server decides log limits based on its configuration and built-in defaults.
- A client may override the server policy by providing an explicit `limit` in the request.
- Consumers must rely on `effective_limit` in the response as the sole source of truth for the applied limit.

---

## Reconnect Behavior

WebSocket reconnect is handled with exponential backoff in the client layer:

- `500ms -> 1s -> 2s -> 5s` (max)

After transport reconnect, the session lifecycle is:

1. receive `hello`
2. receive `snapshot`
3. if `lastSeq > 0`, request replay with `get_logs` and `after_seq=lastSeq` (normal path)
4. merge replayed logs with deduplication by `seq`
5. continue consuming live `log` events

Result: continuity after disconnect without duplicate log lines.

For reconnectable or long-lived sessions, `after_seq` is effectively required for healthy behavior.

---

## Volume and Memory

The TUI maintains an in-memory working window of log entries.

- Buffer size is governed by `effective_limit` received from the server.
- When new entries arrive (live or replay), the buffer is trimmed to `effective_limit`.
- Memory usage is bounded by the server-decided limit.
- The TUI is not a log storage system and does not preserve complete log history.

For durable retention, enable runtime disk projection (`logs.dir`) on the server.

The TUI does not enforce its own retention cap. All retention decisions are driven by `effective_limit` from the server.

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