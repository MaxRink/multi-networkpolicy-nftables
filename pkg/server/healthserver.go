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
	server *http.Server
}

// newHealthServer creates a new HealthServer bound to the given address.
// The s parameter is the main Server whose initialization state drives /readyz.
func newHealthServer(addr string, s *Server) *HealthServer {
	mux := http.NewServeMux()

	// /healthz — liveness: always 200 while the process is alive.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, "ok")
	})

	// /readyz — readiness: 200 once nftables state is initialized, 503 otherwise.
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if s.isInitialized() {
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
	}
}

// Start begins listening in a background goroutine. It returns an error if the
// listener cannot be created (e.g. address already in use).
func (h *HealthServer) Start() error {
	ln, err := net.Listen("tcp", h.server.Addr)
	if err != nil {
		return fmt.Errorf("health server failed to listen on %s: %w", h.server.Addr, err)
	}
	klog.Infof("Health server listening on %s", h.server.Addr)
	go func() {
		if serveErr := h.server.Serve(ln); serveErr != nil && serveErr != http.ErrServerClosed {
			klog.Errorf("Health server exited with error: %v", serveErr)
		}
	}()
	return nil
}

// Stop gracefully shuts down the health server.
func (h *HealthServer) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.server.Shutdown(ctx); err != nil {
		klog.Errorf("Health server shutdown error: %v", err)
	}
}
