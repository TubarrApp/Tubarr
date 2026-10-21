package downloads

import (
	"sync"
	"time"
	"tubarr/internal/domain/consts"
)

// etaMinProgress and etaMinTime are how far a download must get before its ETA is given, as early rates jump about.
const (
	etaMinProgress = 3.0
	etaMinTime     = 6 * time.Second
)

// etaBaseline is when a download's progress was first seen, at what percentage, and the highest percentage since.
type etaBaseline struct {
	at      time.Time
	pct     float64
	highest float64
}

var (
	etaMu        sync.Mutex
	etaBaselines = map[int64]*etaBaseline{} // By video ID.
)

// trackETA records a video's progress for ETA, from its first bytes, restarting when its percentage drops (e.g. yt-dlp
// begins downloading the audio portion).
func trackETA(videoID int64, status consts.DownloadStatus, pct float64, now time.Time) {
	if videoID == 0 {
		return
	}
	etaMu.Lock()
	defer etaMu.Unlock()
	if status != consts.DLStatusDownloading {
		delete(etaBaselines, videoID)
		return
	}
	// Small dips are yt-dlp revising its size estimate, falling below half the highest is a new file.
	if b := etaBaselines[videoID]; b != nil && pct >= b.highest/2 {
		b.highest = max(b.highest, pct)
		return
	}
	delete(etaBaselines, videoID)
	if pct > 0 {
		etaBaselines[videoID] = &etaBaseline{at: now, pct: pct, highest: pct}
	}
}

// ETA returns the seconds a video's download has left at its average rate so far, or -1 if it's too early to say.
func ETA(videoID int64, pct float64) int64 {
	etaMu.Lock()
	b := etaBaselines[videoID]
	etaMu.Unlock()
	if b == nil {
		return -1
	}
	done, took := pct-b.pct, time.Since(b.at)
	if done < etaMinProgress || took < etaMinTime {
		return -1
	}
	return int64(took.Seconds() * (100 - pct) / done)
}
