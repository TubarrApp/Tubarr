package server

import (
	"fmt"
	"net/http"
	"testing"
)

// TestMoveOpDirectoryChecked tests saving a channel refuses a new move op directory with bad template tags, as it does a
// video directory, since one with reversed braces would otherwise reach the directory parser when a video matches.
func TestMoveOpDirectoryChecked(t *testing.T) {
	setupFuzzServer(t)
	const u = "https://example.com/@move-op-dir"
	form := webChannelForm(t, "Move Op Dir", u)
	id := addWebChannel(t, form)
	dir := testDir

	// Channel level.
	for _, tc := range []struct {
		name, moveDir string
		want          int
	}{
		{"plain directory", dir, http.StatusOK},
		{"known tag", dir + "/{{channel_name}}", http.StatusOK},
		{"reversed braces", dir + "/}}x{{", http.StatusBadRequest},
		{"unknown tag", dir + "/{{nope}}", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form.Set("move_ops", "title:contains:x||"+tc.moveDir)
			if w := sendForm(t, http.MethodPut, fmt.Sprintf("/api/v1/channels/%d/update", id), form); w.Code != tc.want {
				t.Errorf("saving = %d %s, want %d", w.Code, w.Body.String(), tc.want)
			}
		})
	}

	// Channel URL level.
	form.Del("move_ops")
	setURLSettings(t, form, u, map[string]any{"move_ops": "title:contains:x||" + dir + "/}}x{{"})
	if w := sendForm(t, http.MethodPut, fmt.Sprintf("/api/v1/channels/%d/update", id), form); w.Code != http.StatusBadRequest {
		t.Errorf("saving it for the URL = %d %s, want %d", w.Code, w.Body.String(), http.StatusBadRequest)
	}
}
