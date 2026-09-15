package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// captureActionScripts replaces desktop commands with test-owned executables.
// Tests exercise the real handlers/CLI, but never reach the user's desktop.
func captureActionScripts(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "scripts.log")
	t.Setenv("CUU_TEST_SCRIPTS", log)
	t.Setenv("ZCODE_CUA_DATA", t.TempDir())
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	stub := "#!/bin/sh\n" +
		"[ \"$#\" = 4 ] && [ \"$1\" = -l ] && [ \"$3\" = -e ] || exit 2\n" +
		"printf '%s\\n' \"$4\" >> \"$CUU_TEST_SCRIPTS\"\n"
	if err := os.WriteFile(filepath.Join(dir, "osascript"), []byte(stub), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"cliclick", "pbcopy", "pbpaste", "screencapture", "open"} {
		blocked := "#!/bin/sh\nprintf 'unexpected desktop command: %s\\n' \"$0\" >> \"$CUU_TEST_SCRIPTS\"\nexit 1\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(blocked), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return log
}

func elementActionState() *serverState {
	return &serverState{
		App:    "Background Demo",
		WinSrc: `(window 2 of process "Background Demo")`,
		Elements: map[int]treeEntry{
			7: {Chain: "1.2", Body: `AXButton "Harmless fixture"`},
		},
	}
}

func assertClickScript(t *testing.T, log string, activate bool) {
	t.Helper()
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	front := ""
	if activate {
		front = "    set frontmost of process \"Background Demo\" to true\n"
	}
	want := "tell application \"System Events\"\n" + front +
		"    repeat 2 times\n" +
		"        perform action \"AXPress\" of (UI element 2 of (UI element 1 of (window 2 of process \"Background Demo\")))\n" +
		"    end repeat\n" +
		"end tell\n"
	if string(raw) != want {
		t.Fatalf("script = %q, want %q", raw, want)
	}
}

func TestClickActivation(t *testing.T) {
	for _, mode := range []string{"default", "true", "false"} {
		t.Run(mode, func(t *testing.T) {
			log := captureActionScripts(t)
			st := elementActionState()
			a := args{"element_index": json.Number("7"), "click_count": json.Number("2")}
			if mode != "default" {
				a["activate"] = mode == "true"
			}
			if _, terr := toolClick(st, a); terr != nil {
				t.Fatal(terr)
			}
			assertClickScript(t, log, mode != "false")
			if !st.Stale || len(st.Elements) != 0 || !loadState().Stale {
				t.Fatal("successful action must invalidate in-memory and saved element state")
			}
		})
	}
}

func TestClickActivationRejects(t *testing.T) {
	cases := map[string]args{
		"string":      {"element_index": json.Number("7"), "activate": "false"},
		"null":        {"element_index": json.Number("7"), "activate": nil},
		"number":      {"element_index": json.Number("7"), "activate": json.Number("0")},
		"coordinates": {"x": json.Number("10"), "y": json.Number("20"), "activate": false},
		"null index":  {"element_index": nil, "x": json.Number("10"), "y": json.Number("20"), "activate": false},
	}
	for name, a := range cases {
		t.Run(name, func(t *testing.T) {
			log := captureActionScripts(t)
			st := elementActionState()
			_, terr := toolClick(st, a)
			if terr == nil || terr.Code != "invalid_args" {
				t.Fatalf("error = %v, want invalid_args", terr)
			}
			if _, err := os.Stat(log); !os.IsNotExist(err) {
				t.Fatalf("invalid call reached a desktop subprocess: %v", err)
			}
			if st.Stale || len(st.Elements) != 1 {
				t.Fatal("argument rejection must not mutate state")
			}
		})
	}
}

func TestClickActivationCLI(t *testing.T) {
	bin := buildBinary(t)
	for _, mode := range []string{"default", "true", "false"} {
		t.Run(mode, func(t *testing.T) {
			log := captureActionScripts(t)
			saveState(elementActionState())
			argv := []string{"click", "--element_index", "7", "--click_count", "2"}
			if mode != "default" {
				argv = append(argv, "--activate="+mode)
			}
			out, err := exec.Command(bin, argv...).CombinedOutput()
			if err != nil {
				t.Fatalf("click CLI: %v\n%s", err, out)
			}
			var payload map[string]any
			if err := json.Unmarshal(out, &payload); err != nil {
				t.Fatalf("CLI result is not JSON: %q", out)
			}
			if _, ok := payload["result"].(string); !ok {
				t.Fatalf("missing action result: %v", payload)
			}
			assertClickScript(t, log, mode != "false")
			if !loadState().Stale {
				t.Fatal("CLI action must save stale state")
			}
		})
	}
}

func TestElementActivationSurface(t *testing.T) {
	if elementActivateArg != "activate" {
		t.Fatalf("consumer expects activate, got %q", elementActivateArg)
	}
	var tools []struct {
		Name        string
		InputSchema struct {
			Properties map[string]struct {
				Type    string
				Default any
			}
			Required []string
		}
	}
	if err := json.Unmarshal([]byte(toolsManifest), &tools); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range tools {
		if tool.Name != "click" {
			continue
		}
		found = true
		prop, ok := tool.InputSchema.Properties["activate"]
		if !ok || prop.Type != "boolean" || prop.Default != true {
			t.Fatalf("click activate schema = %+v, want optional boolean default true", prop)
		}
		for _, name := range tool.InputSchema.Required {
			if name == "activate" {
				t.Fatal("activate must remain optional")
			}
		}
	}
	if !found {
		t.Fatal("click absent from manifest")
	}
	count := 0
	for _, flag := range cliVerbs["click"].flags {
		if strings.TrimPrefix(flag, "b:") == "activate" {
			if flag != "b:activate" {
				t.Fatalf("activate CLI flag is not boolean: %q", flag)
			}
			count++
		}
	}
	if count != 1 {
		t.Fatalf("click has %d activate flags, want 1", count)
	}
}

// --- N4: activation matrix across the remaining element verbs ---------------

func TestElementActivationSurfaceAllTools(t *testing.T) {
	var tools []struct {
		Name        string
		InputSchema struct {
			Properties map[string]struct {
				Type    string
				Default any
			}
			Required []string
		}
	}
	if err := json.Unmarshal([]byte(toolsManifest), &tools); err != nil {
		t.Fatal(err)
	}
	byName := map[string]int{}
	for i, tool := range tools {
		byName[tool.Name] = i
	}

	elementVerbs := []string{"click", "set_value", "select_text", "perform_action"}
	for _, name := range elementVerbs {
		idx, ok := byName[name]
		if !ok {
			t.Fatalf("%s absent from manifest", name)
		}
		prop, ok := tools[idx].InputSchema.Properties["activate"]
		if !ok || prop.Type != "boolean" || prop.Default != true {
			t.Fatalf("%s activate schema = %+v, want optional boolean default true", name, prop)
		}
		for _, req := range tools[idx].InputSchema.Required {
			if req == "activate" {
				t.Fatalf("%s must keep activate optional", name)
			}
		}
		flags := 0
		for _, flag := range cliVerbs[name].flags {
			if flag == "b:"+elementActivateArg {
				flags++
			}
		}
		if flags != 1 {
			t.Fatalf("%s has %d boolean activate flags, want 1", name, flags)
		}
	}

	// Verbs outside the element family must not have acquired the option.
	for _, name := range []string{"type_text", "press_key", "scroll", "menu", "wait_for", "list_apps"} {
		idx, ok := byName[name]
		if !ok {
			continue
		}
		if _, has := tools[idx].InputSchema.Properties["activate"]; has {
			t.Fatalf("%s must not advertise activate", name)
		}
		for _, flag := range cliVerbs[name].flags {
			if strings.TrimPrefix(flag, "b:") == elementActivateArg {
				t.Fatalf("%s must not register an activate flag", name)
			}
		}
	}
}

// assertDirectScript checks one set_value/perform_action script for the
// activation statement and rejects any synthesized-input escape hatch.
func assertDirectScript(t *testing.T, log string, body string, activate bool) {
	t.Helper()
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	front := ""
	if activate {
		front = "    set frontmost of process \"Background Demo\" to true\n"
	}
	want := "tell application \"System Events\"\n" + front + body + "end tell\n"
	if string(raw) != want {
		t.Fatalf("script = %q, want %q", raw, want)
	}
	for _, forbidden := range []string{"key code", "cliclick", "set focused"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("element action script must not contain %q: %q", forbidden, raw)
		}
	}
}

func TestElementActivationDirect(t *testing.T) {
	t.Run("set_value", func(t *testing.T) {
		for _, mode := range []string{"default", "true", "false"} {
			log := captureActionScripts(t)
			a := args{"element_index": json.Number("7"), "value": `say "hi"`}
			if mode != "default" {
				a["activate"] = mode == "true"
			}
			payload, terr := toolSetValue(elementActionState(), a)
			if terr != nil {
				t.Fatal(terr)
			}
			assertDirectScript(t, log,
				"    set value of (UI element 2 of (UI element 1 of (window 2 of process \"Background Demo\"))) to \"say \\\"hi\\\"\"\n",
				mode != "false")
			if mode == "false" {
				if got, _ := payload.(messagePayload).Result, ""; !strings.Contains(got, "without activating") {
					t.Fatalf("non-activating result must not claim a verified UI effect: %q", got)
				}
			}
		}
	})

	t.Run("perform_action", func(t *testing.T) {
		for _, mode := range []string{"default", "true", "false"} {
			log := captureActionScripts(t)
			a := args{"element_index": json.Number("7"), "action": "AXConfirm"}
			if mode != "default" {
				a["activate"] = mode == "true"
			}
			if _, terr := toolPerformAction(elementActionState(), a); terr != nil {
				t.Fatal(terr)
			}
			assertDirectScript(t, log,
				"    perform action \"AXConfirm\" of (UI element 2 of (UI element 1 of (window 2 of process \"Background Demo\")))\n",
				mode != "false")
		}
	})
}

func TestElementActivationErrors(t *testing.T) {
	t.Run("invalid action name", func(t *testing.T) {
		log := captureActionScripts(t)
		_, terr := toolPerformAction(elementActionState(), args{
			"element_index": json.Number("7"), "action": "drop table", "activate": false,
		})
		if terr == nil || terr.Code != "invalid_args" {
			t.Fatalf("error = %v, want invalid_args", terr)
		}
		if _, err := os.Stat(log); !os.IsNotExist(err) {
			t.Fatal("invalid action reached a subprocess")
		}
	})

	t.Run("AXRaise refused without activation", func(t *testing.T) {
		log := captureActionScripts(t)
		st := elementActionState()
		_, terr := toolPerformAction(st, args{
			"element_index": json.Number("7"), "action": "AXRaise", "activate": false,
		})
		if terr == nil || terr.Code != "invalid_args" {
			t.Fatalf("error = %v, want invalid_args", terr)
		}
		if _, err := os.Stat(log); !os.IsNotExist(err) {
			t.Fatal("refused AXRaise reached a subprocess")
		}
		if st.Stale {
			t.Fatal("refusal must not mutate state")
		}
	})

	t.Run("AXRaise still available with activation", func(t *testing.T) {
		log := captureActionScripts(t)
		if _, terr := toolPerformAction(elementActionState(), args{
			"element_index": json.Number("7"), "action": "AXRaise",
		}); terr != nil {
			t.Fatal(terr)
		}
		raw, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), `perform action "AXRaise"`) {
			t.Fatalf("AXRaise not dispatched: %q", raw)
		}
	})
}

// captureSelectionScripts makes the AX readback succeed with an empty selection,
// so selection proceeds to the range write (and, when active, the keyboard walk).
func captureSelectionScripts(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "scripts.log")
	t.Setenv("CUU_TEST_SCRIPTS", log)
	t.Setenv("ZCODE_CUA_DATA", t.TempDir())
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	stub := "#!/bin/sh\n" +
		"printf '%s\\n---\\n' \"$4\" >> \"$CUU_TEST_SCRIPTS\"\n" +
		"case \"$4\" in *AXSelectedText*) printf '\\037\\0370' ;; esac\n"
	if err := os.WriteFile(filepath.Join(dir, "osascript"), []byte(stub), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"cliclick", "pbcopy", "pbpaste", "screencapture", "open"} {
		blocked := "#!/bin/sh\nprintf 'unexpected desktop command: %s\\n' \"$0\" >> \"$CUU_TEST_SCRIPTS\"\nexit 1\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(blocked), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return log
}

func TestElementActivationSelection(t *testing.T) {
	newArgs := func() args {
		return args{"element_index": json.Number("7"), "start": json.Number("0"), "length": json.Number("3")}
	}

	t.Run("false attempts the AX range only", func(t *testing.T) {
		log := captureSelectionScripts(t)
		a := newArgs()
		a["activate"] = false
		_, terr := toolSelectText(elementActionState(), a)
		if terr == nil || terr.Code != "unsupported" {
			t.Fatalf("error = %v, want unsupported", terr)
		}
		if !strings.Contains(terr.Remedy, "keyboard fallback is disabled") {
			t.Fatalf("remedy must name the disabled fallback: %q", terr.Remedy)
		}
		raw, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"set frontmost", "set focused", "key code", "AXRaise"} {
			if strings.Contains(string(raw), forbidden) {
				t.Fatalf("non-activating selection emitted %q: %q", forbidden, raw)
			}
		}
	})

	t.Run("true keeps the keyboard fallback", func(t *testing.T) {
		log := captureSelectionScripts(t)
		a := newArgs()
		if _, terr := toolSelectText(elementActionState(), a); terr == nil || terr.Code != "unsupported" {
			t.Fatalf("error = %v, want unsupported once the walk fails", terr)
		}
		raw, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "key code 124") {
			t.Fatalf("activating selection must still walk with keys: %q", raw)
		}
		if !strings.Contains(string(raw), "set frontmost") {
			t.Fatalf("activating selection must focus the app: %q", raw)
		}
	})
}

func TestSynthesizedInputStillActivates(t *testing.T) {
	cases := map[string]func(*serverState) *ToolError{
		"press_key": func(st *serverState) *ToolError { return pressKeyChord(st, "cmd+s") },
		"type_text": func(st *serverState) *ToolError { return typeViaKeystrokes(st, "hi") },
		"keys": func(st *serverState) *ToolError {
			return axSelectViaKeys(st, `(window 2 of process "Background Demo")`, 1, 2)
		},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			log := captureActionScripts(t)
			if terr := run(elementActionState()); terr != nil {
				t.Fatal(terr)
			}
			raw, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), `set frontmost of process "Background Demo" to true`) {
				t.Fatalf("%s must keep targeting the frontmost app: %q", name, raw)
			}
		})
	}
}

func TestElementActivationCLIStrict(t *testing.T) {
	bin := buildBinary(t)
	t.Run("set_value honours false", func(t *testing.T) {
		log := captureActionScripts(t)
		saveState(elementActionState())
		out, err := exec.Command(bin, "set_value", "--element_index", "7", "--value", "v", "--activate=false").CombinedOutput()
		if err != nil {
			t.Fatalf("set_value CLI: %v\n%s", err, out)
		}
		raw, readErr := os.ReadFile(log)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.Contains(string(raw), "set frontmost") {
			t.Fatalf("activate=false must not focus: %q", raw)
		}
	})
	t.Run("invalid boolean is a usage error", func(t *testing.T) {
		saveState(elementActionState())
		err := exec.Command(bin, "set_value", "--element_index", "7", "--value", "v", "--activate=maybe").Run()
		if err == nil {
			t.Fatal("non-boolean --activate must fail")
		}
		exitErr, ok := err.(*exec.ExitError)
		if !ok || exitErr.ExitCode() != 2 {
			t.Fatalf("exit = %v, want 2 (usage)", err)
		}
	})
}
