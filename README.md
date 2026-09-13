# zcode-computer-use

macOS computer control for ZCode (or any MCP client) — a single static Go
binary that gives an agent eyes and hands on native apps: per-window
screenshots, an indexed accessibility tree with diff markers, element-first
actions with screenshot-pixel fallback, menu-bar driving, condition waiting,
OCR fallback via Apple Vision, window management, explicit clipboard access,
background observe, verified text selection, four-way scrolling, and a headless
DevTools browser for web pages with no focus change.

The design target is **fewer agent turns and tokens per completed task**:
trees carry diff markers so one re-observe tells the agent what changed,
`filter` and `find` keep 350-element trees out of the context window,
`wait_for` replaces observe-poll loops with one call, and
`include_screenshot` returns the capture as MCP image content so
image-rendering clients skip a file-read round trip. A reproducible
[one-task protocol benchmark](docs/turns-tokens-benchmark.md) currently shows
the semantic path at 7 tool turns / 118 estimated request tokens / 1 image
observation versus 10 / 165 / 5 for a scripted Anthropic-style screenshot
loop. It is a control-flow lower bound, not yet an end-to-end agent win.

One binary, two surfaces, one state file:

- **MCP over stdio** (`cuu serve`) — what ZCode spawns; the full tool schema
  lives in the manifest.
- **CLI verbs** (`cuu click --element_index 3`, `cuu get_app_state --app
  TextEdit`, …) — one JSON document on stdout per verb, exit `0` ok / `1`
  structured tool error / `2` usage. Verbs and MCP calls share the on-disk
  `state.json`, so you can mix shell calls with a live MCP session.

The v3 Python server (`mcp/_legacy/server.py`) is kept for provenance; the
Go port reproduces its protocol byte-for-byte, including the golden tests
(`tests/_legacy/test_protocol.py` is the original suite the Go tests mirror).

## Why this over named alternatives

Choose by workload rather than treating every computer-use backend as the
same. This comparison reflects the projects' public documentation as checked
2026-09-03; only the linked protocol-path row has been benchmarked here.

| | zcode-computer-use | [Anthropic computer use](https://platform.claude.com/docs/en/agents-and-tools/tool-use/computer-use-tool) | [macos-use](https://macos-use.dev/) |
|---|---|---|---|
| Primary observation | Per-window screenshot + indexed AX tree + diff markers | Screenshot returned after mouse/keyboard actions | AX tree; action responses return tree diffs |
| Targeting | Element index first; screenshot pixels as fallback | Screenshot coordinates | Element text or coordinates |
| macOS menu bar | List or click an exact path such as `File > Export as PDF…` | Drive it visually | No dedicated menu-path tool documented |
| Weak/absent AX tree | Apple Vision OCR returns click-ready boxes | Native path: pixels/vision | Coordinate fallback; screenshots on demand |
| Waiting | `wait_for` polls text present/gone inside one tool call | Agent repeats observation/action loop | Action + diff tools; no equivalent wait tool documented |
| Interface | MCP-over-stdio **and** matching JSON CLI verbs sharing state | Anthropic API computer tool | MCP-over-stdio |
| Install | Release binary when tagged; source build is one Go binary | Application implements Anthropic's agent loop | `npx` package; Swift builds on first run |
| Local audit surface | JSONL event log, permission-aware selftest, golden protocol suite | Depends on the host application | Local tree dumps; open-source server |
| TextEdit export protocol path | **7 turns, 118 estimated request tokens, 1 image** | **10 turns, 165 estimated request tokens, 5 images** (scripted primitive baseline) | Not measured here |

The benchmark counts compact JSON request bytes at four bytes per estimated
text token and excludes responses, reasoning, system prompts, and image
tokens. It supports only the narrow claim that the scripted semantic route
uses fewer round trips and images; see the [method and source
trace](docs/turns-tokens-benchmark.md). Anthropic computer use remains the
portable choice across operating systems. macos-use is the closer semantic
competitor and currently has the simpler one-line installation. This project
is differentiated by its dual CLI/MCP surface, explicit menu and wait tools,
OCR fallback, and protocol-compatibility tests—not by an unmeasured universal
token claim.

## Install

Prebuilt binaries are wired but **not published yet**: the release workflow
will attach `cuu-vX.Y.Z-macos-universal.tar.gz` to the first `vX.Y.Z` tag.
Until that human-gated tag exists, the releases page has no asset and the
working install path is source build below.

From source — the plugin manifest points at the built binary, which is not
tracked in git:

```bash
git clone https://github.com/void-shell/zcode-computer-use
cd zcode-computer-use/cuu
go build -o bin/cuu .
```

From a marketplace that lists this plugin (the author's `zcode-local`
marketplace does), install `zcode-computer-use` and restart ZCode. The
manifest registers the MCP server as `${ZCODE_PLUGIN_ROOT}/cuu/bin/cuu
serve`.

Manual fallback (any ZCode build): add to your config's `mcpServers`:

```json
{
  "mcpServers": {
    "zcode-computer-use": {
      "command": "/absolute/path/to/zcode-computer-use/cuu/bin/cuu",
      "args": ["serve"]
    }
  }
}
```

Requirements: macOS, Go 1.25+ to build, `cliclick` (`brew install
cliclick`) for coordinate mouse events, and Apple's bundled
`/usr/bin/python3` (Xcode Command Line Tools — ships with pyobjc) for
scroll events only. No Python at runtime otherwise.

## Permissions (the one hurdle)

The **host app** (ZCode, or your terminal if you run the binary by hand)
needs two grants, then a restart:

- **Accessibility** — System Settings → Privacy & Security → Accessibility
- **Screen Recording** — System Settings → Privacy & Security → Screen Recording

Preflight, which names the exact missing grant:

```bash
cuu/bin/cuu selftest
```

## The operating loop

1. `list_apps` if you don't know the app name; `list_windows` to pick a
   window.
2. `get_app_state(app, window?, depth?, activate?)` — returns a screenshot
   PNG path, the pixel scale, and an indexed AX tree whose lines carry diff
   markers (`[+n]` new, `[~n]` changed) against the previous capture of the
   same window. Read the PNG. Observe once; don't re-call without acting.
3. Prefer element targets (`click element_index`, `set_value`,
   `perform_action` — discover names with `element_info`). Fall back to
   `x`,`y` in **screenshot pixels** (the server rescales via the capture's
   backing factor and re-anchors to the captured window after actions).
4. After every action the state goes stale: `get_app_state` again before
   the next element action. The diff markers tell you exactly what your
   action changed.

Four tools cut turns and tokens around that loop:

- `menu(app)` lists the menu bar (where most macOS functionality lives,
  invisible to window captures); `menu(app, path: "File > Export as
  PDF…")` clicks an item — deeper submenus chain with more `" > "`, and a
  missing item is a structured `menu_not_found`.
- `wait_for(text, until: present|gone, timeout_s)` polls the captured
  window's tree until the condition holds — one call instead of an
  observe/not-ready/observe loop; times out as `wait_timeout`.
- `get_app_state(filter: "Save")` shows only matching tree lines while the
  full index registry stays targetable; `find(text?, role?)` re-queries the
  current capture with a different lens without touching the GUI.
- `get_app_state(include_screenshot: true)` attaches the capture to the MCP
  response as image content (downscaled to ≤1568px) — clients that render
  images skip the separate file read.

And three capability tools round out the surface:

- `ocr(filter?)` reads text out of the current screenshot via Apple Vision
  (same osascript stack, no new dependencies) — the fallback when an app
  exposes no usable AX tree. Boxes come back in screenshot pixels, so
  `click x/y` works on them directly.
- `window(app, action, …)` moves/resizes/minimizes/unminimizes/zooms/closes
  a window in screen points; an ambiguous window address is a structured
  refusal, never a silent fallback.
- `clipboard(text?)` reads or sets the pasteboard explicitly (`""` clears);
  non-text content is reported honestly instead of as an empty string.

`select_text` returns the selected text plus the method that worked
(`ax_range` or `keyboard`) — the selection is verified against element
content, not trusted. `type_text` defaults to a clipboard paste (saved and
restored); `method:"keys"` never touches the clipboard.

## Headless browser

Web pages do not need the AX stack. `browser_open(url)` launches Chromium
headless and speaks the DevTools Protocol over a loopback port, so no window
appears, focus never moves, and no Accessibility or Screen Recording grant is
required. The session is recorded in the shared state file, so `cuu` verbs and
the MCP server drive the same browser.

- `browser_state(include_text?, max_chars?)` returns the url, title,
  `readyState` and a capped `document.body.innerText` snapshot.
- `browser_eval(expression, await_promise?)` evaluates JavaScript and returns the
  value as JSON — the escape hatch for structured DOM reads and for waiting on
  app state.
- `browser_click(selector | x,y)` and `browser_type(text, selector?, clear?, submit?)`
  dispatch real input events (`Input.dispatchMouseEvent`, `Input.insertText`), not a
  JS `.click()`, so handlers that ignore synthetic clicks still fire.
  `browser_type` inserts text wholesale; dispatch per-key events through
  `browser_eval` when a page listens for them.
- `browser_screenshot(path?, full_page?)` captures through the protocol and
  reports the PNG size; it never touches the display, so it works with no window
  on screen.
- `browser_close` stops the browser and deletes its throwaway profile.

`headless: false` opens a real window and takes focus — leave it at the default
`true` while the owner is working.

## The CLI

`cuu help` lists every verb with its flags. Two conventions:

- Flags map 1:1 onto the MCP tool arguments (`--element_index`,
  `--activate=false`), so the tool schema stays the single source of
  truth; only explicitly-passed flags are sent.
- Every verb prints one JSON document — the same payload the MCP surface
  returns — so shell pipelines and MCP clients see identical shapes.

```bash
cuu get_app_state --app TextEdit | jq .element_count
cuu click --element_index 4
cuu press_key --key cmd+s
```

## Safety design

- Element addresses are rebuilt server-side from regex-validated index
  chains with a freshly escaped process name — AX text can never forge an
  address, and index maps can never outlive the tree they came from.
- Failed captures never leave live-looking state; coordinate actions
  re-anchor by the captured CGWindowID, never by app name.
- Screenshots are published atomically with `0600` files in a `0700`
  directory; the clipboard is saved/restored around pastes with non-text
  detection.
- No silent launches, no silent focus steals, every error carries a remedy.

## Verification

```bash
cd cuu && go test ./...   # golden protocol tests
cuu/bin/cuu selftest                                 # deps + permissions preflight
python3 tests/_legacy/test_protocol.py               # original v3 suite, against the v3 server
python3 tests/_legacy/e2e_textedit.py                # live v3 drive of TextEdit
```

The Go suite speaks raw JSON-RPC to a spawned `cuu serve` — the same 15
golden cases as the Python suite (RPC error taxonomy, strict argument
validation, alias suppression), plus pure-function goldens for the tree
diff and AppleScript escaping. The two live-GUI cases self-skip when
System Events is unreachable.

## Known limits

- macOS only (System Events / CGWindowList / cliclick stack).
- The frontmost window is the default target; apps exposing huge AX trees
  are bounded by `depth` (0–12) and a 350-element cap.
- `select_text` keyboard fallback caps at 400 characters; `wait_for` polls
  text in the AX tree, not pixels.
- Without Screen Recording you get a full-screen capture where
  multi-display coordinates are approximate.
- Scroll needs Apple's `/usr/bin/python3` (pyobjc); Homebrew-only setups
  get a structured `unsupported` error.

## License

MIT — see [LICENSE](LICENSE).
