package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// Regression tests for the headless DevTools browser surface. Everything here
// is hermetic except TestBrowserLiveRoundTrip, which self-skips when no
// Chromium-based browser is installed.

func TestManifestCoversEveryTool(t *testing.T) {
	var advertised []struct {
		Name string
	}
	if err := json.Unmarshal([]byte(toolsManifest), &advertised); err != nil {
		t.Fatalf("toolsManifest is not valid JSON: %v", err)
	}
	if len(advertised) == 0 {
		t.Fatal("toolsManifest advertises no tools")
	}
	seen := map[string]bool{}
	for _, tool := range advertised {
		seen[tool.Name] = true
		if !handlerExists(tool.Name) {
			t.Errorf("tool %q is advertised but has no handler", tool.Name)
		}
		if _, ok := cliVerbs[tool.Name]; !ok {
			t.Errorf("tool %q is advertised but has no CLI verb", tool.Name)
		}
	}
	for name := range cliVerbs {
		if !seen[name] {
			t.Errorf("CLI verb %q is not advertised in toolsManifest", name)
		}
	}
}

func TestBrowserWebSocketFrameRoundTrip(t *testing.T) {
	// 5 bytes = 7-bit length, 200 = 16-bit extended, 70000 = 64-bit extended.
	for _, size := range []int{5, 200, 70000} {
		t.Run(fmt.Sprintf("payload-%d", size), func(t *testing.T) {
			client, server := net.Pipe()
			defer client.Close()
			defer server.Close()
			writer := &wsConn{conn: client, r: bufio.NewReader(client), w: bufio.NewWriter(client)}
			reader := &wsConn{conn: server, r: bufio.NewReader(server), w: bufio.NewWriter(server)}
			payload := bytes.Repeat([]byte("cuu"), size/3+1)[:size]
			errCh := make(chan error, 1)
			go func() { errCh <- writer.writeFrame(opText, payload) }()
			fin, opcode, got, err := reader.readFrame()
			if err != nil {
				t.Fatalf("readFrame: %v", err)
			}
			if err := <-errCh; err != nil {
				t.Fatalf("writeFrame: %v", err)
			}
			if !fin || opcode != opText {
				t.Fatalf("fin=%v opcode=%d, want true and opText", fin, opcode)
			}
			if !bytes.Equal(got, payload) {
				t.Fatalf("payload corrupted: got %d bytes, want %d", len(got), len(payload))
			}
		})
	}
}

func TestBrowserNormalizeURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"example.com", "https://example.com"},
		{"https://example.com", "https://example.com"},
		{"http://127.0.0.1:3080", "http://127.0.0.1:3080"},
		{"about:blank", "about:blank"},
		{"data:text/html,hi", "data:text/html,hi"},
	}
	for _, c := range cases {
		if got := browserNormalizeURL(c.in); got != c.want {
			t.Errorf("browserNormalizeURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestJSLiteralIsValidJSON(t *testing.T) {
	// A selector with quotes and a backslash must survive embedding in an
	// expression, otherwise browser_click/browser_type build broken JS.
	for _, s := range []string{`#q`, `a[href="x"]`, `he said "hi"`, `back` + string(rune(92)) + `slash`} {
		lit := jsLiteral(s)
		var back string
		if err := json.Unmarshal([]byte(lit), &back); err != nil {
			t.Fatalf("jsLiteral(%q) produced invalid JSON %s: %v", s, lit, err)
		}
		if back != s {
			t.Fatalf("jsLiteral round trip: got %q, want %q", back, s)
		}
	}
}

const browserTestFormJS = `document.body.innerHTML = "<input id=q><button id=b onclick=document.title=String(Date.now())>go</button>"; "built"`

// TestBrowserLiveRoundTrip drives a real headless browser through the same
// tool functions the CLI and MCP surface call. It self-skips when no browser
// is available so the suite still runs on a bare machine.
func TestBrowserLiveRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("live browser round trip skipped in -short mode")
	}
	if _, terr := browserResolve(""); terr != nil {
		t.Skip("no Chromium-based browser available")
	}
	t.Setenv("ZCODE_CUA_DATA", t.TempDir())
	st := loadState()
	t.Cleanup(func() {
		live := loadState()
		if live.Browser != nil {
			browserStop(live.Browser)
			live.Browser = nil
			saveState(live)
		}
	})

	open, terr := toolBrowserOpen(st, args{"url": "about:blank"})
	if terr != nil {
		t.Fatalf("browser_open: %s", terr.Message)
	}
	opened, ok := open.(browserOpenPayload)
	if !ok || !opened.Headless || opened.Port == 0 {
		t.Fatalf("browser_open payload = %#v, want a headless session with a port", open)
	}

	if _, terr := toolBrowserEval(st, args{"expression": browserTestFormJS}); terr != nil {
		t.Fatalf("browser_eval: %s", terr.Message)
	}
	if _, terr := toolBrowserType(st, args{"text": "hello world", "selector": "#q"}); terr != nil {
		t.Fatalf("browser_type: %s", terr.Message)
	}
	readBack, terr := toolBrowserEval(st, args{"expression": "document.querySelector('#q').value"})
	if terr != nil {
		t.Fatalf("read back: %s", terr.Message)
	}
	if got, ok := readBack.(browserEvalPayload); !ok || got.Result != "hello world" {
		t.Fatalf("typed value = %#v, want hello world", readBack)
	}

	if _, terr := toolBrowserClick(st, args{"selector": "#b"}); terr != nil {
		t.Fatalf("browser_click: %s", terr.Message)
	}
	title, terr := toolBrowserEval(st, args{"expression": "document.title"})
	if terr != nil {
		t.Fatalf("title read: %s", terr.Message)
	}
	if got, ok := title.(browserEvalPayload); !ok || got.Result == "" {
		t.Fatalf("title after click = %#v, want the handler result", title)
	}

	shotPath := filepath.Join(t.TempDir(), "browser-shot.png")
	shot, terr := toolBrowserScreenshot(st, args{"path": shotPath})
	if terr != nil {
		t.Fatalf("browser_screenshot: %s", terr.Message)
	}
	sized, ok := shot.(browserShotPayload)
	if !ok || sized.Width <= 0 || sized.Height <= 0 || sized.Bytes <= 0 {
		t.Fatalf("screenshot payload = %#v, want positive dimensions", shot)
	}
	if _, err := os.Stat(shotPath); err != nil {
		t.Fatalf("screenshot file missing: %v", err)
	}

	closed, terr := toolBrowserClose(st, args{})
	if terr != nil {
		t.Fatalf("browser_close: %s", terr.Message)
	}
	done, ok := closed.(browserClosePayload)
	if !ok || done.ClosedPID == 0 {
		t.Fatalf("close payload = %#v, want a closed pid", closed)
	}
	if st.Browser != nil {
		t.Fatal("state still holds a browser session after close")
	}
}
