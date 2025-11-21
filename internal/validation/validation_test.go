package validation_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/sharedtemplates"
	"github.com/TubarrApp/gocommon/sharedvalidation"
)

// TestValidateRenameFlags ------------------------------------------------------------------------------------------------
func TestValidateRenameFlag(t *testing.T) {
	tests := []struct {
		input string
		ok    bool
	}{
		// Valid.
		{"skips", true},
		{"", true},

		// Invalid.
		{"blah", false},
	}

	for _, tt := range tests {
		err := validation.ValidateRenameFlag(tt.input)
		if tt.ok && err != nil {
			t.Fatalf("input %q expected to pass, got err: %v", tt.input, err)
		}
		if !tt.ok && err == nil {
			t.Fatalf("input %q expected to fail, got nil err", tt.input)
		}
	}
}

func TestMaxCPU(t *testing.T) {
	tests := []struct {
		input     float64
		allowZero bool
		expect    float64
	}{
		// Zero values.
		{0, true, 0.0},
		{0.0, true, 0.0},
		{0, false, 101.0},

		// Non-zero values.
		{100, false, 101.0},
		{1, false, 5.0},
		{105, false, 101.0},
		{100, true, 101.0},
		{1, true, 5.0},
		{105, true, 101.0},
	}

	for _, tt := range tests {
		got := sharedvalidation.ValidateMaxCPU(tt.input, tt.allowZero)
		if tt.expect != got {
			t.Fatalf("input %f allowZero %v: expected %f, got %f", tt.input, tt.allowZero, tt.expect, got)
		}
	}
}

// TestValidateMetarrOutputDirs -------------------------------------------------------------------------------------------
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

	_, info, err := sharedvalidation.ValidateDirectory(tmp, false, sharedtemplates.AllTemplatesMap)
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
		if err := os.Remove(missing); err != nil {
			fmt.Printf("Could not remove %q: %v", missing, err)
		}
		if err := os.Remove(invalidName); err != nil {
			fmt.Printf("Could not remove %q: %v", invalidName, err)
		}
	})

	// Missing, create it
	_, info, err := sharedvalidation.ValidateDirectory(missing, true, sharedtemplates.AllTemplatesMap)
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
	_, info, err = sharedvalidation.ValidateDirectory(invalidName, true, sharedtemplates.AllTemplatesMap)
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

	_, info, err := sharedvalidation.ValidateDirectory(missing, false, sharedtemplates.AllTemplatesMap)
	if err == nil {
		t.Fatalf("expected error for missing directory, got nil")
	}
	if info != nil {
		t.Fatalf("expected nil os.FileInfo for missing, uncreated directory")
	}

}

func TestValidateDirectory_ValidTemplate(t *testing.T) {
	goodTag := "/path/{{video_title}}/file"

	_, info, err := sharedvalidation.ValidateDirectory(goodTag, false, sharedtemplates.AllTemplatesMap)
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

	_, info, err := sharedvalidation.ValidateDirectory(dir, false, sharedtemplates.AllTemplatesMap)
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
		if err := os.Remove(f); err != nil {
			fmt.Printf("Could not remove %q: %v", f, err)
		}
	})

	if err := os.WriteFile(f, []byte("hello"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, info, err := sharedvalidation.ValidateFile(f, false, sharedtemplates.AllTemplatesMap)
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
		if err := os.Remove(valid); err != nil {
			fmt.Printf("Could not remove file %q: %v", valid, err)
		}
		if err := os.Remove(invalid); err != nil {
			fmt.Printf("Could not remove file %q: %v", invalid, err)
		}
	})

	// Valid filename
	_, info, err := sharedvalidation.ValidateFile(valid, true, sharedtemplates.AllTemplatesMap)
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
	_, info, err = sharedvalidation.ValidateFile(invalid, true, sharedtemplates.AllTemplatesMap)
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

	if _, _, err := sharedvalidation.ValidateFile(f, false, sharedtemplates.AllTemplatesMap); err == nil {
		t.Fatalf("expected error for missing file without create flag")
	}
}

func TestValidateFile_PathIsDirectory(t *testing.T) {
	tmp := t.TempDir()

	_, _, err := sharedvalidation.ValidateFile(tmp, false, sharedtemplates.AllTemplatesMap)
	if err == nil {
		t.Fatalf("expected error when path is a directory")
	}
}

func TestValidateFile_ValidTemplate(t *testing.T) {
	tmp := os.TempDir()
	goodTag := tmp + "/{{video_title}}.txt"

	_, info, err := sharedvalidation.ValidateFile(goodTag, false, sharedtemplates.AllTemplatesMap)
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

	_, info, err := sharedvalidation.ValidateFile(badTag, false, sharedtemplates.AllTemplatesMap)
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

// TestValidateNotifications -----------------------------------------------------------------------------------------------
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

// TestValidateYtdlpOutputExtension ------------------------------------------------------------------------------------
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
		if err := os.RemoveAll(missingDir); err != nil {
			fmt.Printf("Could not remove %q: %v", missingDir, err)
		}
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

// TestValidateFilteredMetaOps ----------------------------------------------------------------------------------------
func TestValidateFilteredMetaOps_EmptySliceOK(t *testing.T) {
	if err := validation.ValidateFilteredMetaOps(nil); err != nil {
		t.Fatalf("expected nil error for empty slice, got %v", err)
	}
}

func TestValidateFilteredMetaOps_Valid(t *testing.T) {
	filters := []models.Filters{
		{
			Field:         "title",
			ContainsOmits: "contains",
			MustAny:       "must",
			Value:         "foo",
		},
	}
	metaOps := []models.MetaOps{
		{
			Field:  "title",
			OpType: consts.OpReplace,
		},
	}

	input := []models.FilteredMetaOps{
		{
			Filters: filters,
			MetaOps: metaOps,
		},
	}

	if err := validation.ValidateFilteredMetaOps(input); err != nil {
		t.Fatalf("expected valid filtered meta ops, got %v", err)
	}
}

func TestValidateFilteredMetaOps_InvalidFilters(t *testing.T) {
	// invalid filter: bad ContainsOmits
	filters := []models.Filters{
		{
			Field:         "title",
			ContainsOmits: "bad",
			MustAny:       "must",
		},
	}
	metaOps := []models.MetaOps{
		{
			Field:  "title",
			OpType: consts.OpReplace,
		},
	}

	input := []models.FilteredMetaOps{
		{
			Filters: filters,
			MetaOps: metaOps,
		},
	}

	err := validation.ValidateFilteredMetaOps(input)
	if err == nil {
		t.Fatalf("expected error for invalid filters")
	}
	if !strings.Contains(err.Error(), "invalid filters") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidateFilteredMetaOps_NoFilters(t *testing.T) {
	metaOps := []models.MetaOps{
		{
			Field:  "title",
			OpType: consts.OpReplace,
		},
	}

	input := []models.FilteredMetaOps{
		{
			Filters: nil,
			MetaOps: metaOps,
		},
	}

	err := validation.ValidateFilteredMetaOps(input)
	if err == nil {
		t.Fatalf("expected error for no filters")
	}
	if !strings.Contains(err.Error(), "has no filters") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidateFilteredMetaOps_NoMetaOps(t *testing.T) {
	filters := []models.Filters{
		{
			Field:         "title",
			ContainsOmits: "contains",
			MustAny:       "must",
		},
	}

	input := []models.FilteredMetaOps{
		{
			Filters: filters,
			MetaOps: nil,
		},
	}

	err := validation.ValidateFilteredMetaOps(input)
	if err == nil {
		t.Fatalf("expected error for no meta operations")
	}
	if !strings.Contains(err.Error(), "has no meta operations") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestValidateFilteredFilenameOps -----------------------------------------------------------------------------------------------
func TestValidateFilteredFilenameOps_EmptySliceOK(t *testing.T) {
	if err := validation.ValidateFilteredFilenameOps(nil); err != nil {
		t.Fatalf("expected nil error for empty slice, got %v", err)
	}
}

func TestValidateFilteredFilenameOps_Valid(t *testing.T) {
	filters := []models.Filters{
		{
			Field:         "title",
			ContainsOmits: "contains",
			MustAny:       "must",
			Value:         "foo",
		},
	}
	filenameOps := []models.FilenameOps{
		{
			OpType: consts.OpReplace,
		},
	}

	input := []models.FilteredFilenameOps{
		{
			Filters:     filters,
			FilenameOps: filenameOps,
		},
	}

	if err := validation.ValidateFilteredFilenameOps(input); err != nil {
		t.Fatalf("expected valid filtered filename ops, got %v", err)
	}
}

func TestValidateFilteredFilenameOps_InvalidFilters(t *testing.T) {
	filters := []models.Filters{
		{
			Field:         "title",
			ContainsOmits: "bad",
			MustAny:       "must",
		},
	}
	filenameOps := []models.FilenameOps{
		{
			OpType: consts.OpReplace,
		},
	}

	input := []models.FilteredFilenameOps{
		{
			Filters:     filters,
			FilenameOps: filenameOps,
		},
	}

	err := validation.ValidateFilteredFilenameOps(input)
	if err == nil {
		t.Fatalf("expected error for invalid filters")
	}
	if !strings.Contains(err.Error(), "invalid filters") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidateFilteredFilenameOps_NoFilters(t *testing.T) {
	filenameOps := []models.FilenameOps{
		{
			OpType: consts.OpReplace,
		},
	}

	input := []models.FilteredFilenameOps{
		{
			Filters:     nil,
			FilenameOps: filenameOps,
		},
	}

	err := validation.ValidateFilteredFilenameOps(input)
	if err == nil {
		t.Fatalf("expected error for no filters")
	}
	if !strings.Contains(err.Error(), "has no filters") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidateFilteredFilenameOps_NoFilenameOps(t *testing.T) {
	filters := []models.Filters{
		{
			Field:         "title",
			ContainsOmits: "contains",
			MustAny:       "must",
		},
	}

	input := []models.FilteredFilenameOps{
		{
			Filters:     filters,
			FilenameOps: nil,
		},
	}

	err := validation.ValidateFilteredFilenameOps(input)
	if err == nil {
		t.Fatalf("expected error for no filename operations")
	}
	if !strings.Contains(err.Error(), "has no filename operations") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestValidateToFromDate -----------------------------------------------------------------------------------------------------
func TestValidateToFromDate_EmptyOK(t *testing.T) {
	got, err := validation.ValidateToFromDate("")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestValidateToFromDate_Today(t *testing.T) {
	got, err := validation.ValidateToFromDate("today")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := time.Now().Format("20060102")
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestValidateToFromDate_TodayCaseInsensitive(t *testing.T) {
	got, err := validation.ValidateToFromDate("ToDaY")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 8 {
		t.Fatalf("expected 8-char date, got %q", got)
	}
}

func TestValidateToFromDate_RawYYYYMMDD(t *testing.T) {
	got, err := validation.ValidateToFromDate("20241231")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "20241231" {
		t.Fatalf("expected 20241231, got %q", got)
	}
}

func TestValidateToFromDate_WithMarkers(t *testing.T) {
	got, err := validation.ValidateToFromDate("2025y12m31d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "20251231" {
		t.Fatalf("expected 20251231, got %q", got)
	}
}

func TestValidateToFromDate_NoYearDefaultsToCurrent(t *testing.T) {
	year := time.Now().Year()
	got, err := validation.ValidateToFromDate("12m31d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedPrefix := strconv.Itoa(year)
	if !strings.HasPrefix(got, expectedPrefix) {
		t.Fatalf("expected year prefix %q, got %q", expectedPrefix, got)
	}
	if !strings.HasSuffix(got, "1231") {
		t.Fatalf("expected month/day 1231, got %q", got)
	}
}

func TestValidateToFromDate_SingleDigitMonthDayPadded(t *testing.T) {
	got, err := validation.ValidateToFromDate("2025y6m3d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "20250603" {
		t.Fatalf("expected 20250603, got %q", got)
	}
}

func TestValidateToFromDate_InvalidFormat(t *testing.T) {
	in := "nonsense"
	d, err := validation.ValidateToFromDate(in)
	if err == nil {
		t.Fatalf("expected error for invalid format")
	}
	if !strings.Contains(err.Error(), "invalid date format") {
		t.Fatalf("unexpected error message: %v", err)
	}

	t.Logf("Got date %q back from input %q", d, in)
}

func TestValidateToFromDate_InvalidMonth(t *testing.T) {
	// becomes "20251301" after cleaning; regex should parse and range check
	in := "2025-13-01"
	d, err := validation.ValidateToFromDate(in)
	if err == nil {
		t.Fatalf("expected error for invalid month")
	}
	if !strings.Contains(err.Error(), "invalid month") {
		t.Fatalf("unexpected error message: %v", err)
	}

	t.Logf("Got date %q back from input %q", d, in)
}

func TestValidateToFromDate_InvalidDay(t *testing.T) {
	// 32 > 31
	in := "2025y12m32d"
	d, err := validation.ValidateToFromDate(in)
	if err == nil {
		t.Fatalf("expected error for invalid day")
	}
	if !strings.Contains(err.Error(), "invalid day") {
		t.Fatalf("unexpected error message: %v", err)
	}

	t.Logf("Got date %q back from input %q", d, in)
}

func TestValidateToFromDate_InvalidYear(t *testing.T) {
	in := "0999y12m31d"
	d, err := validation.ValidateToFromDate(in)
	if err == nil {
		t.Fatalf("expected error for invalid year")
	}
	if !strings.Contains(err.Error(), "invalid year") {
		t.Fatalf("unexpected error message: %v", err)
	}

	t.Logf("Got date %q back from input %q", d, in)
}

func TestValidateGPU(t *testing.T) {
	tests := []struct {
		gpu         string
		directory   string
		expectGPU   string
		expectDir   string
		gpuMatch    bool
		gpuDirMatch bool
		ok          bool
	}{
		// Pass.
		{"auto", "", "auto", "", true, true, true},
		{"", "", "", "", true, true, true},
		{"nvidia", "/dev/dri/renderD128", "cuda", "/dev/dri/renderD128", true, true, true},

		// Fail.
		{"auto", "", "", "", false, true, false},
		{"cuda", "", "cuda", "", true, true, false},
		{"fake", "", "", "", false, true, false},
		{"fake", "/dev/dri/FAKE", "", "", false, false, false},
	}

	for _, tt := range tests {
		gpu, gpuDir, err := validation.ValidateGPU(tt.gpu, tt.directory)
		if err != nil && tt.ok {
			t.Fatalf("Expected pass, failed with error: %q", err)
		}
		if gpu != tt.expectGPU && tt.gpuMatch {
			t.Fatalf("Expected %q, got %q", tt.expectGPU, gpu)
		}
		if gpuDir != tt.expectDir && tt.gpuDirMatch {
			t.Fatalf("Expected %q, got %q", tt.expectDir, gpuDir)
		}
	}
}
