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
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthzAlwaysOK(t *testing.T) {
	s := &Server{}
	hs := newHealthServer(":0", s)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	hs.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("/healthz: want 200, got %d", rec.Code)
	}
}

func TestReadyzNotAllSynced(t *testing.T) {
	s := &Server{}
	// Only one informer synced — AllSynced() must return false.
	s.policySynced = true
	hs := newHealthServer(":0", s)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	hs.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("/readyz (partial sync): want 503, got %d", rec.Code)
	}
}

func TestReadyzAllSynced(t *testing.T) {
	s := &Server{}
	// All informers that AllSynced() checks must be true.
	s.policySynced = true
	s.netdefSynced = true
	s.nsSynced = true
	hs := newHealthServer(":0", s)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	hs.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("/readyz (all synced): want 200, got %d", rec.Code)
	}
}

func TestReadyzShuttingDown(t *testing.T) {
	s := &Server{}
	// All informers synced, but daemon is shutting down.
	s.policySynced = true
	s.netdefSynced = true
	s.nsSynced = true
	s.shuttingDown.Store(true)
	hs := newHealthServer(":0", s)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	hs.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("/readyz (shutting down): want 503, got %d", rec.Code)
	}
}

func TestHealthServerStartStop(t *testing.T) {
	s := &Server{}
	hs := newHealthServer("127.0.0.1:0", s)

	if err := hs.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	hs.Stop()
}

func TestOptionsValidateHealthPort(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		port    int
		wantErr bool
	}{
		{name: "disabled (0)", port: 0, wantErr: false},
		{name: "valid low (1)", port: 1, wantErr: false},
		{name: "valid high (8081)", port: 8081, wantErr: false},
		{name: "valid max (65535)", port: 65535, wantErr: false},
		{name: "negative (-1)", port: -1, wantErr: true},
		{name: "out of range (65536)", port: 65536, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			o := &Options{healthPort: tc.port}
			err := o.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("Validate() with healthPort=%d: want error, got nil", tc.port)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Validate() with healthPort=%d: want nil, got %v", tc.port, err)
			}
		})
	}
}

func TestOptionsValidateHealthBindAddress(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		addr    string
		wantErr bool
	}{
		{name: "empty (all interfaces)", addr: "", wantErr: false},
		{name: "loopback IPv4", addr: "127.0.0.1", wantErr: false},
		{name: "loopback IPv6", addr: "::1", wantErr: false},
		{name: "any IPv4", addr: "0.0.0.0", wantErr: false},
		{name: "invalid address", addr: "not-an-ip", wantErr: true},
		{name: "hostname (not IP)", addr: "localhost", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			o := &Options{healthBindAddress: tc.addr}
			err := o.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("Validate() with healthBindAddress=%q: want error, got nil", tc.addr)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Validate() with healthBindAddress=%q: want nil, got %v", tc.addr, err)
			}
		})
	}
}
