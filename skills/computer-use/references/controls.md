# cuu controls

The Go `cuu` binary exposes CLI verbs and MCP tools sharing one state file.
CLI responses are JSON with exit codes 0/1/2. Consult available tool schemas for
exact arguments; use these patterns only on the requested app/window.

## Observe and target

- `list_apps` / `list_windows` resolve an unknown target.
- `get_app_state(app, window?, depth?, activate?)` returns a screenshot path,
  `pixel_scale`, and indexed AX tree. `[+n]`/`[~n]` mark changes in the same window.
  `include_screenshot:true` attaches image content when supported.
- Prefer `click element_index`, `set_value`, `select_text`, or `perform_action`;
  discover actions with `element_info`. After acting, refresh state before reusing
  indices. Coordinate clicks use screenshot pixels; the server rescales/reanchors.
- `filter` limits displayed tree lines without invalidating indices. `find(text?,
  role?)` queries the current capture without another screenshot. Bound large
  trees with `depth`; `depth=0` is screenshot-only.

## Conditional controls

- `menu(app)` lists menu-bar commands; `menu(app, path: "File > Export as PDF…")`
  invokes an exact title. `menu_not_found` calls for inspecting the menu, not guessing.
- `wait_for(text, until: present|gone, timeout_s)` waits for loading/export changes;
  use its result instead of repeatedly recapturing an unchanged screen.
- If AX is unavailable, `ocr(filter?)` returns Vision text regions in screenshot
  pixels. Use fresh screenshot evidence for coordinate choices.
- `window` moves/resizes/minimizes/zooms/closes using screen points, unlike clicks.
  `activate:false` observes without stealing focus; `launch:true` deliberately
  opens a non-running app.
- `select_text` reports verified `selected_text` and method, rather than assuming
  a click selected text. Keyboard fallback is capped at 400 characters.
- `type_text` paste uses and restores the clipboard; `method:"keys"` is useful
  when pastes are ignored or clipboard changes are inappropriate. Explicit
  `clipboard` reads/writes touch private user data; use only for the requested task.

## Headless browser (no focus change)

Chromium pages are driven over the DevTools Protocol on a loopback port, so
nothing appears on screen and no Accessibility or Screen Recording grant is
needed. Prefer these tools over the AX tools for web pages.

- `browser_open(url, headless?=true)` starts a throwaway-profile browser and
  navigates. The session (pid, port, profile) is shared through the state file, so
  later browser calls reuse it.
- `browser_state(include_text?, max_chars?=4000)` reports url, title, readyState
  and a capped `document.body.innerText` snapshot.
- `browser_eval(expression, await_promise?=true)` returns the value as JSON —
  the escape hatch for structured DOM reads and waiting on app state.
- `browser_click(selector? | x,y)` and `browser_type(text, selector?, clear?, submit?)`
  dispatch real input events (`Input.dispatchMouseEvent`, `Input.insertText`) rather
  than a JS `.click()`, which some applications ignore. `browser_type` does not fire
  per-key events; dispatch those with `browser_eval`.
- `browser_screenshot(path?, full_page?)` writes a PNG and reports its size. It
  never touches the display, so it works with no window on screen.
- `browser_close` stops the browser and deletes its profile directory: call it
  when the task is finished. `headless:false` opens a real window and takes focus.

Errors return `code`, `message`, and `remedy`. Recover from stale state or missing
windows by reobserving, not replaying an external action. Missing permissions need
the owner's action. Without Screen Recording the fallback capture/coordinates may
be approximate; don't treat that as precise evidence for delicate clicks.
