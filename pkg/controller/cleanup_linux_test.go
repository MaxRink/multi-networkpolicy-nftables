//go:build linux

package controller

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	klog "k8s.io/klog/v2"
)

// TestDebugLogWritesOnlyThroughKlog checks that a cleanup line goes through the
// klog logger (so that --log-format=json applies) and not also as text to stderr.
func TestDebugLogWritesOnlyThroughKlog(t *testing.T) {
	var buf bytes.Buffer
	klog.SetLoggerWithOptions(logr.FromSlogHandler(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(klog.ClearLogger)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = w
	debugLog("", "cleanup: removing rules for %s/%s", "ns", "pod")
	os.Stderr = saved
	_ = w.Close()
	var stderr bytes.Buffer
	_, _ = stderr.ReadFrom(r)

	if stderr.Len() != 0 {
		t.Errorf("debugLog wrote to stderr: %q", stderr.String())
	}
	var rec struct{ Msg string }
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil || strings.Count(buf.String(), "\n") != 1 {
		t.Fatalf("want one JSON line through klog, got %q (%v)", buf.String(), err)
	}
	if rec.Msg != "cleanup: removing rules for ns/pod" {
		t.Errorf("msg %q", rec.Msg)
	}
}
