package server

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"

	"github.com/go-logr/logr"
	"k8s.io/klog/v2"
)

// logLevels maps --log-level to klog verbosity (-v). klog has no level that
// hides info lines, so warn and error both mean -v=0.
var logLevels = map[string]int{"debug": 4, "info": 2, "warn": 0, "error": 0}

// logOutput is where JSON lines go; a variable so that tests can capture them.
var logOutput io.Writer = os.Stderr

// validateLogging rejects unknown values of --log-level and --log-format.
func (o *Options) validateLogging() error {
	if _, ok := logLevels[o.logLevel]; !ok && o.logLevel != "" {
		return fmt.Errorf("log-level %q is invalid, must be one of debug, info, warn or error", o.logLevel)
	}
	if o.logFormat != "" && o.logFormat != "text" && o.logFormat != "json" {
		return fmt.Errorf("log-format %q is invalid, must be text or json", o.logFormat)
	}
	return nil
}

// applyLogging sets the klog verbosity from --log-level, if given, and sends
// klog output through a JSON handler for --log-format=json. It tolerates
// invalid values, so that the validation error is logged in the chosen format.
func (o *Options) applyLogging() error {
	if n, ok := logLevels[o.logLevel]; ok {
		var v klog.Level
		if err := v.Set(strconv.Itoa(n)); err != nil {
			return fmt.Errorf("set klog verbosity: %w", err)
		}
	}
	if o.JSONLogs() {
		// klog applies -v before a line reaches the handler, so the handler passes every level.
		// Unlike klog.SetSlogLogger, this keeps klog.Background and klog.FromContext behind -v.
		handler := slog.NewJSONHandler(logOutput, &slog.HandlerOptions{Level: slog.Level(-128)})
		klog.SetLoggerWithOptions(logr.FromSlogHandler(handler))
	}
	return nil
}

// JSONLogs reports whether --log-format=json was given.
func (o *Options) JSONLogs() bool {
	return o.logFormat == "json"
}
