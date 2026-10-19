package validation

import (
	"os"
	"path/filepath"
	"testing"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/sharedtemplates"
)

// TestCheckDirectory tests that directory paths are validated without anything being created.
func TestCheckDirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"existing", dir, false},
		{"missing (created when saved)", filepath.Join(dir, "new", "videos"), false},
		{"templated", filepath.Join(dir, "{{"+sharedtemplates.ChannelName+"}}", "videos"), false},
		{"unsupported template tag", filepath.Join(dir, "{{not_a_tag}}"), true},
		{"file, not a directory", file, true},
	}
	for _, tt := range tests {
		if err := CheckDirectory(tt.path, sharedtemplates.TubarrTemplateTags); (err != nil) != tt.wantErr {
			t.Errorf("%s: CheckDirectory(%q) error = %v, want error: %v", tt.name, tt.path, err, tt.wantErr)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("CheckDirectory created something: %v", entries)
	}
}

// TestCheckDestination tests that a destination is only available when it (or, if templated, the directory holding its
// fixed part) exists.
func TestCheckDestination(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path    string
		wantErr bool
	}{
		{"", false},
		{dir, false},
		{filepath.Join(dir, "missing"), true},
		{filepath.Join(dir, "{{year}}", "videos"), false},
		{filepath.Join(dir, "show-{{year}}"), false},
		{filepath.Join(dir, "missing", "{{year}}"), true},
		{"/{{year}}/videos", false},
		{"{{channel_name}}/videos", false},
		{file, true},
	}
	for _, tt := range tests {
		if err := CheckDestination(tt.path); (err != nil) != tt.wantErr {
			t.Errorf("CheckDestination(%q) error = %v, want error: %v", tt.path, err, tt.wantErr)
		}
	}
}

// TestCheckTemplatedBase tests that only a templated directory is checked, by the fixed part before its first tag.
func TestCheckTemplatedBase(t *testing.T) {
	dir := t.TempDir()
	for path, wantErr := range map[string]bool{
		filepath.Join(dir, "missing"):                     false, // Plain, so created when saved.
		filepath.Join(dir, "{{channel_name}}", "videos"):  false,
		filepath.Join(dir, "missing", "{{channel_name}}"): true,
	} {
		if err := CheckTemplatedBase(path); (err != nil) != wantErr {
			t.Errorf("CheckTemplatedBase(%q) error = %v, want error: %v", path, err, wantErr)
		}
	}
}

// TestSettingsRefuseTemplatedFolderWithoutBase tests saving settings refuses a templated folder whose fixed part is missing.
func TestSettingsRefuseTemplatedFolderWithoutBase(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing", "{{channel_name}}")
	if err := ValidateSettingsModel(&models.Settings{VideoDir: missing}, true); err == nil {
		t.Error("video folder: accepted")
	}
	if err := ValidateSettingsModel(&models.Settings{JSONDir: missing}, true); err == nil {
		t.Error("JSON folder: accepted")
	}
	moveOps := []models.MetaFilterMoveOps{{Field: "title", ContainsValue: "cats", OutputDir: missing}}
	if err := ValidateSettingsModel(&models.Settings{MetaFilterMoveOps: moveOps}, true); err == nil {
		t.Error("move op folder: accepted")
	}
	if err := ValidateSettingsModel(&models.Settings{VideoDir: missing}, false); err != nil {
		t.Errorf("loading (paths unchecked): %v", err)
	}
}
