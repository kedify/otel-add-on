package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
)

func TestHAQueryServer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var ready atomic.Bool
	started := make(chan struct{}, 5)
	stopped := make(chan struct{}, 5)
	done := make(chan error, 1)
	go func() {
		done <- runHAQueryServer(ctx, func() error {
			if ready.Load() {
				return nil
			}
			return errors.New("warming")
		}, func(serverCtx context.Context) error {
			started <- struct{}{}
			<-serverCtx.Done()
			stopped <- struct{}{}
			return grpc.ErrServerStopped
		})
	}()
	select {
	case <-started:
		t.Fatal("served before warmup")
	case <-time.After(50 * time.Millisecond):
	}
	wait := func(ch chan struct{}) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(3 * time.Second):
			t.Fatal("query listener did not transition")
		}
	}
	ready.Store(true)
	wait(started)
	ready.Store(false)
	wait(stopped)
	select {
	case <-started:
		t.Fatal("restarted while unready")
	case <-time.After(50 * time.Millisecond):
	}
	ready.Store(true)
	wait(started)
	cancel()
	wait(stopped)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown leaked readiness watcher")
	}
	failure := errors.New("listen failed")
	if err := runHAQueryServer(context.Background(), func() error { return nil }, func(context.Context) error { return failure }); !errors.Is(err, failure) {
		t.Fatalf("listener failure was swallowed: %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if err := runHAQueryServer(ctx, func() error { return errors.New("warming") }, func(context.Context) error { t.Fatal("served canceled context"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
