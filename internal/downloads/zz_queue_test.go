package downloads

import (
	"context"
	"errors"
	"testing"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/state"

	"github.com/TubarrApp/gocommon/logging"
)

// quietLogger swaps in a logger writing to a buffer for the test.
func quietLogger(t *testing.T) {
	origLogger := logger.Pl
	logger.Pl = &logging.ProgramLogger{Console: &lockedBuffer{}}
	t.Cleanup(func() { logger.Pl = origLogger })
}

// TestQueueCancelWhileWaiting tests a queued video shows on the Downloads page, and canceling it before it starts takes
// it off the page and stops its job starting it.
func TestQueueCancelWhileWaiting(t *testing.T) {
	quietLogger(t)
	v := &models.Video{ID: 9101, ChannelID: 1, URL: "https://example.com/v/waiting"}
	t.Cleanup(func() { Dequeue(v); state.DeleteStatusUpdate(v.ID) })

	Enqueue(v, "Waiting for the channel's running job")
	if got := state.GetStatusUpdate(v.ID); got.Status != consts.DLStatusQueued || got.Stage != "Waiting for the channel's running job" {
		t.Fatalf("queued video's status = %+v, want Queued at the given stage", got)
	}
	Enqueue(v, "Waiting to start")
	if got := state.GetStatusUpdate(v.ID); got.Stage != "Waiting to start" {
		t.Errorf("re-queued video's stage = %q, want it updated", got.Stage)
	}

	if !CancelDownloadByVideoID(v.ID, v.URL) {
		t.Fatal("expected the queued video to be cancelable")
	}
	if got := state.GetStatusUpdate(v.ID); got.Status != "" {
		t.Errorf("canceled waiting video still on the Downloads page: %+v", got)
	}
	Enqueue(v, "Waiting to start")
	if got := state.GetStatusUpdate(v.ID); got.Status != "" {
		t.Errorf("queuing a canceled video put it back on the Downloads page: %+v", got)
	}
	if _, wasCanceled := Start(v); !wasCanceled {
		t.Error("expected Start to report the video was canceled while waiting")
	}
}

// TestQueueCancelTracked tests canceling a video its job tracks cancels its context, leaving it on the Downloads page
// for its job to finish with.
func TestQueueCancelTracked(t *testing.T) {
	quietLogger(t)
	v := &models.Video{ID: 9102, ChannelID: 1, URL: "https://example.com/v/tracked"}
	t.Cleanup(func() { Dequeue(v); state.DeleteStatusUpdate(v.ID) })

	Enqueue(v, "Waiting to start")
	ctx, wasCanceled := Start(v)
	if wasCanceled {
		t.Fatal("expected the video to start")
	}
	Tracked(v)

	if !CancelDownloadByVideoID(v.ID, v.URL) {
		t.Fatal("expected the started video to be cancelable")
	}
	if !errors.Is(context.Cause(ctx), ErrCanceledByUser) {
		t.Errorf("started video's context cause = %v, want ErrCanceledByUser", context.Cause(ctx))
	}
	if got := state.GetStatusUpdate(v.ID); got.Status != consts.DLStatusQueued {
		t.Errorf("tracked video removed from the Downloads page by the cancel, got %+v", got)
	}
	Dequeue(v)
	if got := state.GetStatusUpdate(v.ID); got.Status != consts.DLStatusQueued {
		t.Errorf("dequeuing a tracked video removed it from the Downloads page, got %+v", got)
	}
}

// TestQueueDequeueUnstarted tests a video its job never started leaves the Downloads page when dequeued.
func TestQueueDequeueUnstarted(t *testing.T) {
	quietLogger(t)
	v := &models.Video{ID: 9103, ChannelID: 1, URL: "https://example.com/v/unstarted"}
	t.Cleanup(func() { Dequeue(v); state.DeleteStatusUpdate(v.ID) })

	Enqueue(v, "Waiting to start")
	Dequeue(v)
	if got := state.GetStatusUpdate(v.ID); got.Status != "" {
		t.Errorf("unstarted video still on the Downloads page after dequeuing: %+v", got)
	}
	if CancelDownloadByVideoID(v.ID, v.URL) {
		t.Error("expected a dequeued video to have no cancel registered")
	}
}
