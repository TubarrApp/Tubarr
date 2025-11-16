// Package times provides utility functions related to times and timers.
package times

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"

	"github.com/TubarrApp/gocommon/abstractions"
	"github.com/TubarrApp/gocommon/sharedconsts"
)

// StartupWait adds a 0–30 minute wait time with a visible countdown.
// This is used to help avoid bot detection.
func StartupWait(ctx context.Context) error {
	if abstractions.GetBool(keys.SkipInitialWait) || abstractions.GetBool(keys.SkipAllWaits) {
		logger.Pl.W("Skipping wait period, running Tubarr immediately. You may encounter bot detection on some platforms if requests come at predictable intervals.")
		return nil
	}

	// Add random stagger on startup (0–30 minutes)
	stagger := RandomMinsDuration(consts.DefaultStartupStaggerMinutes)
	logger.Pl.I("Waiting %v before beginning channel check (helps hide cronjobs from bot detection). To skip startup jitter, use:\n\ntubarr -s\n", stagger.Round(time.Second))

	ticker := time.NewTicker(consts.CountdownTickInterval)
	defer ticker.Stop()

	waitTimer := time.NewTimer(stagger)
	defer waitTimer.Stop()

	endTime := time.Now().Add(stagger)
	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			select {
			case <-ticker.C:
				remaining := time.Until(endTime)
				if remaining <= 0 {
					fmt.Print(sharedconsts.ClearLine)
					return
				}
				m := int(remaining.Minutes())
				s := int(remaining.Seconds()) % 60
				fmt.Print(sharedconsts.ClearLine)
				logger.Pl.P("\r%s %dm%ds", consts.TimeRemainingMsg, m, s)
			case <-ctx.Done():
				fmt.Print(sharedconsts.ClearLine)
				return
			}
		}
	}()

	select {
	case <-waitTimer.C:
		fmt.Print(sharedconsts.ClearLine)
		<-done
		return nil
	case <-ctx.Done():
		<-done
		return ctx.Err()
	}
}

// WaitTime adds a specified wait time.
func WaitTime(ctx context.Context, stagger time.Duration, channelName, videoURL string) error {
	if abstractions.GetBool(keys.SkipAllWaits) {
		if videoURL == "" {
			logger.Pl.D(3, "Skipping wait time call for channel %q (video URL: %q)", channelName, videoURL)
		} else {
			logger.Pl.D(3, "Skipping wait time call for channel %q", channelName)
		}
		return nil
	}

	if videoURL == "" {
		fmt.Println()
		logger.Pl.I("Sleeping %v before processing channel %q", stagger.Round(time.Second), channelName)
	} else {
		fmt.Println()
		logger.Pl.I("Sleeping %v before processing video %q for channel %q", stagger.Round(time.Second), videoURL, channelName)
	}

	waitTimer := time.NewTimer(stagger)
	defer waitTimer.Stop()

	select {
	case <-waitTimer.C:
		return nil
	case <-ctx.Done():
		if videoURL == "" {
			return fmt.Errorf("context cancelled during stagger wait for channel %q", channelName)
		}
		return fmt.Errorf("context cancelled during stagger wait for video %q in channel %q", videoURL, channelName)
	}
}

// RandomSecsDuration returns a random duration between 0 and s seconds.
func RandomSecsDuration(s int) time.Duration {
	if s <= 0 {
		return 0
	}
	return time.Duration(rand.IntN(s+1)) * time.Second
}

// RandomMinsDuration returns a random duration between 0 and s minutes.
func RandomMinsDuration(s int) time.Duration {
	if s <= 0 {
		return 0
	}
	return time.Duration(rand.IntN(s+1)) * time.Minute
}
