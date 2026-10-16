package server

import "testing"

// TestParseURLSettingsNumbers tests that numeric per-URL settings sent as JSON numbers are kept.
func TestParseURLSettingsNumbers(t *testing.T) {
	const raw = `{"https://example.com/c":{
		"settings":{"max_concurrency":3,"crawl_freq":45,"download_retries":2,"paused":true},
		"metarr":{"metarr_concurrency":4,"metarr_max_cpu_usage":50.5}}}`

	parsed, err := parseURLSettings(raw)
	if err != nil {
		t.Fatalf("parseURLSettings: %v", err)
	}
	got, ok := parsed["https://example.com/c"]
	if !ok || got.Settings == nil || got.Metarr == nil {
		t.Fatalf("parsed = %+v, want settings and metarr for the URL", parsed)
	}
	if s := got.Settings; s.Concurrency != 3 || s.CrawlFreq != 45 || s.Retries != 2 || !s.Paused {
		t.Errorf("settings = concurrency %d, crawl freq %d, retries %d, paused %v; want 3, 45, 2, true",
			s.Concurrency, s.CrawlFreq, s.Retries, s.Paused)
	}
	if m := got.Metarr; m.Concurrency != 4 || m.MaxCPU != 50.5 {
		t.Errorf("metarr = concurrency %d, max CPU %v; want 4, 50.5", m.Concurrency, m.MaxCPU)
	}
}

// TestSplitAuthDetails tests that a JSON array keeps values containing spaces, and the old space-separated form still works.
func TestSplitAuthDetails(t *testing.T) {
	got, err := splitAuthDetails(`[{"username":"a b","password":"correct horse"},{"username":"c"}]`)
	if err != nil || len(got) != 2 || got[0] != `{"username":"a b","password":"correct horse"}` {
		t.Fatalf("splitAuthDetails(array) = %q, %v", got, err)
	}
	if got, err := splitAuthDetails(`{"username":"a"} {"username":"b"}`); err != nil || len(got) != 2 {
		t.Fatalf("splitAuthDetails(legacy) = %q, %v", got, err)
	}
}

// TestParseURLSettingsRejectsBadType tests that a wrongly typed value is an error rather than silently dropped.
func TestParseURLSettingsRejectsBadType(t *testing.T) {
	if _, err := parseURLSettings(`{"https://example.com/c":{"settings":{"max_concurrency":"three"}}}`); err == nil {
		t.Fatal("parseURLSettings accepted a string concurrency, want an error")
	}
}
