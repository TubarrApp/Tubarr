package validation_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"tubarr/internal/models"
	"tubarr/internal/validation"
)

// TestValidateMetarrOutputDirs checks if the function correctly identifies valid directories in a map of URLs ---------------------
func TestValidateMetarrOutputDirs(t *testing.T) {
	// Test 1: Empty map → no error
	if err := validation.ValidateMetarrOutputDirs(nil); err != nil {
		t.Fatalf("expected nil error for empty input, got: %v", err)
	}

	// Base temp directory
	temp := t.TempDir()

	// Valid URL + existing directory
	goodMap := map[string]string{
		"https://example.com/video": temp,
	}

	if err := validation.ValidateMetarrOutputDirs(goodMap); err != nil {
		t.Fatalf("expected valid map to pass, got: %v", err)
	}

	// Invalid URL
	badURL := map[string]string{
		"::::::not-a-url": temp,
	}

	if err := validation.ValidateMetarrOutputDirs(badURL); err == nil {
		t.Fatalf("expected error for invalid URL, got nil")
	}

	// Non-existent directory
	missingDir := temp + "/does_not_exist"

	missingMap := map[string]string{
		"https://example.com/video": missingDir,
	}

	if err := validation.ValidateMetarrOutputDirs(missingMap); err == nil {
		t.Fatalf("expected error for non-existent path, got nil")
	}

	// Duplicate directory paths
	duplicateMap := map[string]string{
		"https://example.com/a": temp,
		"https://example.com/b": temp,
	}

	if err := validation.ValidateMetarrOutputDirs(duplicateMap); err != nil {
		t.Fatalf("expected duplicates to pass (single stat), got: %v", err)
	}

	// Duplicate directory paths
	missingMultiMap := map[string]string{
		"https://example.com/c": temp,
		"https://example.com/d": missingDir,
	}

	if err := validation.ValidateMetarrOutputDirs(missingMultiMap); err == nil {
		t.Fatalf("expected second iteration to fail, got: %v", err)
	}
}

// TestValidateDirectory runs checks for directory validation --------------------------------------------------------------------
func TestValidateDirectory_ExistingDirectory(t *testing.T) {
	tmp := t.TempDir()

	info, err := validation.ValidateDirectory(tmp, false)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if info == nil {
		t.Fatalf("expected file info, got nil")
	}
}

func TestValidateDirectory_CreateIfMissing(t *testing.T) {
	tmp := t.TempDir()
	missing := tmp + "/new"
	invalidName := tmp + "/bad\x00name"
	t.Cleanup(func() {
		os.Remove(missing)
		os.Remove(invalidName)
	})

	// Missing, create it
	info, err := validation.ValidateDirectory(missing, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(missing); statErr != nil {
		t.Fatalf("directory was not created")
	}
	if info == nil {
		t.Fatalf("expected file info, got nil")
	}

	// Missing, invalid directory name
	info, err = validation.ValidateDirectory(invalidName, true)
	if err == nil {
		t.Fatalf("expected error, got: %v", err)
	}
	if _, statErr := os.Stat(invalidName); statErr == nil {
		t.Fatalf("directory was created, should have failed")
	}
	if info != nil {
		t.Fatalf("expected nil file info, got info")
	}
}

func TestValidateDirectory_ErrorIfMissing(t *testing.T) {
	tmp := t.TempDir()
	missing := tmp + "/missing"

	info, err := validation.ValidateDirectory(missing, false)
	if err == nil {
		t.Fatalf("expected error for missing directory, got nil")
	}
	if info != nil {
		t.Fatalf("expected nil os.FileInfo for missing, uncreated directory")
	}

}

func TestValidateDirectory_ValidTemplate(t *testing.T) {
	goodTag := "/path/{{video_title}}/file"

	info, err := validation.ValidateDirectory(goodTag, false)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if info != nil {
		t.Fatalf("expected os.FileInfo for good template directories")
	}
}

func TestValidateDirectory_InvalidTemplate(t *testing.T) {
	// Unsupported tag: {{badtag}}
	dir := "/root/{{badtag}}/video"

	info, err := validation.ValidateDirectory(dir, false)
	if err == nil {
		t.Fatalf("expected error for invalid template tag")
	}
	if info != nil {
		t.Fatalf("expected nil os.Info, got info")
	}
}

// TestValidateFile runs checks for file validation -----------------------------------------------------------------------------
func TestValidateFile_ExistingFile(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "x.txt")
	t.Cleanup(func() {
		os.Remove(f)
	})

	if err := os.WriteFile(f, []byte("hello"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	info, err := validation.ValidateFile(f, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatalf("expected file info, got nil")
	}
}

func TestValidateFile_CreateIfMissing(t *testing.T) {
	tmp := t.TempDir()
	valid := tmp + "newfile.txt"
	invalid := tmp + "\x00"
	t.Cleanup(func() {
		os.Remove(valid)
		os.Remove(invalid)
	})

	// Valid filename
	info, err := validation.ValidateFile(valid, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(valid); statErr != nil {
		t.Fatalf("file was not created")
	}
	if info == nil {
		t.Fatalf("expected os.FileInfo, got nil")
	}

	// Invalid filename
	info, err = validation.ValidateFile(invalid, true)
	if err == nil {
		t.Fatalf("expected error, got: %v", err)
	}
	if _, statErr := os.Stat(invalid); statErr == nil {
		t.Fatalf("invalid file was created")
	}
	if info != nil {
		t.Fatalf("expected nil os.FileInfo, got info")
	}
}

func TestValidateFile_MissingNoCreate(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "does_not_exist.txt")

	if _, err := validation.ValidateFile(f, false); err == nil {
		t.Fatalf("expected error for missing file without create flag")
	}
}

func TestValidateFile_PathIsDirectory(t *testing.T) {
	tmp := t.TempDir()

	_, err := validation.ValidateFile(tmp, false)
	if err == nil {
		t.Fatalf("expected error when path is a directory")
	}
}

func TestValidateFile_ValidTemplate(t *testing.T) {
	tmp := os.TempDir()
	goodTag := tmp + "/{{video_title}}.txt"

	info, err := validation.ValidateFile(goodTag, false)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if info != nil {
		t.Fatalf("expected os.FileInfo for good template directories")
	}
}

func TestValidateFile_InvalidTemplate(t *testing.T) {
	// Unsupported tag: {{badtag}}
	tmp := os.TempDir()
	badTag := tmp + "/{{badtag}}.txt"

	info, err := validation.ValidateFile(badTag, false)
	if err == nil {
		t.Fatalf("expected error for invalid template tag")
	}
	if info != nil {
		t.Fatalf("expected nil os.Info, got info")
	}
}

// TestValidateMaxFilesize checks various values for validation -----------------------------------------------------------------
func TestValidateMaxFilesize(t *testing.T) {
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		// GB
		{"1g", "1g", true},
		{"60gb", "60g", true},
		// MB
		{"10m", "10m", true},
		{"60MB", "60m", true},
		// KB
		{"500k", "500k", true},
		{"1000KB", "1000k", true},
		// Bytes
		{"1B", "1", true},
		{"b", "", false},
		{"5BB", "", false},
		// Fail
		{"", "", true},
		{"1xb", "", false},
		{"abc", "", false},
	}

	for _, tc := range tests {
		got, err := validation.ValidateMaxFilesize(tc.input)
		if tc.ok && err != nil {
			t.Fatalf("input %q: unexpected error: %v", tc.input, err)
		}
		if !tc.ok && err == nil {
			t.Fatalf("input %q: expected error, got none", tc.input)
		}
		if got != tc.want {
			t.Fatalf("input %q: expected %q, got %q", tc.input, tc.want, got)
		}
	}
}

// Notifications -----------------------------------------------------------------------------------------------
func TestValidateNotifications(t *testing.T) {
	// ---- SHOULD PASS ----
	var valid = []*models.Notification{
		{
			ChannelID:  1,
			NotifyURL:  "http://www.google.com",
			ChannelURL: "http://www.example.com",
			Name:       "Google",
		},
	}
	if err := validation.ValidateNotifications(valid); err != nil {
		t.Fatalf("Failed for valid notification")
	}

	var validNoURL = []*models.Notification{
		{
			ChannelID: 1,
			NotifyURL: "http://www.google.com",
			Name:      "Google",
		},
	}
	if err := validation.ValidateNotifications(validNoURL); err != nil {
		t.Fatalf("Failed test")
	}

	var validNoURLNoName = []*models.Notification{
		{
			ChannelID: 1,
			NotifyURL: "http://www.google.com",
		},
	}
	if err := validation.ValidateNotifications(validNoURLNoName); err != nil {
		t.Fatalf("Failed test")
	}

	// ---- SHOULD FAIL ----
	var invalidNotifyURL = []*models.Notification{
		{
			ChannelID:  1,
			NotifyURL:  "http://example.com/%😃",
			ChannelURL: "http://www.example.com",
			Name:       "Google",
		},
	}
	if err := validation.ValidateNotifications(invalidNotifyURL); err == nil {
		t.Fatalf("Passed for invalid notification")
	}

	var noNotifyURL = []*models.Notification{
		{
			ChannelID: 1,
			NotifyURL: "",
		},
	}
	if err := validation.ValidateNotifications(noNotifyURL); err == nil {
		t.Fatalf("Passed for invalid notification")
	}
}

// YTDLP Output Extension ------------------------------------------------------------------------------------
func TestValidateYtdlpOutputExtension(t *testing.T) {
	var validExts = []string{
		"mp4",
		"mov",
		"mkv",
		".mp4",
		".webm",
	}

	for _, v := range validExts {
		if err := validation.ValidateYtdlpOutputExtension(v); err != nil {
			t.Fatalf("valid YTDLP extension failed")
		}
	}

	var invalidExts = []string{
		"mk4",
		".nope",
	}

	for _, v := range invalidExts {
		if err := validation.ValidateYtdlpOutputExtension(v); err == nil {
			t.Fatalf("invalid YTDLP extension passed")
		}
	}
}

// TestValidateFilterOps --------------------------------------------------------------------------------------
func TestValidateFilterOps_EmptySliceOK(t *testing.T) {
	if err := validation.ValidateFilterOps(nil); err != nil {
		t.Fatalf("expected nil error for empty slice, got %v", err)
	}
}

func TestValidateFilterOps_Valid(t *testing.T) {
	filters := []models.Filters{
		{
			ChannelURL:    "",
			Field:         "title",
			ContainsOmits: "contains",
			MustAny:       "must",
			Value:         "foo",
		},
		{
			Field:         "channel",
			ContainsOmits: "omits",
			MustAny:       "any",
			Value:         "bar",
		},
	}

	if err := validation.ValidateFilterOps(filters); err != nil {
		t.Fatalf("expected filters to be valid, got %v", err)
	}
}

func TestValidateFilterOps_InvalidContainsOmits(t *testing.T) {
	filters := []models.Filters{
		{
			Field:         "title",
			ContainsOmits: "bad-type",
			MustAny:       "must",
			Value:         "foo",
		},
	}

	err := validation.ValidateFilterOps(filters)
	if err == nil {
		t.Fatalf("expected error for invalid ContainsOmits")
	}
	if !strings.Contains(err.Error(), "invalid type") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidateFilterOps_InvalidMustAny(t *testing.T) {
	filters := []models.Filters{
		{
			Field:         "title",
			ContainsOmits: "contains",
			MustAny:       "wrong",
			Value:         "foo",
		},
	}

	err := validation.ValidateFilterOps(filters)
	if err == nil {
		t.Fatalf("expected error for invalid MustAny")
	}
	if !strings.Contains(err.Error(), "invalid condition") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidateFilterOps_EmptyField(t *testing.T) {
	filters := []models.Filters{
		{
			Field:         "",
			ContainsOmits: "contains",
			MustAny:       "must",
			Value:         "foo",
		},
	}

	err := validation.ValidateFilterOps(filters)
	if err == nil {
		t.Fatalf("expected error for empty field")
	}
	if !strings.Contains(err.Error(), "empty field") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestValidateMetaFilterMoveOps ----------------------------------------------------------------------------------------
func TestValidateMetaFilterMoveOps_EmptySliceOK(t *testing.T) {
	if err := validation.ValidateMetaFilterMoveOps(nil); err != nil {
		t.Fatalf("expected nil error for empty slice, got %v", err)
	}
}

func TestValidateMetaFilterMoveOps_Valid(t *testing.T) {
	tmp := t.TempDir()

	moveOps := []models.MetaFilterMoveOps{
		{
			Field:     "channel",
			OutputDir: tmp,
		},
	}

	if err := validation.ValidateMetaFilterMoveOps(moveOps); err != nil {
		t.Fatalf("expected moveOps to be valid, got %v", err)
	}
}

func TestValidateMetaFilterMoveOps_EmptyField(t *testing.T) {
	tmp := t.TempDir()

	moveOps := []models.MetaFilterMoveOps{
		{
			Field:     "",
			OutputDir: tmp,
		},
	}

	err := validation.ValidateMetaFilterMoveOps(moveOps)
	if err == nil {
		t.Fatalf("expected error for empty field")
	}
	if !strings.Contains(err.Error(), "empty field") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidateMetaFilterMoveOps_InvalidDirectory(t *testing.T) {
	// Non-existent directory to trigger ValidateDirectory error
	tmp := t.TempDir()
	missingDir := filepath.Join(tmp, "fake_dir")
	t.Cleanup(func() {
		os.RemoveAll(missingDir)
	})

	moveOps := []models.MetaFilterMoveOps{
		{
			Field:     "channel",
			OutputDir: missingDir,
		},
	}

	err := validation.ValidateMetaFilterMoveOps(moveOps)
	if err == nil {
		t.Fatalf("expected error for invalid directory")
	}
	// Don't assert too tightly on the message; just ensure it's about directory
	if !strings.Contains(err.Error(), "invalid directory") && !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
