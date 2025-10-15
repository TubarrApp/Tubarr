// Package times provides utility functions related to times and timers.
package times

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/utils/logging"

	"github.com/spf13/viper"
)

// StartupWait adds a 0–30 minute wait time with a visible countdown.
// This is used to help avoid bot detection.
func StartupWait(ctx context.Context) error {
	if viper.GetBool(keys.SkipWait) {
		logging.W("Skipping wait period, running Tubarr immediately. You may encounter bot detection on some platforms if requests come at predictable intervals.")
		return nil
	}

	// Add random stagger on startup (0–30 minutes)
	stagger := RandomMinsDuration(30)
	logging.I("Waiting %v before channel check (helps hide from bot detection). To skip startup jitter, use:\n\ntubarr -s\n", stagger.Round(time.Second))

	ticker := time.NewTicker(1 * time.Second)
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
					fmt.Print(consts.ClearLine)
					return
				}
				m := int(remaining.Minutes())
				s := int(remaining.Seconds()) % 60
				fmt.Print(consts.ClearLine)
				fmt.Printf("\r%s %dm%ds", consts.TimeRemainingMsg, m, s)
			case <-ctx.Done():
				fmt.Print(consts.ClearLine)
				return
			}
		}
	}()

	select {
	case <-waitTimer.C:
		fmt.Print(consts.ClearLine)
		<-done
		return nil
	case <-ctx.Done():
		<-done
		return ctx.Err()
	}
}

// WaitTime adds a specified wait time.
func WaitTime(ctx context.Context, stagger time.Duration, channelName, videoURL string) error {
	if viper.GetBool(keys.SkipWait) {
		return nil
	}

	if videoURL == "" {
		logging.I("Sleeping %v before processing channel %q", stagger.Round(time.Second), channelName)
	} else {
		logging.I("Sleeping %v before processing video %q for channel %q", stagger.Round(time.Second), videoURL, channelName)
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
