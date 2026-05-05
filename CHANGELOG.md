# Changelog

## [1.1.1] - 2026-05-05

### Changed
- Increased the WebSocket client read limit to support larger `get_logs` responses.
- Restored and fixed logs follow/tail mode behavior on initial load.
- Stabilized older logs pagination.
- Preserved paginated history while live logs are incoming.
- Clarified logs fetch/history footer diagnostics.
- Added a command-ready guard before requesting older logs.
- Fixed pagination and reconnection state edge cases.

[1.1.1]: https://github.com/FlowLayer/flowlayer/releases/tag/v1.1.1
