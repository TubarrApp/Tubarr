package queue

import (
	"testing"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
)

// TestDownloadAttempts tests that downloads run once plus their Download Retries setting, else the default.
func TestDownloadAttempts(t *testing.T) {
	tests := []struct {
		name string
		cu   *models.ChannelURL
		want int
	}{
		{"no settings", &models.ChannelURL{}, 1 + consts.DefaultDownloadRetries},
		{"unset", &models.ChannelURL{ChanURLSettings: &models.Settings{}}, 1 + consts.DefaultDownloadRetries},
		{"negative", &models.ChannelURL{ChanURLSettings: &models.Settings{Retries: -1}}, 1 + consts.DefaultDownloadRetries},
		{"set", &models.ChannelURL{ChanURLSettings: &models.Settings{Retries: 5}}, 6},
	}
	for _, tt := range tests {
		if got := downloadAttempts(tt.cu); got != tt.want {
			t.Errorf("%s: got %d attempts, want %d", tt.name, got, tt.want)
		}
	}
}
