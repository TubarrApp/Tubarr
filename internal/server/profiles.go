package server

import (
	"fmt"
	"net/http"
	"strconv"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"

	"github.com/go-chi/chi/v5"
)

// profileResponse is a profile as listed in the web UI, with the channels and channel URLs using it.
type profileResponse struct {
	ID               int64                 `json:"id"`
	Name             string                `json:"name"`
	LoadError        string                `json:"load_error,omitempty"`
	Settings         *settingsResponse     `json:"settings,omitempty"`
	Metarr           *metarrArgsResponse   `json:"metarr,omitempty"`
	UsedBy           []profileUserResponse `json:"used_by"`
	VideoDirNeededBy []string              `json:"video_dir_needed_by"`
}

// profileUserResponse is a channel using a profile, or one of its URLs if URL isn't blank.
type profileUserResponse struct {
	ChannelID int64  `json:"channel_id"`
	Channel   string `json:"channel"`
	URL       string `json:"url,omitempty"`
}

// handleListProfiles lists every profile, with the channels and channel URLs using each.
func (ss *serverStore) handleListProfiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := ss.cs.GetAllProfiles()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	channels, _, err := ss.cs.GetAllChannels(false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Each profile's users, and the channels relying on it for a video directory.
	users := map[int64][]profileUserResponse{}
	videoDirNeededBy := map[int64][]string{}
	for _, c := range channels {
		if c.ProfileID != 0 {
			users[c.ProfileID] = append(users[c.ProfileID], profileUserResponse{ChannelID: c.ID, Channel: c.Name})
			if c.ChanSettings.VideoDir == "" {
				videoDirNeededBy[c.ProfileID] = append(videoDirNeededBy[c.ProfileID], c.Name)
			}
		}
		for _, cu := range c.URLModels {
			if cu.ProfileID != 0 {
				users[cu.ProfileID] = append(users[cu.ProfileID], profileUserResponse{ChannelID: c.ID, Channel: c.Name, URL: cu.URL})
			}
		}
	}

	response := make([]profileResponse, 0, len(profiles))
	for _, p := range profiles {
		pr := profileResponse{
			ID:               p.ID,
			Name:             p.Name,
			Settings:         newSettingsResponse(p.ProfileSettings),
			Metarr:           newMetarrArgsResponse(p.ProfileMetarrArgs),
			UsedBy:           append([]profileUserResponse{}, users[p.ID]...),
			VideoDirNeededBy: append([]string{}, videoDirNeededBy[p.ID]...),
		}
		if p.LoadErr != nil {
			pr.LoadError = p.LoadErr.Error()
		}
		response = append(response, pr)
	}
	writeJSON(w, r, http.StatusOK, response)
}

// handleAddProfile adds a profile from the profile form.
func (ss *serverStore) handleAddProfile(w http.ResponseWriter, r *http.Request) {
	p, err := parseProfileForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := ss.cs.AddProfile(p); err != nil {
		http.Error(w, err.Error(), channelErrStatus(err))
		return
	}
	writeText(w, http.StatusCreated, fmt.Sprintf("Profile %q added successfully", p.Name))
}

// handleUpdateProfile saves the profile form over a profile.
func (ss *serverStore) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, consts.WebParamProfileID), 10, 64)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid profile ID: %v", err), http.StatusBadRequest)
		return
	}
	p, err := parseProfileForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p.ID = id
	if err := ss.cs.UpdateProfile(p); err != nil {
		http.Error(w, err.Error(), channelErrStatus(err))
		return
	}
	writeText(w, http.StatusOK, fmt.Sprintf("Profile %q updated successfully", p.Name))
}

// handleDeleteProfile deletes a profile nothing uses.
func (ss *serverStore) handleDeleteProfile(w http.ResponseWriter, r *http.Request) {
	if err := ss.cs.DeleteProfile(consts.QProfileID, chi.URLParam(r, consts.WebParamProfileID)); err != nil {
		http.Error(w, err.Error(), channelErrStatus(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// parseProfileForm parses and validates the profile form, which sends the channel form's settings fields.
func parseProfileForm(r *http.Request) (*models.Profile, error) {
	if err := r.ParseForm(); err != nil {
		return nil, fmt.Errorf("failed to parse form data: %w", err)
	}
	blankWhitespaceValues(r)

	p := &models.Profile{Name: r.FormValue("name")}
	var err error
	if p.ProfileSettings, err = settingsFromForm(r); err != nil {
		return nil, err
	}
	if p.ProfileMetarrArgs, err = metarrArgsFromForm(r); err != nil {
		return nil, err
	}
	return p, nil
}
