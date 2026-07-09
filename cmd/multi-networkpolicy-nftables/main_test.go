/*
Copyright 2026 Deutsche Telekom AG.

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

package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeManagerStarter struct {
	startErr error
	start    func()
	started  bool
}

func (f *fakeManagerStarter) Start(context.Context) error {
	f.started = true
	if f.start != nil {
		f.start()
	}
	return f.startErr
}

func TestStartManagerAndCleanupRunsCleanupAfterNormalStop(t *testing.T) {
	mgr := &fakeManagerStarter{}
	cleanupCalled := false

	err := startManagerAndCleanup(context.Background(), mgr, time.Second, func(context.Context) error {
		cleanupCalled = true
		return nil
	})

	if err != nil {
		t.Fatalf("startManagerAndCleanup() error = %v, want nil", err)
	}
	if !mgr.started {
		t.Fatal("manager was not started")
	}
	if !cleanupCalled {
		t.Fatal("cleanup was not called")
	}
}

func TestStartManagerAndCleanupSkipsCleanupAfterStartError(t *testing.T) {
	startErr := errors.New("start failed")
	mgr := &fakeManagerStarter{startErr: startErr}
	cleanupCalled := false

	err := startManagerAndCleanup(context.Background(), mgr, time.Second, func(context.Context) error {
		cleanupCalled = true
		return nil
	})

	if !errors.Is(err, startErr) {
		t.Fatalf("startManagerAndCleanup() error = %v, want start error", err)
	}
	if cleanupCalled {
		t.Fatal("cleanup was called after start error")
	}
}

func TestStartManagerAndCleanupRunsCleanupAfterShutdownError(t *testing.T) {
	startErr := errors.New("graceful shutdown timed out")
	cleanupErr := errors.New("cleanup failed")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := &fakeManagerStarter{
		startErr: startErr,
		start:    cancel,
	}
	cleanupCalled := false

	err := startManagerAndCleanup(ctx, mgr, time.Second, func(context.Context) error {
		cleanupCalled = true
		return cleanupErr
	})

	if !errors.Is(err, startErr) {
		t.Fatalf("startManagerAndCleanup() error = %v, want start error", err)
	}
	if !errors.Is(err, cleanupErr) {
		t.Fatalf("startManagerAndCleanup() error = %v, want cleanup error", err)
	}
	if !cleanupCalled {
		t.Fatal("cleanup was not called after shutdown error")
	}
}

func TestStartManagerAndCleanupReturnsCleanupErrorAfterNormalStop(t *testing.T) {
	cleanupErr := errors.New("cleanup failed")
	mgr := &fakeManagerStarter{}

	err := startManagerAndCleanup(context.Background(), mgr, time.Second, func(context.Context) error {
		return cleanupErr
	})

	if !errors.Is(err, cleanupErr) {
		t.Fatalf("startManagerAndCleanup() error = %v, want cleanup error", err)
	}
}
