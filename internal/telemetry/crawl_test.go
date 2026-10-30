package telemetry

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zhaocore/dutybotgo/internal/config"
)

func TestConcurrentMeasurements(t *testing.T) {
	ctx, recorder := New(context.Background(), "run", "user", "task", time.Now().Add(-time.Second), config.ConcurrencyConfig{Worker: 4})
	var group sync.WaitGroup
	for range 20 {
		group.Go(func() {
			Measure(ctx, "capture", time.Now().Add(-10*time.Millisecond), errors.New("secret"))
			Count(ctx, "pages", 1)
			recorder.Snapshot("running")
		})
	}
	group.Wait()
	snapshot := recorder.Snapshot("failed")
	timing := snapshot.Timings["capture"]
	if timing.Count != 20 || timing.Failures != 20 || timing.TotalMS < 200 || timing.MaxMS < 10 || snapshot.QueueMS < 1000 || snapshot.Counters["pages"] != 20 {
		t.Fatal(snapshot)
	}
	snapshot.Counters["pages"] = 0
	delete(snapshot.Timings, "capture")
	if recorder.Snapshot("succeeded").Counters["pages"] != 20 || len(recorder.Snapshot("succeeded").Timings) != 1 {
		t.Fatal("snapshot aliases state")
	}
	if RunID(context.Background()) != "" || RunID(ctx) != "run" {
		t.Fatal("run identity")
	}
	if FromContext(context.Background()) != nil || FromContext(ctx) != recorder {
		t.Fatal("context isolation")
	}
	Measure(context.Background(), "ignored", time.Now(), nil)
	Count(context.Background(), "ignored", 1)
	Measure(ctx, "success", time.Now(), nil)
	if recorder.Snapshot("succeeded").Timings["success"].Failures != 0 {
		t.Fatal("success marked failed")
	}
}
