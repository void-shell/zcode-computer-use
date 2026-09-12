package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Headless browser control. Chrome is driven over the DevTools Protocol on a
// loopback websocket, so nothing appears on screen and no Accessibility or
// Screen Recording grant is involved: this is the path for pages, forms and
// scraped DOM state, leaving the AX tools for native windows.
//
// The session (pid, port, profile dir) lives in state.json next to the GUI
// capture state, so the CLI verbs and the MCP server drive the SAME browser.
// The profile is a throwaway directory: browser_close deletes it.

const (
	browserDialTimeout = 10 * time.Second
	browserCallTimeout = 20 * time.Second
	browserLaunchWait  = 25 * time.Second
)

type browserSession struct {
	PID      int    `json:"pid"`
	Port     int    `json:"port"`
	Profile  string `json:"profile"`
	Binary   string `json:"binary"`
	Headless bool   `json:"headless"`
}

// ---------------------------------------------------------------- payloads

type browserOpenPayload struct {
	URL      string `json:"url"`
	Title    string `json:"title"`
	Browser  string `json:"browser"`
	Headless bool   `json:"headless"`
	Port     int    `json:"port"`
	Hint     string `json:"hint"`
}

type browserStatePayload struct {
	URL        string `json:"url"`
	Title      string `json:"title"`
	ReadyState string `json:"ready_state"`
	Text       string `json:"text"`
	TextChars  int    `json:"text_chars"`
	Truncated  bool   `json:"truncated"`
	Hint       string `json:"hint"`
}

type browserEvalPayload struct {
	Result string `json:"result"`
	Type   string `json:"type"`
	Hint   string `json:"hint"`
}

type browserShotPayload struct {
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Hint   string `json:"hint"`
}

type browserActionResult struct {
	Result string `json:"result"`
	Detail string `json:"detail,omitempty"`
	Hint   string `json:"hint"`
}

type browserClosePayload struct {
	Result    string `json:"result"`
	ClosedPID int    `json:"closed_pid,omitempty"`
	Hint      string `json:"hint"`
}

// ---------------------------------------------------------------- discovery

func browserCandidatePaths() []string {
	return []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/Applications/Helium.app/Contents/MacOS/Helium",
		"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
		"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
	}
}

func browserResolve(prefer string) (string, *ToolError) {
	if prefer != "" {
		if p, err := exec.LookPath(prefer); err == nil {
			return p, nil
		}
		if st, err := os.Stat(prefer); err == nil && !st.IsDir() {
			return prefer, nil
		}
		return "", toolErr("browser_not_found",
			"no browser executable at "+prefer,
			"pass an absolute path, a PATH name, or omit browser to auto-detect")
	}
	for _, c := range browserCandidatePaths() {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	for _, n := range []string{"google-chrome", "chromium", "chromium-browser", "helium"} {
		if p, err := exec.LookPath(n); err == nil {
			return p, nil
		}
	}
	return "", toolErr("browser_not_found",
		"no Chromium-based browser found",
		"install Chrome or Chromium, or pass browser with an absolute path")
}

func browserFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func browserDevToolsURL(port int, suffix string) string {
	return "http://127.0.0.1:" + strconv.Itoa(port) + suffix
}

func browserWaitDevTools(port int, timeout time.Duration) *ToolError {
	client := &http.Client{Timeout: 3 * time.Second}
	deadline := time.Now().Add(timeout)
	last := "no response"
	for time.Now().Before(deadline) {
		resp, err := client.Get(browserDevToolsURL(port, "/json/version"))
		if err == nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
			last = resp.Status + ": " + string(body)
		} else {
			last = err.Error()
		}
		time.Sleep(150 * time.Millisecond)
	}
	return toolErr("browser_launch_failed",
		"the devtools endpoint never came up on port "+strconv.Itoa(port)+" ("+last+")",
		"confirm the browser runs headless here, or pass browser with another binary")
}

type devTarget struct {
	ID                   string
	Type                 string
	URL                  string
	Title                string
	WebSocketDebuggerURL string
}

func browserTargets(port int) ([]devTarget, *ToolError) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(browserDevToolsURL(port, "/json/list"))
	if err != nil {
		return nil, toolErr("browser_unreachable",
			"could not list devtools targets: "+err.Error(),
			"the browser may have exited; call browser_open again")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, toolErr("browser_unreachable", "could not read the target list: "+err.Error(), "")
	}
	var out []devTarget
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, toolErr("browser_unreachable", "the target list was not valid JSON: "+err.Error(), "")
	}
	return out, nil
}

func browserPageSocket(port int) (string, *ToolError) {
	targets, terr := browserTargets(port)
	if terr != nil {
		return "", terr
	}
	for _, t := range targets {
		if t.Type == "page" && t.WebSocketDebuggerURL != "" {
			return t.WebSocketDebuggerURL, nil
		}
	}
	req, err := http.NewRequest(http.MethodPut, browserDevToolsURL(port, "/json/new?about:blank"), nil)
	if err == nil {
		client := &http.Client{Timeout: 5 * time.Second}
		if resp, derr := client.Do(req); derr == nil {
			data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			var t devTarget
			if json.Unmarshal(data, &t) == nil && t.WebSocketDebuggerURL != "" {
				return t.WebSocketDebuggerURL, nil
			}
		}
	}
	return "", toolErr("browser_unreachable",
		"no page target is available",
		"call browser_open with a url to create one")
}

// ---------------------------------------------------------------- lifecycle

func browserStart(binary string, headless bool) (*browserSession, *ToolError) {
	port, err := browserFreePort()
	if err != nil {
		return nil, toolErr("browser_launch_failed", "could not reserve a loopback port: "+err.Error(), "")
	}
	profile, err := os.MkdirTemp("", "cuu-browser-*")
	if err != nil {
		return nil, toolErr("browser_launch_failed", "could not create a profile directory: "+err.Error(), "")
	}
	flags := []string{
		"--remote-debugging-port=" + strconv.Itoa(port),
		"--user-data-dir=" + profile,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-networking",
		"--disable-component-update",
		"--disable-default-apps",
		"--disable-extensions",
		"--disable-sync",
		"--disable-translate",
		"--mute-audio",
		"--metrics-recording-only",
		"--window-size=1440,900",
	}
	if headless {
		flags = append(flags, "--headless=new", "--disable-gpu", "--hide-scrollbars")
	}
	flags = append(flags, "about:blank")
	cmd := exec.Command(binary, flags...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		os.RemoveAll(profile)
		return nil, toolErr("browser_launch_failed", "could not start "+binary+": "+err.Error(), "")
	}
	go func() { _ = cmd.Wait() }()
	bs := &browserSession{
		PID:      cmd.Process.Pid,
		Port:     port,
		Profile:  profile,
		Binary:   binary,
		Headless: headless,
	}
	if terr := browserWaitDevTools(port, browserLaunchWait); terr != nil {
		browserStop(bs)
		return nil, terr
	}
	return bs, nil
}

func browserAlive(bs *browserSession) bool {
	if bs == nil || bs.PID <= 0 {
		return false
	}
	if err := syscall.Kill(bs.PID, 0); err != nil {
		return false
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(browserDevToolsURL(bs.Port, "/json/version"))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func browserStop(bs *browserSession) {
	if bs == nil {
		return
	}
	if bs.PID > 0 {
		_ = syscall.Kill(bs.PID, syscall.SIGTERM)
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if syscall.Kill(bs.PID, 0) != nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		_ = syscall.Kill(bs.PID, syscall.SIGKILL)
	}
	if bs.Profile != "" {
		os.RemoveAll(bs.Profile)
	}
}

func browserEnsure(st *serverState, headless bool, prefer string) (*browserSession, *ToolError) {
	if browserAlive(st.Browser) {
		return st.Browser, nil
	}
	if st.Browser != nil {
		browserStop(st.Browser)
		st.Browser = nil
	}
	binary, terr := browserResolve(prefer)
	if terr != nil {
		return nil, terr
	}
	bs, terr := browserStart(binary, headless)
	if terr != nil {
		return nil, terr
	}
	st.Browser = bs
	saveState(st)
	return bs, nil
}

func browserConnect(st *serverState, headless bool, prefer string) (*cdpConn, *browserSession, *ToolError) {
	bs, terr := browserEnsure(st, headless, prefer)
	if terr != nil {
		return nil, nil, terr
	}
	socket, terr := browserPageSocket(bs.Port)
	if terr != nil {
		return nil, nil, terr
	}
	conn, err := cdpDial(socket, browserDialTimeout)
	if err != nil {
		return nil, nil, toolErr("browser_unreachable",
			"could not open the devtools socket: "+err.Error(),
			"the browser may have exited; call browser_open again")
	}
	_ = conn.Call("Page.enable", nil, nil, browserCallTimeout)
	_ = conn.Call("Runtime.enable", nil, nil, browserCallTimeout)
	return conn, bs, nil
}

// ---------------------------------------------------------------- helpers

// evalRaw runs one expression with returnByValue and reports either the
// value, a stringified exception, or a tool error.
func evalRaw(conn *cdpConn, expr string, awaitPromise bool, timeout time.Duration) (json.RawMessage, string, string, *ToolError) {
	var res struct {
		Result struct {
			Type  string
			Value json.RawMessage
		}
		ExceptionDetails *struct {
			Text      string
			Exception *struct {
				Description string
			}
		}
	}
	params := map[string]any{
		"expression":    expr,
		"returnByValue": true,
		"awaitPromise":  awaitPromise,
		"userGesture":   true,
	}
	if err := conn.Call("Runtime.evaluate", params, &res, timeout); err != nil {
		return nil, "", "", toolErr("browser_eval_failed", err.Error(), "")
	}
	if res.ExceptionDetails != nil {
		msg := res.ExceptionDetails.Text
		if res.ExceptionDetails.Exception != nil && res.ExceptionDetails.Exception.Description != "" {
			msg = res.ExceptionDetails.Exception.Description
		}
		if msg == "" {
			msg = "the page threw while evaluating the expression"
		}
		return nil, res.Result.Type, msg, nil
	}
	return res.Result.Value, res.Result.Type, "", nil
}

// displayValue renders a CDP value for humans: strings verbatim, everything
// else as compact JSON.
func displayValue(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var anyVal any
	if json.Unmarshal(raw, &anyVal) == nil {
		if b, err := json.Marshal(anyVal); err == nil {
			return string(b)
		}
	}
	return string(raw)
}

// jsLiteral embeds a Go string as a JavaScript string literal. JSON string
// syntax is a subset of JavaScript's, so marshalling is sufficient.
func jsLiteral(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return "\"\""
	}
	return string(b)
}

func browserNormalizeURL(raw string) string {
	if strings.Contains(raw, "://") {
		return raw
	}
	for _, p := range []string{"about:", "data:", "file:", "chrome:"} {
		if strings.HasPrefix(raw, p) {
			return raw
		}
	}
	return "https://" + raw
}

// ---------------------------------------------------------------- tools

func toolBrowserOpen(st *serverState, a args) (any, *ToolError) {
	rawURL, terr := argStr(a, "url", true)
	if terr != nil {
		return nil, terr
	}
	headless, terr := argBool(a, "headless", true)
	if terr != nil {
		return nil, terr
	}
	prefer, terr := argStr(a, "browser", false)
	if terr != nil {
		return nil, terr
	}
	timeoutS, terr := argNumber(a, "timeout_s", float64(30), true)
	if terr != nil {
		return nil, terr
	}
	conn, bs, terr := browserConnect(st, headless, prefer)
	if terr != nil {
		return nil, terr
	}
	defer conn.close()
	navURL := browserNormalizeURL(rawURL)
	if err := conn.Call("Page.navigate", map[string]any{"url": navURL}, nil, browserCallTimeout); err != nil {
		return nil, toolErr("browser_navigate_failed", err.Error(), "check the url and try again")
	}
	deadline := time.Now().Add(time.Duration(timeoutS * float64(time.Second)))
	var current struct {
		URL   string
		Title string
		Ready string
	}
	for {
		raw, _, exc, eterr := evalRaw(conn,
			"({url:location.href,title:document.title,ready:document.readyState})",
			false, browserCallTimeout)
		if eterr != nil {
			return nil, eterr
		}
		if exc == "" {
			_ = json.Unmarshal(raw, &current)
			if current.Ready == "complete" {
				break
			}
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	return browserOpenPayload{
		URL:      current.URL,
		Title:    current.Title,
		Browser:  bs.Binary,
		Headless: bs.Headless,
		Port:     bs.Port,
		Hint:     "Drive the page with browser_eval, browser_click, browser_type and browser_screenshot; call browser_close when finished.",
	}, nil
}

func toolBrowserState(st *serverState, a args) (any, *ToolError) {
	includeText, terr := argBool(a, "include_text", true)
	if terr != nil {
		return nil, terr
	}
	maxChars, terr := argNumber(a, "max_chars", float64(4000), true)
	if terr != nil {
		return nil, terr
	}
	limit := int(maxChars)
	if limit < 0 {
		limit = 0
	}
	conn, _, terr := browserConnect(st, true, "")
	if terr != nil {
		return nil, terr
	}
	defer conn.close()
	expr := "({url:location.href,title:document.title,ready:document.readyState," +
		"text:(document.body?document.body.innerText:\"\").slice(0," + strconv.Itoa(limit+1) + ")})"
	raw, _, exc, eterr := evalRaw(conn, expr, false, browserCallTimeout)
	if eterr != nil {
		return nil, eterr
	}
	if exc != "" {
		return nil, toolErr("browser_eval_failed", exc, "the page may have navigated; retry browser_state")
	}
	var got struct {
		URL   string
		Title string
		Ready string
		Text  string
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		return nil, toolErr("browser_eval_failed", "could not decode the page state: "+err.Error(), "")
	}
	full := got.Text
	text := full
	truncated := false
	if limit > 0 && len(text) > limit {
		text = text[:limit]
		truncated = true
	}
	if !includeText {
		text = ""
	}
	return browserStatePayload{
		URL:        got.URL,
		Title:      got.Title,
		ReadyState: got.Ready,
		Text:       text,
		TextChars:  len(full),
		Truncated:  truncated,
		Hint:       "Text is document.body.innerText; raise max_chars for more, or use browser_eval for structured DOM reads.",
	}, nil
}

func toolBrowserEval(st *serverState, a args) (any, *ToolError) {
	expr, terr := argStr(a, "expression", true)
	if terr != nil {
		return nil, terr
	}
	awaitPromise, terr := argBool(a, "await_promise", true)
	if terr != nil {
		return nil, terr
	}
	conn, _, terr := browserConnect(st, true, "")
	if terr != nil {
		return nil, terr
	}
	defer conn.close()
	raw, typeName, exc, eterr := evalRaw(conn, expr, awaitPromise, browserCallTimeout)
	if eterr != nil {
		return nil, eterr
	}
	if exc != "" {
		return nil, toolErr("browser_eval_failed", exc, "the expression threw in the page; fix it and retry")
	}
	return browserEvalPayload{
		Result: displayValue(raw),
		Type:   typeName,
		Hint:   "returnByValue is on: the result is a JSON snapshot, not a live handle.",
	}, nil
}

func toolBrowserScreenshot(st *serverState, a args) (any, *ToolError) {
	pathArg, terr := argStr(a, "path", false)
	if terr != nil {
		return nil, terr
	}
	fullPage, terr := argBool(a, "full_page", false)
	if terr != nil {
		return nil, terr
	}
	conn, _, terr := browserConnect(st, true, "")
	if terr != nil {
		return nil, terr
	}
	defer conn.close()
	var shot struct {
		Data string
	}
	params := map[string]any{"format": "png", "captureBeyondViewport": fullPage}
	if err := conn.Call("Page.captureScreenshot", params, &shot, 60*time.Second); err != nil {
		return nil, toolErr("browser_capture_failed", err.Error(), "the page may still be loading; retry")
	}
	data, err := base64.StdEncoding.DecodeString(shot.Data)
	if err != nil {
		return nil, toolErr("browser_capture_failed", "the screenshot payload was not base64: "+err.Error(), "")
	}
	dest := pathArg
	if dest == "" {
		dest = filepath.Join(dataDir(),
			fmt.Sprintf("browser-%d-%d.png", time.Now().Unix(), len(data)))
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return nil, toolErr("browser_capture_failed", "could not write the screenshot: "+err.Error(), "")
	}
	w, h, _ := pngSize(dest)
	return browserShotPayload{
		Path:   dest,
		Bytes:  len(data),
		Width:  w,
		Height: h,
		Hint:   "read_image opens this PNG; full_page=true captures beyond the viewport.",
	}, nil
}

func toolBrowserClick(st *serverState, a args) (any, *ToolError) {
	selector, terr := argStr(a, "selector", false)
	if terr != nil {
		return nil, terr
	}
	hasPoint := false
	x, y := 0.0, 0.0
	if _, ok := a["x"]; ok {
		x, terr = argNumber(a, "x", nil, false)
		if terr != nil {
			return nil, terr
		}
		hasPoint = true
	}
	if _, ok := a["y"]; ok {
		y, terr = argNumber(a, "y", nil, false)
		if terr != nil {
			return nil, terr
		}
		hasPoint = true
	}
	if selector == "" && !hasPoint {
		return nil, toolErr("invalid_args",
			"click needs a selector or both x and y",
			"pass selector for an element, or x/y in viewport pixels")
	}
	conn, _, terr := browserConnect(st, true, "")
	if terr != nil {
		return nil, terr
	}
	defer conn.close()
	detail := ""
	if selector != "" {
		expr := "(function(){var e=document.querySelector(" + jsLiteral(selector) +
			");if(!e)return null;e.scrollIntoView({block:\"center\",inline:\"center\"});" +
			"var r=e.getBoundingClientRect();return {x:r.left+r.width/2,y:r.top+r.height/2};})()"
		raw, _, exc, eterr := evalRaw(conn, expr, false, browserCallTimeout)
		if eterr != nil {
			return nil, eterr
		}
		if exc != "" {
			return nil, toolErr("browser_click_failed", exc, "the selector expression threw")
		}
		if len(raw) == 0 || string(raw) == "null" {
			return nil, toolErr("browser_click_failed",
				"no element matched "+selector,
				"check the selector, or wait for the page to finish loading")
		}
		var pt struct {
			X float64
			Y float64
		}
		if err := json.Unmarshal(raw, &pt); err != nil {
			return nil, toolErr("browser_click_failed", "could not resolve the element box: "+err.Error(), "")
		}
		x, y = pt.X, pt.Y
		detail = fmt.Sprintf("element %s at (%.0f, %.0f)", selector, x, y)
	}
	for _, ev := range []string{"mousePressed", "mouseReleased"} {
		params := map[string]any{
			"type":       ev,
			"x":          x,
			"y":          y,
			"button":     "left",
			"clickCount": 1,
		}
		if err := conn.Call("Input.dispatchMouseEvent", params, nil, browserCallTimeout); err != nil {
			return nil, toolErr("browser_click_failed", err.Error(), "")
		}
	}
	if detail == "" {
		detail = fmt.Sprintf("viewport point (%.0f, %.0f)", x, y)
	}
	return browserActionResult{
		Result: "clicked",
		Detail: detail,
		Hint:   "Call browser_state to confirm the page reacted.",
	}, nil
}

func toolBrowserType(st *serverState, a args) (any, *ToolError) {
	text, terr := argStr(a, "text", true)
	if terr != nil {
		return nil, terr
	}
	selector, terr := argStr(a, "selector", false)
	if terr != nil {
		return nil, terr
	}
	submit, terr := argBool(a, "submit", false)
	if terr != nil {
		return nil, terr
	}
	clearFirst, terr := argBool(a, "clear", false)
	if terr != nil {
		return nil, terr
	}
	conn, _, terr := browserConnect(st, true, "")
	if terr != nil {
		return nil, terr
	}
	defer conn.close()
	detail := "into the focused element"
	if selector != "" {
		focus := "(function(){var e=document.querySelector(" + jsLiteral(selector) +
			");if(!e)return false;e.focus();return true;})()"
		if clearFirst {
			focus = "(function(){var e=document.querySelector(" + jsLiteral(selector) +
				");if(!e)return false;e.focus();" +
				"if(e.isContentEditable){e.textContent=\"\";}" +
				"else if(\"value\" in e){" +
				"var d=Object.getOwnPropertyDescriptor(e.constructor.prototype,\"value\");" +
				"if(d&&d.set){d.set.call(e,\"\");}else{e.value=\"\";}" +
				"e.dispatchEvent(new Event(\"input\",{bubbles:true}));}return true;})()"
		}
		raw, _, exc, eterr := evalRaw(conn, focus, false, browserCallTimeout)
		if eterr != nil {
			return nil, eterr
		}
		if exc != "" {
			return nil, toolErr("browser_type_failed", exc, "the focus expression threw")
		}
		if string(raw) != "true" {
			return nil, toolErr("browser_type_failed",
				"no element matched "+selector,
				"check the selector, or wait for the page to finish loading")
		}
		detail = "into " + selector
	}
	if err := conn.Call("Input.insertText", map[string]any{"text": text}, nil, browserCallTimeout); err != nil {
		return nil, toolErr("browser_type_failed", err.Error(), "")
	}
	if submit {
		base := map[string]any{
			"key": "Enter", "code": "Enter",
			"windowsVirtualKeyCode": 13, "nativeVirtualKeyCode": 13,
		}
		for _, ev := range []string{"keyDown", "keyUp"} {
			params := map[string]any{}
			for k, v := range base {
				params[k] = v
			}
			params["type"] = ev
			if err := conn.Call("Input.dispatchKeyEvent", params, nil, browserCallTimeout); err != nil {
				return nil, toolErr("browser_type_failed", err.Error(), "")
			}
		}
		detail += " then Enter"
	}
	return browserActionResult{
		Result: "typed",
		Detail: detail,
		Hint:   "Input.insertText does not fire per-key events, so JS keydown listeners will not see it; use browser_eval to dispatch those.",
	}, nil
}

func toolBrowserClose(st *serverState, a args) (any, *ToolError) {
	if st.Browser == nil {
		return browserClosePayload{
			Result: "no browser session",
			Hint:   "browser_open starts one when needed.",
		}, nil
	}
	pid := st.Browser.PID
	browserStop(st.Browser)
	st.Browser = nil
	saveState(st)
	return browserClosePayload{
		Result:    "browser stopped",
		ClosedPID: pid,
		Hint:      "The throwaway profile directory was removed.",
	}, nil
}
