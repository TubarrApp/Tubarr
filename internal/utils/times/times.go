// Package times provides utility functions related to times and timers.
package times

import (
	"context"
	"fmt"
	"math/rand"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/utils/logging"

	"github.com/spf13/viper"
)

// StartupWait adds a 0 to 30 minute wait time with a visible countdown.
// This is used to help avoid bot detection.
func StartupWait(ctx context.Context) error {
	if viper.GetBool(keys.SkipWait) {
		return nil
	}

	// Add random stagger on startup (0-30 minutes)
	stagger := ReturnMinutes(31) // 0 to 30 minutes
	logging.I("Waiting %v before channel check (helps hide from bot detection). To skip startup jitter, use:\n\ntubarr -s\n", stagger.Round(time.Second))

	// Countdown display
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

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

	// Wait for the stagger delay
	select {
	case <-time.After(stagger):
		fmt.Print(consts.ClearLine)
		<-done // Wait for goroutine to clean up
		return nil
	case <-ctx.Done():
		<-done // Wait for goroutine to clean up
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

	ticker := time.NewTimer(stagger)
	defer ticker.Stop()

	select {
	case <-ticker.C:
		return nil
	case <-ctx.Done():
		if videoURL == "" {
			return fmt.Errorf("context cancelled during stagger wait for channel %q", channelName)
		}
		return fmt.Errorf("context cancelled during stagger wait for video %q in channel %q", videoURL, channelName)
	}
}

// ReturnSeconds returns a time.Duration in seconds.
func ReturnSeconds(s int) time.Duration {
	return time.Duration(rand.Intn(s)) * time.Second
}

// ReturnMinutes returns a time.Duration in minutes.
func ReturnMinutes(s int) time.Duration {
	return time.Duration(rand.Intn(s)) * time.Second
}
