package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

func TestSharedPayloadCapCannotBeExceededByConcurrentWorkers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var total atomic.Uint64
	output := sink{total: &total, limit: 10003, cancel: cancel}
	var workers sync.WaitGroup
	for range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 100 {
				if _, err := output.Write(make([]byte, 257)); err != nil {
					return
				}
			}
		}()
	}
	workers.Wait()
	if total.Load() != 10003 {
		t.Fatalf("counted payload=%d", total.Load())
	}
	if ctx.Err() == nil {
		t.Fatal("cap did not cancel outstanding requests")
	}
	if n, err := output.Write([]byte("extra")); n != 0 || err == nil {
		t.Fatal("accepted bytes after cap")
	}
}
