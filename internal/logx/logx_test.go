package logx

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureSink writes to a temp file so we can read back what was
// rendered. We can't redirect to a *bytes.Buffer because the public
// constructor takes *os.File specifically — keeps the public surface
// honest about where logs land.
func captureSink(t *testing.T) (*Sink, func() string) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "logx-*.log")
	if err != nil {
		t.Fatalf("temp: %v", err)
	}
	s := New(16, f)
	return s, func() string {
		_ = f.Sync()
		b, err := os.ReadFile(filepath.Join(f.Name()))
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		return string(b)
	}
}

func TestFormat_TextDefault(t *testing.T) {
	s, read := captureSink(t)
	s.Log(LevelInfo, "test.event", "k", "v", "n", 42)
	out := read()
	if !strings.Contains(out, "INFO test.event") {
		t.Fatalf("missing INFO line: %q", out)
	}
	if !strings.Contains(out, "k=v") || !strings.Contains(out, "n=42") {
		t.Fatalf("missing attrs: %q", out)
	}
}

func TestFormat_JSONOptIn(t *testing.T) {
	s, read := captureSink(t)
	s.SetFormat(FormatJSON)
	s.Log(LevelWarn, "stripe.webhook.bad_sig", "ip", "1.2.3.4", "err", "decode fail")

	out := strings.TrimSpace(read())
	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("JSON parse: %v (raw=%q)", err, out)
	}
	if obj["level"] != "WARN" {
		t.Fatalf("level = %v, want WARN", obj["level"])
	}
	if obj["msg"] != "stripe.webhook.bad_sig" {
		t.Fatalf("msg = %v", obj["msg"])
	}
	if obj["ip"] != "1.2.3.4" {
		t.Fatalf("ip attr missing or wrong: %v", obj["ip"])
	}
	if obj["err"] != "decode fail" {
		t.Fatalf("err attr missing: %v", obj["err"])
	}
	if _, ok := obj["ts"]; !ok {
		t.Fatalf("ts missing")
	}
}

func TestFormat_JSONReservedKeyClash(t *testing.T) {
	s, read := captureSink(t)
	s.SetFormat(FormatJSON)
	// "msg" as an attr key would clobber the entry's own msg. Should be
	// renamed with an "attr." prefix.
	s.Log(LevelInfo, "evt", "msg", "shadowing", "ok", true)

	out := strings.TrimSpace(read())
	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if obj["msg"] != "evt" {
		t.Fatalf("msg got shadowed: %v", obj["msg"])
	}
	if obj["attr.msg"] != "shadowing" {
		t.Fatalf("renamed attr missing: %v", obj["attr.msg"])
	}
}

func TestFormat_RuntimeFlip(t *testing.T) {
	s, read := captureSink(t)
	s.Log(LevelInfo, "before")
	s.SetFormat(FormatJSON)
	s.Log(LevelInfo, "after")

	lines := strings.Split(strings.TrimSpace(read()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], "INFO before") {
		t.Fatalf("first line not text-format: %q", lines[0])
	}
	if !bytes.HasPrefix([]byte(lines[1]), []byte(`{`)) {
		t.Fatalf("second line not JSON: %q", lines[1])
	}
}
