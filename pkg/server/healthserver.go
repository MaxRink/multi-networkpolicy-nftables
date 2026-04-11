/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"k8s.io/klog"
)

// HealthServer provides HTTP health and readiness endpoints for the daemon.
type HealthServer struct {
	server  *http.Server
	serving chan struct{} // closed once Serve() begins accepting connections
	ln      net.Listener  // stored so Stop() can close it on shutdown
}

// newHealthServer creates a new HealthServer bound to the given address.
// The s parameter is the main Server whose state drives /readyz.
func newHealthServer(addr string, s *Server) *HealthServer {
	mux := http.NewServeMux()

	// /healthz — liveness: always 200 while the process is alive.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, "ok")
	})

	// /readyz — readiness: 200 only when the server is fully initialised AND we are
	// not shutting down.  Returns 503 in all other cases.
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if s.shuttingDown.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprintln(w, "shutting down")
			return
		}
		if s.ready.Load() {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintln(w, "ok")
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprintln(w, "not ready")
		}
	})

	return &HealthServer{
		server: &http.Server{
			Addr:              addr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		},
		serving: make(chan struct{}),
	}
}

// Start begins listening in a background goroutine. It returns an error if the
// listener cannot be created (e.g. address already in use).
// The method blocks only until the listener is bound; actual request serving
// happens asynchronously.  Stop() is safe to call immediately after Start()
// returns because it waits for the serving goroutine to begin before issuing
// Shutdown.
func (h *HealthServer) Start() error {
	ln, err := net.Listen("tcp", h.server.Addr)
	if err != nil {
		return fmt.Errorf("health server failed to listen on %s: %w", h.server.Addr, err)
	}
	h.ln = ln
	klog.Infof("Health server listening on %s", ln.Addr().String())
	go func() {
		close(h.serving) // signal that Serve() is about to be called
		if serveErr := h.server.Serve(ln); serveErr != nil && serveErr != http.ErrServerClosed {
			klog.Errorf("Health server exited with error: %v", serveErr)
		}
	}()
	return nil
}

// Stop gracefully shuts down the health server.  It waits until Start() has
// begun serving before calling Shutdown so that the two never race.
// Stop must only be called after a successful call to Start().
func (h *HealthServer) Stop() {
	<-h.serving

	if h.ln != nil {
		_ = h.ln.Close()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.server.Shutdown(ctx); err != nil {
		klog.Errorf("Health server shutdown error: %v", err)
	}
}
