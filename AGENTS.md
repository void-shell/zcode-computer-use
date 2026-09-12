# ZCode computer use

macOS control via the Go `cuu` CLI and MCP server (`cuu serve`). CLI/MCP share
state in `~/.cache/zcode-computer-use`; structured logs are in `server.log` there.
The v3 Python implementation under `mcp/_legacy/` and `tests/_legacy/` is provenance.

## Environment gotchas

- `cuu/bin/` is ignored; build the binary before loading the plugin after a clone.
- Runtime dependencies: `cliclick` and Apple's `/usr/bin/python3` (pyobjc scrolling).
  Accessibility and Screen Recording grants belong to the host app/terminal.
- JXA wheel events at `kCGHIDEventTap` are dropped on macOS. Scrolling uses
  `kCGSessionEventTap`, pixel units, ≤120px chunks, axis2 negative = right.
  Preserve this behavior when editing scroll handling.
- Go tests speak JSON-RPC to a spawned `cuu serve`; live GUI cases self-skip
  headless. Distinguish isolated protocol checks from live desktop automation.

## Work and release boundaries

Complete local implementation, relevant checks, result inspection, and in-scope
fixes. New regression tests and disposable fixtures are allowed. For Go changes,
`cd cuu && go build -o bin/cuu . && go test ./...` is available; select narrower
checks when adequate. `cuu/bin/cuu selftest` diagnoses runtime grants/dependencies.
Do not run legacy TextEdit e2e or other live GUI drivers merely for a docs change.

Live desktop actions must match the user's task; don't capture private windows,
paste, or modify app state as an incidental test. Permission grants, credentials,
production configuration, publishing, and irreversible data require approval.
Pages publishes `docs/` from `main`; release tags build binaries and must match
`serverVersion`. Any authorized history rewrite must preserve the outgoing tip on
an upstream archive branch first; a failed push is not permission to force-push.
