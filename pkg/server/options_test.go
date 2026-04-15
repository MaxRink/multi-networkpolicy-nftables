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
	"sync"
	"testing"
	"time"
)

// TestStopDoubleCallNoDeadlock verifies that calling Stop() twice does not deadlock.
// Before the fix, stopCh was an unbuffered channel; a second Stop() would block
// forever because there was no receiver after the first signal was consumed.
func TestStopDoubleCallNoDeadlock(t *testing.T) {
	t.Parallel()

	o := NewOptions()

	done := make(chan struct{})
	go func() {
		defer close(done)
		o.Stop()
		o.Stop() // must not block
	}()

	select {
	case <-done:
		// success: both Stop() calls returned without deadlock
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() deadlocked on second call — buffered stopCh fix not applied")
	}
}

// TestStopConcurrentCallsNoPanic verifies that N goroutines calling Stop()
// concurrently neither deadlock nor panic. This exercises the race-free path
// enabled by the buffered stopCh: all callers hit the select default branch
// once the single buffer slot is occupied.
func TestStopConcurrentCallsNoPanic(t *testing.T) {
	t.Parallel()

	const goroutines = 10

	o := NewOptions()

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			o.Stop()
		}()
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		wg.Wait()
	}()

	select {
	case <-done:
		// success: all concurrent Stop() calls completed without deadlock or panic
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() deadlocked under concurrent callers — buffered stopCh fix not applied or has a race")
	}
}
