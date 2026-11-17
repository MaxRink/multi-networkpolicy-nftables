package server

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"k8s.io/klog/v2"
)

func TestValidateLogging(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		level, format string
		ok            bool
	}{
		{"", "", true},
		{"", "text", true},
		{"debug", "json", true},
		{"info", "text", true},
		{"warn", "text", true},
		{"error", "json", true},
		{"warning", "text", false},
		{"INFO", "text", false},
		{"4", "text", false},
		{"info", "logfmt", false},
		{"info", "JSON", false},
	} {
		o := &Options{logLevel: tt.level, logFormat: tt.format}
		if err := o.validateLogging(); (err == nil) != tt.ok {
			t.Errorf("level %q format %q: error %v, want ok=%t", tt.level, tt.format, err, tt.ok)
		}
	}
}

// TestApplyLogging changes the global klog state, so it must not run in parallel.
func TestApplyLogging(t *testing.T) {
	var buf bytes.Buffer
	saved := logOutput
	logOutput = &buf
	t.Cleanup(func() {
		logOutput = saved
		klog.ClearLogger()
		var v klog.Level
		_ = v.Set("0")
	})

	for _, tt := range []struct {
		level string
		want  []string
	}{
		{"info", []string{"v2", "info", "warning", "error"}},
		{"warn", []string{"info", "warning", "error"}},
		{"error", []string{"info", "warning", "error"}},
		{"debug", []string{"v4", "ctx-v4", "v2", "info", "warning", "error"}},
	} {
		buf.Reset()
		o := &Options{logLevel: tt.level, logFormat: "json"}
		if err := o.applyLogging(); err != nil {
			t.Fatal(err)
		}
		klog.V(5).Info("v5")
		klog.V(4).Info("v4")
		klog.Background().V(5).Info("ctx-v5")
		klog.Background().V(4).Info("ctx-v4")
		klog.V(2).Info("v2")
		klog.Info("info")
		klog.Warning("warning")
		klog.Error("error")
		klog.Flush()

		var got []string
		for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
			var rec struct{ Msg string }
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				t.Fatalf("%s: line is not JSON: %q", tt.level, line)
			}
			got = append(got, rec.Msg)
		}
		if strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Errorf("%s: messages %v, want %v", tt.level, got, tt.want)
		}
	}
}
