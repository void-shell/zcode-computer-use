package main

// Element actions share one option name and schema property. CLI flags and
// strict argument parsing use the same name.
const elementActivateArg = "activate"
const elementActivateProperty = `"` + elementActivateArg + `": {"type": "boolean", "default": true, "description": "Raise the app before the element action. false skips activation and disallows synthesized-input fallbacks; apps may still activate themselves in response."}`

// toolsManifest is the MCP tools/list payload. Kept as a raw JSON constant
// so object key order is stable.
const toolsManifest = `[
 {
  "name": "list_apps",
  "description": "List running GUI apps and the frontmost one.",
  "inputSchema": {"type": "object", "properties": {}}
 },
 {
  "name": "list_windows",
  "description": "List an app's on-screen windows (id, title, bounds, frontmost flag) so you can target one with get_app_state's ` + "`window`" + ` argument.",
  "inputSchema": {
   "type": "object",
   "properties": {"app": {"type": "string"}},
   "required": ["app"]
  }
 },
 {
  "name": "get_app_state",
  "description": "Return an app window's screenshot (PNG path), pixel scale, and an indexed accessibility tree with diff markers against the previous capture of the same window ([+n] new, [~n] changed). Call ONCE per observation — do not re-call without acting in between. Element targets are preferred; fall back to x/y in screenshot pixels. ` + "`window`" + ` targets a specific window by id or title substring; ` + "`activate:false`" + ` observes without stealing focus; ` + "`launch:true`" + ` is required to open a non-running app.",
  "inputSchema": {
   "type": "object",
   "properties": {
    "app": {"type": "string", "description": "app name, path, or partial match"},
    "depth": {"type": "integer", "minimum": 0, "maximum": 12, "default": 8},
    "window": {"description": "window id (integer) or title substring; default frontmost"},
    "activate": {"type": "boolean", "default": true, "description": "false = background observe, no focus steal"},
    "launch": {"type": "boolean", "default": false, "description": "true = open the app if not running"},
    "filter": {"type": "string", "description": "case-insensitive substring: only matching tree lines are shown (the full index registry stays targetable; re-query with find)"},
    "include_screenshot": {"type": "boolean", "default": false, "description": "also attach the screenshot as MCP image content (downscaled), saving a separate file read"}
   },
   "required": ["app"]
  }
 },
 {
  "name": "click",
  "description": "Click an element by index (preferred; left-press via AXPress) or by screenshot pixel coordinates (any button, click_count 1-2; double-click is left-only). activate:false skips app activation for element targets; coordinate clicks reject it.",
  "inputSchema": {
   "type": "object",
   "properties": {
    "element_index": {"type": "integer"},
    "x": {"type": "number"},
    "y": {"type": "number"},
    "mouse_button": {"type": "string", "enum": ["left", "right", "middle"], "default": "left", "description": "coordinate clicks only"},
    "click_count": {"type": "integer", "minimum": 1, "maximum": 5, "default": 1},
    ` + elementActivateProperty + `
   }
  }
 },
 {
  "name": "type_text",
  "description": "Type literal text into the frontmost app. method=paste (default) is fast and unicode-safe but briefly uses the clipboard (restored as plain text — rich formatting can be lost; method=keys never touches the clipboard but is slow and capped at 2000 characters).",
  "inputSchema": {
   "type": "object",
   "properties": {
    "text": {"type": "string"},
    "method": {"type": "string", "enum": ["paste", "keys"], "default": "paste"}
   },
   "required": ["text"]
  }
 },
 {
  "name": "press_key",
  "description": "Press a key or chord in xdotool syntax (super+c, Return, KP_0, ctrl+shift+t).",
  "inputSchema": {
   "type": "object",
   "properties": {"key": {"type": "string"}},
   "required": ["key"]
  }
 },
 {
  "name": "scroll",
  "description": "Scroll up/down/left/right by pages, optionally at given screenshot pixel coordinates (recommended: wheel events land under the cursor, so pass x/y or position first).",
  "inputSchema": {
   "type": "object",
   "properties": {
    "direction": {"type": "string", "enum": ["up", "down", "left", "right"]},
    "pages": {"type": "number", "default": 1},
    "x": {"type": "number"},
    "y": {"type": "number"}
   }
  }
 },
 {
  "name": "drag",
  "description": "Drag from one screenshot pixel point to another.",
  "inputSchema": {
   "type": "object",
   "properties": {
    "from_x": {"type": "number"},
    "from_y": {"type": "number"},
    "to_x": {"type": "number"},
    "to_y": {"type": "number"}
   },
   "required": ["from_x", "from_y", "to_x", "to_y"]
  }
 },
 {
  "name": "set_value",
  "description": "Set the value of a settable accessibility element by index (no keystrokes, no clipboard). activate:false skips app activation.",
  "inputSchema": {
   "type": "object",
   "properties": {
    "element_index": {"type": "integer"},
    "value": {"type": "string"},
    ` + elementActivateProperty + `
   },
   "required": ["element_index", "value"]
  }
 },
 {
  "name": "select_text",
  "description": "Select a character range [start, start+length) in a text element by index. The selection is verified by content and the chosen method is reported; apps that ignore the AX range write get a keyboard-walk fallback (capped at 400 characters). activate:false attempts the AX range only and disables the keyboard fallback.",
  "inputSchema": {
   "type": "object",
   "properties": {
    "element_index": {"type": "integer"},
    "start": {"type": "integer", "minimum": 0},
    "length": {"type": "integer", "minimum": 1},
    ` + elementActivateProperty + `
   },
   "required": ["element_index", "start", "length"]
  }
 },
 {
  "name": "element_info",
  "description": "Inspect one element by index: role, name, value, enabled/focused, frame, and the AX actions it exposes (for perform_action).",
  "inputSchema": {
   "type": "object",
   "properties": {"element_index": {"type": "integer"}},
   "required": ["element_index"]
  }
 },
 {
  "name": "perform_action",
  "description": "Invoke a named accessibility action on an element (AXPick, AXConfirm, AXCancel, AXIncrement, ...). Discover names with element_info. activate:false skips app activation and refuses AXRaise. (The v2 name perform_secondary_action still works as an alias.)",
  "inputSchema": {
   "type": "object",
   "properties": {
    "element_index": {"type": "integer"},
    "action": {"type": "string", "description": "AX action name, e.g. AXPick"},
    ` + elementActivateProperty + `
   },
   "required": ["element_index", "action"]
  }
 },
 {
  "name": "find",
  "description": "Search the CURRENT capture's tree by text substring and/or role (e.g. AXButton) without re-capturing. Returns matching indexed lines; the indices are the same ones element actions take. Use after a get_app_state whose tree is large or was trimmed with ` + "`filter`" + `.",
  "inputSchema": {
   "type": "object",
   "properties": {
    "text": {"type": "string", "description": "case-insensitive substring of the element line"},
    "role": {"type": "string", "description": "exact AX role, e.g. AXButton, AXTextField"},
    "limit": {"type": "integer", "minimum": 1, "maximum": 200, "default": 40}
   }
  }
 },
 {
  "name": "menu",
  "description": "Read or drive the app's menu bar — where most macOS functionality lives, invisible to window captures. Without ` + "`path`" + `: list every menu and its items. With ` + "`path`" + ` (e.g. \"File > Export as PDF\u2026\", deeper submenus chain with more \" > \"): click that item. Titles are exact, including \u2026 and case.",
  "inputSchema": {
   "type": "object",
   "properties": {
    "app": {"type": "string", "description": "app name (partial match ok)"},
    "path": {"type": "string", "description": "menu > item path to click; omit to list"}
   },
   "required": ["app"]
  }
 },
 {
  "name": "wait_for",
  "description": "Poll the captured window's accessibility tree until a text is present (default) or gone, then return — one call instead of an observe/not-ready-yet/observe loop. Times out with a structured wait_timeout error. On return the state is stale: get_app_state before the next element action.",
  "inputSchema": {
   "type": "object",
   "properties": {
    "text": {"type": "string", "description": "case-insensitive substring to watch for"},
    "until": {"type": "string", "enum": ["present", "gone"], "default": "present"},
    "timeout_s": {"type": "number", "minimum": 1, "maximum": 60, "default": 10}
   },
   "required": ["text"]
  }
 },
 {
  "name": "clipboard",
  "description": "Read or set the system clipboard explicitly. Without ` + "`text`" + `: return the clipboard's plain-text content (empty ` + "`text`" + ` plus a hint when it holds non-text content like an image). With ` + "`text`" + `: set the clipboard to it (\"\" clears it).",
  "inputSchema": {
   "type": "object",
   "properties": {
    "text": {"type": "string", "description": "text to place on the clipboard; omit to read"}
   }
  }
 },
 {
  "name": "window",
  "description": "Move, resize, minimize, unminimize, zoom, or close an app window — no pixel-hunting. Coordinates are screen points, not screenshot pixels; no capture is involved. ` + "`window`" + ` targets a specific window by id or title substring (default frontmost). ` + "`close`" + ` may prompt the app to save unsaved changes. ` + "`unminimize`" + ` targets minimized windows by title substring or first-minimized — they have no CGWindow id while off-screen.",
  "inputSchema": {
   "type": "object",
   "properties": {
    "app": {"type": "string", "description": "app name (partial match ok)"},
    "window": {"description": "window id (integer) or title substring; default frontmost"},
    "action": {"type": "string", "enum": ["move", "resize", "minimize", "unminimize", "zoom", "close"], "description": "what to do with the window"},
    "x": {"type": "number", "description": "screen points — new left edge (move only)"},
    "y": {"type": "number", "description": "screen points — new top edge (move only)"},
    "width": {"type": "number", "description": "screen points — new width (resize only)"},
    "height": {"type": "number", "description": "screen points — new height (resize only)"}
   },
   "required": ["app", "action"]
  }
 },
 {
  "name": "ocr",
  "description": "Read text out of the CURRENT screenshot via Apple Vision — the fallback for apps whose accessibility tree is empty or unusable (Electron, canvases, games). Returns recognized lines with bounding boxes in screenshot pixels, so click x/y works on the box centers directly. Read-only: requires a capture (get_app_state first) and never invalidates element indices.",
  "inputSchema": {
   "type": "object",
   "properties": {
    "filter": {"type": "string", "description": "case-insensitive substring: only lines whose text matches are returned"}
   }
  }
 },
 {
  "name": "browser_open",
  "description": "Launch a headless Chromium browser (if none is running) and navigate to a url over the DevTools Protocol. Nothing appears on screen and no Accessibility or Screen Recording grant is needed — use this instead of the AX tools for web pages. Returns the final url, title, browser binary and debug port; the session is reused by later browser_* calls until browser_close.",
  "inputSchema": {
   "type": "object",
   "properties": {
    "url": {"type": "string", "description": "url to open; a bare host is upgraded to https"},
    "browser": {"type": "string", "description": "browser binary (absolute path or PATH name); default auto-detects Chrome, Chromium, Helium, Brave, Edge"},
    "headless": {"type": "boolean", "default": true, "description": "false opens a real window and WILL take focus"},
    "timeout_s": {"type": "number", "default": 30, "description": "seconds to wait for document.readyState to reach complete"}
   },
   "required": ["url"]
  }
 },
 {
  "name": "browser_state",
  "description": "Report the open page url, title, readyState and a text snapshot (document.body.innerText, capped at max_chars). One call per observation is enough — act on what it returns instead of polling.",
  "inputSchema": {
   "type": "object",
   "properties": {
    "include_text": {"type": "boolean", "default": true, "description": "false returns metadata only"},
    "max_chars": {"type": "number", "default": 4000, "description": "text cap; text_chars still reports the untruncated length"}
   }
  }
 },
 {
  "name": "browser_eval",
  "description": "Evaluate a JavaScript expression in the page and return its value as JSON (returnByValue). This is the escape hatch for anything the other browser tools do not cover: structured DOM reads, form logic, waiting on app state. A thrown exception returns a structured browser_eval_failed error.",
  "inputSchema": {
   "type": "object",
   "properties": {
    "expression": {"type": "string", "description": "JavaScript expression; a bare object literal must be wrapped in parentheses"},
    "await_promise": {"type": "boolean", "default": true, "description": "await a returned promise before responding"}
   },
   "required": ["expression"]
  }
 },
 {
  "name": "browser_screenshot",
  "description": "Capture the page as a PNG through the DevTools Protocol and return its path, byte size and pixel dimensions. Open the result with read_image. Unlike the AX screenshot this never touches the display, so it works with no window on screen.",
  "inputSchema": {
   "type": "object",
   "properties": {
    "path": {"type": "string", "description": "destination file; default is browser-<time>.png in the cuu data dir"},
    "full_page": {"type": "boolean", "default": false, "description": "capture beyond the viewport"}
   }
  }
 },
 {
  "name": "browser_click",
  "description": "Click in the page with a synthesized mouse event (Input.dispatchMouseEvent), not a JS .click(). Target an element by CSS selector — scrolled into view and centred automatically — or a viewport point with x/y. Prefer the selector: it survives layout changes.",
  "inputSchema": {
   "type": "object",
   "properties": {
    "selector": {"type": "string", "description": "CSS selector; the element centre becomes the click point"},
    "x": {"type": "number", "description": "viewport x in CSS pixels (requires y)"},
    "y": {"type": "number", "description": "viewport y in CSS pixels (requires x)"}
   }
  }
 },
 {
  "name": "browser_type",
  "description": "Type text into the page: optionally focus a CSS selector first, insert the text at the caret, then optionally press Enter. Uses Input.insertText, so JS keydown handlers are not triggered character by character.",
  "inputSchema": {
   "type": "object",
   "properties": {
    "text": {"type": "string", "description": "literal text to insert"},
    "selector": {"type": "string", "description": "CSS selector to focus first; default uses the already-focused element"},
    "clear": {"type": "boolean", "default": false, "description": "empty the field before typing and fire an input event"},
    "submit": {"type": "boolean", "default": false, "description": "press Enter after typing"}
   },
   "required": ["text"]
  }
 },
 {
  "name": "browser_close",
  "description": "Stop the headless browser and delete its throwaway profile directory. Call this when a browsing task is finished; a later browser_open starts a fresh session.",
  "inputSchema": {"type": "object", "properties": {}}
 }
]`
