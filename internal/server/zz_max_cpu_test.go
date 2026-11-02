package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestMaxCPURoundTrip tests the edit form gets back a Max CPU its 0-100 field can hold, or the browser won't submit the
// form, and that saving it again keeps it.
func TestMaxCPURoundTrip(t *testing.T) {
	setupFuzzServer(t)
	form := webChannelForm(t, "Max CPU Round Trip", "https://example.com/@max-cpu")
	form.Set("metarr_max_cpu_usage", "100")
	id := addWebChannel(t, form)

	for _, save := range []string{"100", "80.5"} {
		form.Set("metarr_max_cpu_usage", save)
		if w := sendForm(t, http.MethodPut, fmt.Sprintf("/api/v1/channels/%d/update", id), form); w.Code != http.StatusOK {
			t.Fatalf("saving %s = %d %s", save, w.Code, w.Body.String())
		}
		var got channelDetails
		fetchJSON(t, fmt.Sprintf("/api/v1/channels/%d", id), &got)
		if v := formValue(got.Metarr["metarr_max_cpu_usage"]); v != save {
			t.Errorf("saved %s, the form gets back %q", save, v)
		}
	}
}

// TestMaxCPUFieldsTakeDecimals tests the forms' Max CPU fields allow decimals, or the browser won't submit the form.
func TestMaxCPUFieldsTakeDecimals(t *testing.T) {
	field := regexp.MustCompile(`(?s)<input[^>]*metarr_max_cpu_usage[^>]*>`)
	for _, partial := range []string{"settings-fields.html", "url-settings-fields.html"} {
		page, err := os.ReadFile(filepath.Join(pkgDir, "..", "..", "web", "partials", partial))
		if err != nil {
			t.Fatal(err)
		}
		found := field.Find(page)
		if found == nil {
			t.Errorf("%s has no Max CPU field", partial)
			continue
		}
		if !regexp.MustCompile(`step="any"`).Match(found) {
			t.Errorf("%s: Max CPU field has no step=\"any\": %s", partial, found)
		}
	}
}
