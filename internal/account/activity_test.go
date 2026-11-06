package account

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStopWaitsForCanceledWork(t *testing.T) {
	a := NewActivity()
	ctx, done := a.Begin(context.Background(), "user")
	stopped := make(chan error, 1)
	go func() { stopped <- a.Stop(context.Background(), "user") }()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("work was not canceled")
	}
	select {
	case <-stopped:
		t.Fatal("stop returned before worker cleanup")
	default:
	}
	done()
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if err := a.Stop(context.Background(), "absent"); err != nil {
		t.Fatal(err)
	}
}
func TestStopDeadlineDoesNotPretendQuiescence(t *testing.T) {
	a := NewActivity()
	_, done := a.Begin(context.Background(), WorkKey("u", "r"))
	defer done()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.Stop(ctx, WorkKey("u", "r")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
