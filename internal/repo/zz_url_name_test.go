package repo

import (
	"maps"
	"testing"
	"tubarr/internal/models"
)

// TestChannelURLNameSaved tests a channel URL's name is saved with its channel, changed, and saved with a URL added later.
func TestChannelURLNameSaved(t *testing.T) {
	cs := newTestChannelStore(t)
	c := testChannel(t, "chan", "https://example.com/@a")
	c.URLModels[0].Name = "Main"
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}
	urls, err := cs.GetChannelURLModels(c, false)
	if err != nil || len(urls) != 1 || urls[0].Name != "Main" {
		t.Fatalf("after adding the channel: got %v, %v; want one URL named %q", urls, err, "Main")
	}

	// Renamed.
	urls[0].Name = "Shorts"
	if err := cs.UpdateChannelURLSettings(urls[0]); err != nil {
		t.Fatal(err)
	}

	// Added later.
	if _, err := cs.AddChannelURL(c.ID, &models.ChannelURL{URL: "https://example.com/@b", Name: "Second"}, false); err != nil {
		t.Fatal(err)
	}

	urls, err = cs.GetChannelURLModels(c, false)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, cu := range urls {
		got[cu.URL] = cu.Name
	}
	if want := map[string]string{"https://example.com/@a": "Shorts", "https://example.com/@b": "Second"}; !maps.Equal(got, want) {
		t.Errorf("got names %v, want %v", got, want)
	}
}
