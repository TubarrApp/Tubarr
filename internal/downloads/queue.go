package downloads

import (
	"context"
	"sync"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
	"tubarr/internal/state"
)

// queuedVideo is a video accepted for download, which the Downloads page can cancel before and while it runs.
type queuedVideo struct {
	mu      sync.Mutex
	ctx     context.Context
	tracked bool
}

// queue holds the videos accepted for download, by video ID.
var queue sync.Map // Video ID -> *queuedVideo.

// Enqueue shows a stored video on the Downloads page as queued at the given stage, cancelable before it starts.
//
// A video already queued only has its stage updated, unless it's tracked by its job or was canceled.
func Enqueue(v *models.Video, stage string) {
	if v.ID == 0 {
		return
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	q := &queuedVideo{ctx: ctx}
	if existing, loaded := queue.LoadOrStore(v.ID, q); loaded {
		cancel(nil)
		if q, _ = existing.(*queuedVideo); q == nil {
			return
		}
	} else {
		id := v.ID
		RegisterDownloadContext(id, v.URL, func(cause error) {
			cancel(cause)
			q.mu.Lock()
			defer q.mu.Unlock()
			if !q.tracked { // A tracked video leaves the page once its job sees the cancel.
				state.DeleteStatusUpdate(id)
			}
		})
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if q.tracked || q.ctx.Err() != nil {
		return
	}
	state.SetStatusUpdate(v.ID, models.StatusUpdate{
		VideoID:      v.ID,
		VideoTitle:   v.Title,
		ChannelID:    v.ChannelID,
		ChannelURLID: v.ChannelURLID,
		ChannelURL:   v.ChannelURL,
		VideoURL:     v.URL,
		Status:       consts.DLStatusQueued,
		Stage:        stage,
	})
}

// Start returns the context of a video its job is starting, which canceling it from the Downloads page cancels. A
// video not yet queued is queued first.
//
// Returns true if the video was canceled while it waited.
func Start(v *models.Video) (ctx context.Context, wasCanceled bool) {
	if v.ID == 0 {
		return context.Background(), false
	}
	Enqueue(v, "Waiting to start")
	q := queued(v)
	if q == nil {
		return context.Background(), false
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if q.ctx.Err() != nil {
		return nil, true
	}
	return q.ctx, false
}

// Tracked marks a queued video as shown on the Downloads page by its job's download tracker, which then removes it.
func Tracked(v *models.Video) {
	if q := queued(v); q != nil {
		q.mu.Lock()
		q.tracked = true
		q.mu.Unlock()
	}
}

// Dequeue removes a video from the queue once its job is done with it, and from the Downloads page if never tracked there.
func Dequeue(v *models.Video) {
	value, ok := queue.LoadAndDelete(v.ID)
	if !ok {
		return
	}
	UnregisterDownloadContext(v.ID, v.URL)
	q, _ := value.(*queuedVideo)
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.tracked {
		state.DeleteStatusUpdateIf(v.ID, consts.DLStatusQueued)
	}
}

// queued returns a video's queue entry, nil if it isn't queued.
func queued(v *models.Video) *queuedVideo {
	value, _ := queue.Load(v.ID)
	q, _ := value.(*queuedVideo)
	return q
}
