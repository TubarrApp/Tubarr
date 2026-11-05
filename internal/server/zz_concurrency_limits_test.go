package server

import (
	"net/http"
	"net/url"
	"testing"
	"tubarr/internal/limits"
)

// TestConcurrencySettingsResizeInPlace tests that saving a global limit changes the semaphore in use, counting running
// work, rather than leaving it behind.
func TestConcurrencySettingsResizeInPlace(t *testing.T) {
	setupFuzzServer(t)

	for path, sem := range map[string]*limits.Semaphore{
		"/api/v1/settings/crawl-concurrency":           limits.GlobalCrawls,
		"/api/v1/settings/global-download-concurrency": limits.GlobalDownloads,
		"/api/v1/settings/post-process-concurrency":    limits.GlobalPostProcessing,
	} {
		t.Run(path, func(t *testing.T) {
			setLimit := func(limit string) {
				t.Helper()
				if w := sendForm(t, http.MethodPut, path, url.Values{"limit": {limit}}); w.Code != http.StatusOK {
					t.Fatalf("setting %s to %s = %d %s", path, limit, w.Code, w.Body.String())
				}
			}
			setLimit("1")
			t.Cleanup(func() { setLimit("0") })

			// The limit is used up by one running.
			if !sem.TryAcquire() {
				t.Fatal("no slot free under a limit of 1")
			}
			defer sem.Release()
			if sem.TryAcquire() {
				sem.Release()
				t.Fatal("second slot given out under a limit of 1")
			}

			// Saving it again leaves it used up.
			setLimit("1")
			if sem.TryAcquire() {
				sem.Release()
				t.Fatal("re-saving the limit gave out an extra slot")
			}

			// Raising it frees one more, with the first still counted.
			setLimit("2")
			if !sem.TryAcquire() {
				t.Fatal("raising the limit freed no slot")
			}
			defer sem.Release()
			if sem.TryAcquire() {
				sem.Release()
				t.Fatal("raising the limit stopped counting the running slot")
			}
		})
	}
}

// TestDomainLimitSettingKeepsOtherKindsSlots tests that changing a domain's crawl limit leaves its download slots counted.
func TestDomainLimitSettingKeepsOtherKindsSlots(t *testing.T) {
	setupFuzzServer(t)

	const (
		path     = "/api/v1/settings/domain-limits"
		host     = "resize.limit-test.example"
		videoURL = "https://resize.limit-test.example/video"
	)
	setLimits := func(download, crawl string) {
		t.Helper()
		if w := sendForm(t, http.MethodPut, path, url.Values{"hostname": {host}, "download": {download}, "crawl": {crawl}}); w.Code != http.StatusOK {
			t.Fatalf("setting domain limits = %d %s", w.Code, w.Body.String())
		}
	}
	setLimits("1", "1")
	t.Cleanup(func() { setLimits("0", "0") })

	sem, err := limits.DomainDownloads.Acquire(t.Context(), videoURL)
	if err != nil {
		t.Fatal(err)
	}
	defer sem.Release()

	// Only the crawl limit changes, and the download slot is still taken.
	setLimits("1", "5")
	if sem.TryAcquire() {
		sem.Release()
		t.Fatal("changing the crawl limit gave out an extra download slot")
	}
}
