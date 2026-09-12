# Host setup

The host app that launches `cuu` needs Accessibility and Screen Recording in macOS
Privacy & Security. Diagnose missing grants with `cuu/bin/cuu selftest`; granting
permissions is an owner action, not something to bypass.

`cuu/bin/` is ignored. Build from the repo root when needed:

```sh
(cd cuu && go build -o bin/cuu .)
```

Runtime needs `cliclick` and Apple's `/usr/bin/python3` for pyobjc scrolling.
Install dependencies only as part of requested setup; no recurring install/preflight
is necessary when tools already work. Both MCP and CLI use
`~/.cache/zcode-computer-use/state.json` with JSONL `server.log` alongside it.
