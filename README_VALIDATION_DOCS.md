# Validation Analysis Documentation

Complete analysis of validation functions in Tubarr has been performed and documented in 5 comprehensive files.

## Documents Created

### 1. VALIDATION_SUMMARY.txt
**Best for: Quick executive overview (2 min read)**
- Key findings at a glance
- 30+ validation functions summary
- Current validation coverage percentages
- Critical gaps identified
- Where validation currently happens
- High-priority recommendations

### 2. VALIDATION_INDEX.md
**Best for: Navigation and context (5 min read)**
- Entry point guide explaining all documents
- Critical validation gaps with exact solutions
- Recommended implementation order
- File locations for all components
- Key insights and patterns
- Implementation tips and example

### 3. VALIDATION_QUICK_REFERENCE.md
**Best for: While coding (reference material)**
- Compact table of all 30+ validators
- Fields that NEED validation (with exact validator)
- Fields that ARE being validated
- Copy-paste code blocks for missing validators
- Supported formats and valid values
- File locations and statistics

### 4. VALIDATION_ANALYSIS.md
**Best for: Deep understanding (30 min read)**
- All 30+ validation functions with full descriptions
- Complete Settings struct analysis (23 fields)
- Complete MetarrArgs struct analysis (23 fields)
- Validation status for every single field
- Detailed validation gaps by priority
- Common patterns used throughout codebase
- Error handling strategies

### 5. VALIDATION_CODE_SNIPPETS.md
**Best for: Implementation reference**
- Quick function signatures for all validators
- Real code usage examples from the codebase
- Current vs missing validation code blocks
- All supported formats and values listed
- Error handling patterns
- Copy-paste ready solutions

## Quick Facts

| Metric | Count |
|--------|-------|
| **Total Validators Available** | 30+ |
| **Settings Fields** | 23 (11 validated = 48%) |
| **MetarrArgs Fields** | 23 (16 validated = 70%) |
| **Overall Coverage** | 46 fields (27 validated = 59%) |
| **Critical Gaps** | 8 file paths + 1 maxfilesize |

## Critical Validation Gaps

### In buildSettingsFromInput()
1. `ConfigFile` - needs `ValidateFile(configFile, false)`
2. `FilterFile` - needs `ValidateFile(filterFile, false)`
3. `MoveOpFile` - needs `ValidateFile(moveOpFile, false)`
4. `MaxFilesize` - needs `ValidateMaxFilesize(maxFilesize)` (validator exists but not called!)

### In buildMetarrArgsFromInput()
1. `OutputExt` - needs `ValidateOutputFiletype(outputExt)`
2. `FilenameOpsFile` - needs `ValidateFile(filenameOpsFile, false)`
3. `MetaOpsFile` - needs `ValidateFile(metaOpsFile, false)`
4. `FilteredMetaOpsFile` - needs `ValidateFile(filteredMetaOpsFile, false)`
5. `FilteredFilenameOpsFile` - needs `ValidateFile(filteredFilenameOpsFile, false)`

## Reading Guide

### For Quick Overview (5 minutes)
1. Read **VALIDATION_SUMMARY.txt** first

### For Implementation (15 minutes)
2. Check **VALIDATION_QUICK_REFERENCE.md** for field fixes
3. Use **VALIDATION_CODE_SNIPPETS.md** for code solutions

### For Comprehensive Understanding (45 minutes)
4. Deep dive into **VALIDATION_ANALYSIS.md**
5. Reference **VALIDATION_INDEX.md** for roadmap

## File Locations

**Validators (30+ functions):**
```
/home/calum/code/apps/Tubarr/internal/validation/
  ├── common.go      (15 core validators)
  ├── metarr.go      (14 metarr-specific validators)
  ├── db.go          (1 database validator)
  └── helpers.go     (3 utility helpers)
```

**Functions Needing Updates:**
```
/home/calum/code/apps/Tubarr/internal/server/helpers.go
  ├── buildSettingsFromInput()    (lines 429-538)
  ├── buildMetarrArgsFromInput()  (lines 540-687)
  └── fillChannelFromConfigFile() (lines 14-292) [reference]
```

**Data Structures:**
```
/home/calum/code/apps/Tubarr/internal/models/
  ├── settings.go  (Settings, MetarrArgs structs)
  └── vars.go      (ChannelInputPtrs struct)
```

## Validator Categories

1. **Path Validation** - ValidateDirectory(), ValidateFile()
2. **Extension Validation** - ValidateYtdlpOutputExtension(), ValidateOutputFiletype()
3. **Size/Format** - ValidateMaxFilesize(), ValidateMinFreeMem()
4. **Date** - ValidateToFromDate(), ValidateDateFormat()
5. **Operations** - ValidateFilterOps(), ValidateMoveOps(), ValidateFilenameOps(), ValidateMetaOps()
6. **Transcoding** - ValidateGPU(), ValidateVideoTranscodeCodecSlice(), ValidateAudioTranscodeCodecSlice()
7. **Config** - ValidateRenameFlag(), ValidateConcurrencyLimit(), ValidateNotificationStrings()
8. **Helpers** - EscapedSplit(), CheckForOpURL(), DeduplicateSliceEntries()

## Implementation Priority

### High Priority (Data Integrity)
- Add ValidateFile() for all *File fields
- Add ValidateMaxFilesize() in buildSettingsFromInput
- Add ValidateOutputFiletype() for MetarrArgs.OutputExt

### Medium Priority (Robustness)
- Add ValidateConcurrencyLimit() in buildMetarrArgsFromInput
- Validate integer ranges (CrawlFreq, Retries)
- Add hostname validation for BotBlockedHostnames

### Low Priority (Nice to Have)
- External downloader existence check
- Cookie browser type validation
- FFmpeg args validation

## Key Insights

### What IS Currently Validated

**In buildSettingsFromInput():**
- FromDate, ToDate (date format)
- YTDLPOutputExt (extension whitelist)
- DLFilters (operation format)
- MoveOps (operation format + directory)

**In buildMetarrArgsFromInput():**
- RenameStyle, MinFreeMem (format validation)
- TranscodeGPU (GPU type + device)
- All codec validations
- All operation validations
- OutputDir (directory existence)

### Error Handling Patterns

**Per-URL Settings (graceful degradation):**
```go
if err != nil {
    logging.W("Failed to validate X for per-URL settings: %v", err)
    // Skip the field but continue
}
```

**Main Channel Setup (strict validation):**
```go
if err != nil {
    http.Error(w, err.Error(), http.StatusBadRequest)
    return nil, ...  // Fail the entire operation
}
```

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

## Summary

- **Total documentation lines:** 1000+
- **Validators analyzed:** 30+
- **Struct fields analyzed:** 46
- **Validation gaps identified:** 9
- **Code examples provided:** 20+
- **Copy-paste blocks:** 15+

All documentation is self-contained and can be read independently or as a complete guide.

---

**Last Updated:** November 14, 2025
**Created by:** Validation Analysis Tool
**Location:** /home/calum/code/apps/Tubarr/
