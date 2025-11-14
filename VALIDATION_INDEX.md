# Validation Analysis - Complete Documentation Index

## Overview
This folder contains comprehensive analysis of all validation functions in the Tubarr project, their usage, and validation gaps in `buildSettingsFromInput()` and `buildMetarrArgsFromInput()` functions.

## Documents

### 1. VALIDATION_SUMMARY.txt
**Start here for a quick overview.**
- Key findings at a glance
- 30+ validation functions available
- Current validation coverage (59% overall)
- Critical gaps identified
- Quick reference to where validation happens

### 2. VALIDATION_QUICK_REFERENCE.md
**Use this for quick lookups while coding.**
- Comprehensive validator table (30+ functions)
- Fields that NEED validation (with exact validator to use)
- Fields that ARE being validated
- Copy-paste code blocks for missing validators
- File locations and statistics

### 3. VALIDATION_ANALYSIS.md
**Read this for detailed information.**
- All 30+ validation functions with descriptions
- Complete Settings struct field analysis (23 fields)
- Complete MetarrArgs struct field analysis (23 fields)
- Validation status for every single field
- Validation gaps and recommendations by priority
- Where validation currently happens
- Common validation patterns used

### 4. VALIDATION_CODE_SNIPPETS.md
**Reference this when implementing fixes.**
- Quick reference for all validators (with signatures)
- Real code usage examples from the codebase
- Current vs Missing validation code blocks
- Supported formats and valid values
- Error handling patterns
- Copy-paste ready solutions

## Quick Facts

| Metric | Count |
|--------|-------|
| Total Validators Available | 30+ |
| Settings Fields | 23 (11 validated = 48%) |
| MetarrArgs Fields | 23 (16 validated = 70%) |
| Overall Coverage | 46 fields (27 validated = 59%) |
| Critical Gaps | 8 file path fields + 1 maxfilesize |

## Critical Validation Gaps

### Settings struct needs validation for:
1. **ConfigFile** - Use `ValidateFile(configFile, false)`
2. **FilterFile** - Use `ValidateFile(filterFile, false)`
3. **MoveOpFile** - Use `ValidateFile(moveOpFile, false)`
4. **MaxFilesize** - Use `ValidateMaxFilesize(maxFilesize)` (validator exists but not called!)

### MetarrArgs struct needs validation for:
1. **OutputExt** - Use `ValidateOutputFiletype(outputExt)`
2. **FilenameOpsFile** - Use `ValidateFile(filenameOpsFile, false)`
3. **MetaOpsFile** - Use `ValidateFile(metaOpsFile, false)`
4. **FilteredMetaOpsFile** - Use `ValidateFile(filteredMetaOpsFile, false)`
5. **FilteredFilenameOpsFile** - Use `ValidateFile(filteredFilenameOpsFile, false)`

## Recommended Implementation Order

### High Priority (Fixes Data Integrity)
1. Add `ValidateFile()` for all *File fields
2. Add `ValidateMaxFilesize()` in buildSettingsFromInput
3. Add `ValidateOutputFiletype()` for MetarrArgs.OutputExt

### Medium Priority (Improves Robustness)
1. Add `ValidateConcurrencyLimit()` in buildMetarrArgsFromInput
2. Validate integer ranges (CrawlFreq, Retries)
3. Add hostname validation for BotBlockedHostnames

### Low Priority (Nice to Have)
1. Add external downloader existence check
2. Add cookie browser type validation

## File Locations

**Validators (internal/validation/)**
- `common.go` - 15 core validators (files, directories, dates, operations)
- `metarr.go` - 14 metarr-specific validators (transcoding, codecs, GPU)
- `db.go` - 1 database validator
- `helpers.go` - 3 utility helpers

**Functions to Update (internal/server/helpers.go)**
- `buildSettingsFromInput()` - lines 429-538
- `buildMetarrArgsFromInput()` - lines 540-687
- `fillChannelFromConfigFile()` - lines 14-292 (reference implementation)

**Data Structures (internal/models/)**
- `settings.go` - Settings, MetarrArgs, and related structs
- `vars.go` - ChannelInputPtrs struct definition

## Key Insights

### What IS Currently Validated

In `buildSettingsFromInput()`:
- FromDate, ToDate (date format)
- YTDLPOutputExt (extension whitelist)
- DLFilters (operation format)
- MoveOps (operation format + directory existence)

In `buildMetarrArgsFromInput()`:
- RenameStyle, MinFreeMem (format validation)
- TranscodeGPU + GPUDir (GPU type + device existence)
- Video/Audio Codecs (codec validation)
- TranscodeQuality (range clamping)
- All operation validations (MetaOps, FilenameOps, etc.)
- OutputDir (directory existence/creation)

### Error Handling Pattern

**Per-URL settings (buildSettingsFromInput/buildMetarrArgsFromInput):**
```go
if err != nil {
    logging.W("Failed to validate X for per-URL settings: %v", err)
    // Gracefully skip the field, don't fail the entire operation
}
```

**Main channel setup (fillChannelFromConfigFile):**
```go
if err != nil {
    http.Error(w, err.Error(), http.StatusBadRequest)
    return nil, ...  // Fail the entire operation
}
```

## Validation Function Categories

1. **Path Validation** - ValidateDirectory(), ValidateFile()
2. **Extension Validation** - ValidateYtdlpOutputExtension(), ValidateOutputFiletype()
3. **Size/Format** - ValidateMaxFilesize(), ValidateMinFreeMem()
4. **Date Validation** - ValidateToFromDate(), ValidateDateFormat()
5. **Operations** - ValidateFilterOps(), ValidateMoveOps(), ValidateFilenameOps(), ValidateMetaOps()
6. **Transcoding** - ValidateGPU(), ValidateVideoTranscodeCodecSlice(), ValidateAudioTranscodeCodecSlice(), ValidateTranscodeQuality()
7. **Config** - ValidateRenameFlag(), ValidateConcurrencyLimit(), ValidateNotificationStrings()
8. **Helpers** - EscapedSplit(), CheckForOpURL(), DeduplicateSliceEntries()

## Implementation Tips

1. **Always check pointer is not nil** - All input fields are pointers
2. **Check for empty strings** - Even after nil check
3. **Use logging.W() for warnings** - For per-URL settings (don't fail)
4. **Use http.Error() for critical** - For main channel setup
5. **Update the field on success** - Don't just validate, store the result
6. **Follow the existing pattern** - See lines 106-120 in helpers.go for example

## Example: Adding Missing Validation

```go
// Before (missing validation)
if input.MaxFilesize != nil {
    settings.MaxFilesize = *input.MaxFilesize
}

// After (with validation)
if input.MaxFilesize != nil && *input.MaxFilesize != "" {
    v, err := validation.ValidateMaxFilesize(*input.MaxFilesize)
    if err != nil {
        logging.W("Failed to validate max filesize for per-URL settings: %v", err)
    } else {
        settings.MaxFilesize = v
    }
}
```

## Next Steps

1. Read VALIDATION_SUMMARY.txt (2 min read)
2. Review VALIDATION_QUICK_REFERENCE.md (5 min read)
3. Reference VALIDATION_ANALYSIS.md for specific field details
4. Use VALIDATION_CODE_SNIPPETS.md when implementing fixes
5. Implement high-priority fixes first (file path validations)

---

Last updated: November 14, 2025
Total validation functions analyzed: 30+
Total struct fields analyzed: 46
Documentation pages: 4 comprehensive guides
