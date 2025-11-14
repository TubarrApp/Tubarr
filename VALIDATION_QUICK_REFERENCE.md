# Validation Functions Quick Reference

## All Available Validators (30+ functions)

### Common Validators (internal/validation/common.go)
| Function | Input | Output | Quick Use |
|----------|-------|--------|-----------|
| `ValidateDirectory()` | path, createIfNotFound | FileInfo, error | Directories |
| `ValidateFile()` | path, createIfNotFound | FileInfo, error | Files |
| `ValidateYtdlpOutputExtension()` | extension | error | yt-dlp ext: avi,flv,mkv,mov,mp4,webm |
| `ValidateMaxFilesize()` | size string | string, error | Format: 1K, 2M, 3G |
| `ValidateFilterOps()` | []string | []Filters, error | field:contains/omits:value:must/any |
| `ValidateMoveOps()` | []string | []MetaFilterMoveOps, error | field:value:output_dir |
| `ValidateFilteredMetaOps()` | []string | []FilteredMetaOps, error | Combined filters+meta |
| `ValidateFilteredFilenameOps()` | []string | []FilteredFilenameOps, error | Combined filters+filename |
| `ValidateToFromDate()` | date string | string, error | "today", 20250101, 2025y1m1d |
| `ValidateNotificationStrings()` | []string | []*Notification, error | URL\|Name format |

### Metarr Validators (internal/validation/metarr.go)
| Function | Input | Output | Quick Use |
|----------|-------|--------|-----------|
| `ValidateFilenameOps()` | []string | []FilenameOps, error | append, prefix, replace-*, date-tag |
| `ValidateMetaOps()` | []string | []MetaOps, error | append, copy-to, replace, set, date-tag |
| `ValidateRenameFlag()` | string | error | spaces, underscores, fixes-only, skip |
| `ValidateDateFormat()` | format | bool | Ymd, ymd, Ydm, dmY, dmy, md, dm |
| `ValidateMinFreeMem()` | memory | error | 1K, 2M, 3G format |
| `ValidateOutputFiletype()` | ext | string, error | FFmpeg extension |
| `ValidatePurgeMetafiles()` | type | bool | all, json, nfo |
| `ValidateGPU()` | gpu, dir | string, string, error | auto, intel, nvidia, amd |
| `ValidateVideoTranscodeCodecSlice()` | []string, accel | []string, error | h264, hevc, av1, mpeg2, vp8, vp9 |
| `ValidateAudioTranscodeCodecSlice()` | []string | []string, error | aac, mp3, opus, flac, vorbis, etc |
| `ValidateTranscodeQuality()` | quality | string, error | 0-51 numeric |
| `ValidateTranscodeVideoFilter()` | filter | string, error | No-op (passthrough) |

### Helpers (internal/validation/helpers.go)
| Function | Purpose | Returns |
|----------|---------|---------|
| `EscapedSplit()` | Split with backslash escaping | []string |
| `CheckForOpURL()` | Extract channel URL from operation | string, string |
| `DeduplicateSliceEntries()` | Remove duplicates from slice | []string |

---

## Fields That NEED Validation (Missing in buildSettingsFromInput)

### ConfigFile
- **Validator:** `ValidateFile(configFile, false)`
- **Status:** ❌ NOT VALIDATED
- **Why:** User may provide invalid paths

### FilterFile
- **Validator:** `ValidateFile(filterFile, false)`
- **Status:** ❌ NOT VALIDATED
- **Why:** File must exist before using

### MoveOpFile
- **Validator:** `ValidateFile(moveOpFile, false)`
- **Status:** ❌ NOT VALIDATED
- **Why:** File must exist before using

### MaxFilesize
- **Validator:** `ValidateMaxFilesize(maxFilesize)`
- **Status:** ❌ NOT VALIDATED (but exists!)
- **Why:** Validates format correctness

---

## Fields That NEED Validation (Missing in buildMetarrArgsFromInput)

### OutputExt
- **Validator:** `ValidateOutputFiletype(outputExt)`
- **Status:** ❌ NOT VALIDATED
- **Why:** Must be valid video extension

### FilenameOpsFile
- **Validator:** `ValidateFile(filenameOpsFile, false)`
- **Status:** ❌ NOT VALIDATED
- **Why:** File must exist

### MetaOpsFile
- **Validator:** `ValidateFile(metaOpsFile, false)`
- **Status:** ❌ NOT VALIDATED
- **Why:** File must exist

### FilteredMetaOpsFile
- **Validator:** `ValidateFile(filteredMetaOpsFile, false)`
- **Status:** ❌ NOT VALIDATED
- **Why:** File must exist

### FilteredFilenameOpsFile
- **Validator:** `ValidateFile(filteredFilenameOpsFile, false)`
- **Status:** ❌ NOT VALIDATED
- **Why:** File must exist

---

## What IS Being Validated

### In buildSettingsFromInput (Lines 429-538)
- ✅ FromDate - ValidateToFromDate()
- ✅ ToDate - ValidateToFromDate()
- ✅ YTDLPOutputExt - ValidateYtdlpOutputExtension()
- ✅ DLFilters - ValidateFilterOps()
- ✅ MoveOps - ValidateMoveOps()

### In buildMetarrArgsFromInput (Lines 540-687)
- ✅ RenameStyle - ValidateRenameFlag()
- ✅ MinFreeMem - ValidateMinFreeMem()
- ✅ TranscodeGPU - ValidateGPU()
- ✅ VideoCodec - ValidateVideoTranscodeCodecSlice()
- ✅ AudioCodec - ValidateAudioTranscodeCodecSlice()
- ✅ TranscodeQuality - ValidateTranscodeQuality()
- ✅ MetaOps - ValidateMetaOps()
- ✅ FilenameOps - ValidateFilenameOps()
- ✅ FilteredMetaOps - ValidateFilteredMetaOps()
- ✅ FilteredFilenameOps - ValidateFilteredFilenameOps()
- ✅ OutputDir - ValidateDirectory()

---

## Copy-Paste Code Blocks

### Add Missing Settings Validation
```go
// MaxFilesize validation (missing)
if input.MaxFilesize != nil && *input.MaxFilesize != "" {
    v, err := validation.ValidateMaxFilesize(*input.MaxFilesize)
    if err != nil {
        logging.W("Failed to validate max filesize for per-URL settings: %v", err)
    } else {
        settings.MaxFilesize = v
    }
}

// ConfigFile validation (missing)
if input.ConfigFile != nil && *input.ConfigFile != "" {
    if _, err := validation.ValidateFile(*input.ConfigFile, false); err != nil {
        logging.W("Failed to validate config file for per-URL settings: %v", err)
    }
}

// FilterFile validation (missing)
if input.DLFilterFile != nil && *input.DLFilterFile != "" {
    if _, err := validation.ValidateFile(*input.DLFilterFile, false); err != nil {
        logging.W("Failed to validate filter file for per-URL settings: %v", err)
    }
}

// MoveOpFile validation (missing)
if input.MoveOpFile != nil && *input.MoveOpFile != "" {
    if _, err := validation.ValidateFile(*input.MoveOpFile, false); err != nil {
        logging.W("Failed to validate move op file for per-URL settings: %v", err)
    }
}
```

### Add Missing Metarr Validation
```go
// OutputExt validation (missing)
if input.MetarrExt != nil && *input.MetarrExt != "" {
    dottedExt, err := validation.ValidateOutputFiletype(*input.MetarrExt)
    if err != nil {
        logging.W("Failed to validate output extension for per-URL settings: %v", err)
    } else {
        metarr.OutputExt = dottedExt
    }
}

// FilenameOpsFile validation (missing)
if input.FilenameOpsFile != nil && *input.FilenameOpsFile != "" {
    if _, err := validation.ValidateFile(*input.FilenameOpsFile, false); err != nil {
        logging.W("Failed to validate filename ops file for per-URL settings: %v", err)
    }
}

// MetaOpsFile validation (missing)
if input.MetaOpsFile != nil && *input.MetaOpsFile != "" {
    if _, err := validation.ValidateFile(*input.MetaOpsFile, false); err != nil {
        logging.W("Failed to validate meta ops file for per-URL settings: %v", err)
    }
}

// FilteredMetaOpsFile validation (missing)
if input.FilteredMetaOpsFile != nil && *input.FilteredMetaOpsFile != "" {
    if _, err := validation.ValidateFile(*input.FilteredMetaOpsFile, false); err != nil {
        logging.W("Failed to validate filtered meta ops file for per-URL settings: %v", err)
    }
}

// FilteredFilenameOpsFile validation (missing)
if input.FilteredFilenameOpsFile != nil && *input.FilteredFilenameOpsFile != "" {
    if _, err := validation.ValidateFile(*input.FilteredFilenameOpsFile, false); err != nil {
        logging.W("Failed to validate filtered filename ops file for per-URL settings: %v", err)
    }
}

// Concurrency validation (optional improvement)
if input.MetarrConcurrency != nil {
    metarr.Concurrency = validation.ValidateConcurrencyLimit(*input.MetarrConcurrency)
}
```

---

## File Locations

- Validators: `/home/calum/code/apps/Tubarr/internal/validation/`
  - common.go, metarr.go, db.go, helpers.go

- Builder Functions: `/home/calum/code/apps/Tubarr/internal/server/helpers.go`
  - buildSettingsFromInput() lines 429-538
  - buildMetarrArgsFromInput() lines 540-687
  - fillChannelFromConfigFile() lines 14-292

- Structs: `/home/calum/code/apps/Tubarr/internal/models/`
  - settings.go (Settings, MetarrArgs)
  - vars.go (ChannelInputPtrs)

---

## Stats

- **Total Validators:** 30+
- **Settings Fields:** 23 (11 validated = 48%)
- **MetarrArgs Fields:** 23 (16 validated = 70%)
- **Overall Coverage:** 46 fields (27 validated = 59%)
- **Missing Critical Validations:** 8 file path fields
