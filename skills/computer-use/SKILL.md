---
name: computer-use
description: Operate macOS app windows through ZCode's cuu tools.
license: MIT
---

# macOS computer use

Use for requested native-app interaction. Prefer an available purpose-built API/CLI
when it can do the task without driving the desktop.

For web pages prefer the `browser_*` tools: they drive headless Chromium over
the DevTools Protocol with no window and no focus change. Drive native windows with
the AX tools.

Observe the target window before acting. Use current element indices; coordinate
fallbacks are screenshot pixels, not screen points. Every action invalidates the
captured state: refresh before another indexed action. Inspect the screenshot when
visual placement matters; a returned file path is not visual evidence.

- [Controls](references/controls.md): observation, menus, waits, OCR, headless
  browser (DevTools, no focus change), text, coordinates, and structured errors.
- [Host setup](references/setup.md): missing binary, dependencies, and permissions.

Proceed through requested safe, reversible interaction and inspect the result.
Don't inspect unrelated private windows/clipboard data or close the owner's windows.
Purchases, uploads/sends, publishing, account or permission changes, and destructive
actions require explicit authorization for that action. Do not repeat an uncertain
external submission merely because the UI response was unclear.
