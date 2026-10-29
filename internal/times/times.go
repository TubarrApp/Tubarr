// Package times provides utility functions related to times and timers.
package times

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/state"

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

// WaitTime adds a specified wait time before processing a channel, one of its URLs, or one of its videos.
func WaitTime(ctx context.Context, stagger time.Duration, channelName, channelURL, videoURL string) error {
	var target string
	switch {
	case videoURL != "":
		target = fmt.Sprintf("video %q for channel %q", videoURL, channelName)
	case channelURL != "":
		target = fmt.Sprintf("URL %q for channel %q", channelURL, channelName)
	default:
		target = fmt.Sprintf("channel %q", channelName)
	}

	if abstractions.GetBool(keys.SkipAllWaits) {
		logger.Pl.D(3, "Skipping wait time call for %s", target)
		return nil
	}

	fmt.Println()
	logger.Pl.I("Sleeping %v before processing %s", stagger.Round(time.Second), target)

	waitTimer := time.NewTimer(stagger)
	defer waitTimer.Stop()

	select {
	case <-waitTimer.C:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("context canceled during stagger wait for %s", target)
	}
}

// RandomSecsDuration returns a random duration between 0 and s seconds.
func RandomSecsDuration(s int) time.Duration {
	if s <= 0 {
		return 0
	}
	//nolint:gosec // Cryptographic randomness is unnecessary for timing delays.
	return time.Duration(rand.IntN(s+1)) * time.Second
}

// RandomMinsDuration returns a random duration between 0 and s minutes.
func RandomMinsDuration(s int) time.Duration {
	if s <= 0 {
		return 0
	}
	//nolint:gosec // Cryptographic randomness is unnecessary for timing delays.
	return time.Duration(rand.IntN(s+1)) * time.Minute
}

// CalculateJitter calculates a number to use as jitter.
func CalculateJitter(c *models.Channel) (jitter time.Duration) {
	jitterInt := state.GetOrComputeJitter(c.ID, len(c.URLModels), func() int {
		j := 5
		for _, u := range c.URLModels {
			url := strings.ToLower(u.URL)
			switch {
			case strings.Contains(url, "tube"), strings.Contains(url, "tok"),
				strings.Contains(url, "gram"), strings.Contains(url, "book"), strings.Contains(url, "x."):
				j = max(10, j)
			case strings.Contains(url, "motion"), strings.Contains(url, "vime"),
				strings.Contains(url, "bili"), strings.Contains(url, "ddit"):
				j = max(8, j)
			}
		}
		return j
	})

	if jitterInt > 0 {
		jitter = RandomMinsDuration(jitterInt) // Probability, with re-rolls, expect ~half the time added
	}

	return jitter
}
