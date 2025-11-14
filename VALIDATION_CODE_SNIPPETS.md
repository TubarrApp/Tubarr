# Tubarr Validation Functions - Code Snippets & Quick Reference

## File Locations

- **Validation Package:** `/home/calum/code/apps/Tubarr/internal/validation/`
  - `common.go` (15 functions)
  - `metarr.go` (14 functions)
  - `db.go` (1 function)
  - `helpers.go` (3 helper functions)

- **Helper Functions:** `/home/calum/code/apps/Tubarr/internal/server/helpers.go`
  - `buildSettingsFromInput()` (lines 429-538)
  - `buildMetarrArgsFromInput()` (lines 540-687)
  - `fillChannelFromConfigFile()` (lines 14-292)

- **Model Definitions:** `/home/calum/code/apps/Tubarr/internal/models/`
  - `settings.go` (Settings, MetarrArgs, and related structs)
  - `vars.go` (ChannelInputPtrs struct)

---

## Available Validators - Quick Reference

### Directory & File Validation
```go
// Validate directory exists (or create if needed)
fileInfo, err := validation.ValidateDirectory(dir, createIfNotFound bool)

// Validate file exists (or create if needed)
fileInfo, err := validation.ValidateFile(file, createIfNotFound bool)
```

### Extension Validation
```go
// Validate yt-dlp output extension (avi, flv, mkv, mov, mp4, webm)
err := validation.ValidateYtdlpOutputExtension(ext string)

// Validate FFmpeg output filetype (returns with leading dot)
dottedExt, err := validation.ValidateOutputFiletype(ext string)
```

### Size Validation
```go
// Validate max filesize format (1K, 2M, 3G, etc)
result, err := validation.ValidateMaxFilesize(input string)

// Validate minimum free memory (1K, 2M, 3G, etc)
err := validation.ValidateMinFreeMem(minFreeMem string)
```

### Date Validation
```go
// Validate date (supports "today", "20250101", "2025y01m01d", etc)
// Returns in yyyymmdd format
result, err := validation.ValidateToFromDate(dateStr string)
```

### Operation Validation
```go
// Validate filter operations
filters, err := validation.ValidateFilterOps(ops []string)

// Validate move operations
moveOps, err := validation.ValidateMoveOps(ops []string)

// Validate filename transformations
filenameOps, err := validation.ValidateFilenameOps(filenameOps []string)

// Validate metadata transformations
metaOps, err := validation.ValidateMetaOps(metaOps []string)

// Validate filtered operations (combined filter + ops)
filteredMetaOps, err := validation.ValidateFilteredMetaOps(filteredMetaOps []string)
filteredFilenameOps, err := validation.ValidateFilteredFilenameOps(filteredFilenameOps []string)
```

### Transcoding Validation
```go
// Validate GPU selection
gpuType, gpuDir, err := validation.ValidateGPU(gpuStr string, deviceDir string)

// Validate video codec mappings
codecs, err := validation.ValidateVideoTranscodeCodecSlice(pairs []string, accelType string)

// Validate audio codec mappings
codecs, err := validation.ValidateAudioTranscodeCodecSlice(pairs []string)

// Validate transcode quality (0-51)
quality, err := validation.ValidateTranscodeQuality(q string)

// Validate video filter (no-op currently)
result, err := validation.ValidateTranscodeVideoFilter(filter string)
```

### Rename & Format Validation
```go
// Validate rename style (spaces, underscores, fixes-only, skip)
err := validation.ValidateRenameFlag(flag string)

// Validate date format (Ymd, ymd, dmY, dmy, md, dm, etc)
isValid := validation.ValidateDateFormat(dateFmt string)
```

### Other Validation
```go
// Validate concurrency limit (returns max(input, 1))
result := validation.ValidateConcurrencyLimit(c int)

// Validate notification strings
notifications, err := validation.ValidateNotificationStrings(notifications []string)

// Parse and validate Metarr output directory mappings
dirMap, err := validation.ValidateMetarrOutputDirs(defaultDir string, urlDirs []string, channel *Channel)
```

### Helper Functions
```go
// Split string with escape character support
parts := validation.EscapedSplit(s string, separator rune)

// Extract channel URL from operation
chanURL, ops := validation.CheckForOpURL(op string)

// Remove duplicates from string slice
deduped := validation.DeduplicateSliceEntries(input []string)
```

---

## Usage Examples from Code

### Example 1: Directory Validation in fillChannelFromConfigFile
```go
if _, err := validation.ValidateDirectory(*input.VideoDir, true); err != nil {
    http.Error(w, err.Error(), http.StatusBadRequest)
    return nil, make(map[string]*models.ChannelAccessDetails)
}
```

### Example 2: Filter Operation Validation in buildSettingsFromInput
```go
if input.DLFilters != nil {
    m, err := validation.ValidateFilterOps(*input.DLFilters)
    if err != nil {
        logging.W("Failed to validate filter ops for per-URL settings: %v", err)
    } else {
        settings.Filters = m
    }
}
```

### Example 3: Date Validation in buildSettingsFromInput
```go
if input.FromDate != nil {
    v, err := validation.ValidateToFromDate(*input.FromDate)
    if err != nil {
        logging.W("Failed to validate from date for per-URL settings: %v", err)
    } else {
        settings.FromDate = v
    }
}
```

### Example 4: GPU and Codec Validation in buildMetarrArgsFromInput
```go
if input.TranscodeGPU != nil {
    g, d, err := validation.ValidateGPU(*input.TranscodeGPU, parsing.NilOrZeroValue(input.GPUDir))
    if err != nil {
        logging.W("Failed to validate GPU settings for per-URL settings: %v", err)
    } else {
        metarr.UseGPU = g
        metarr.GPUDir = d
    }
}

if input.VideoCodec != nil {
    c, err := validation.ValidateVideoTranscodeCodecSlice(*input.VideoCodec, metarr.UseGPU)
    if err != nil {
        logging.W("Failed to validate video codecs for per-URL settings: %v", err)
    } else {
        metarr.TranscodeVideoCodecs = c
    }
}
```

---

## Supported Formats & Values

### Valid YTDLP Output Extensions
- avi, flv, mkv, mov, mp4, webm

### Valid Rename Styles
- spaces, underscores, fixes-only, skip

### Valid Date Formats
- Ymd, ymd (4-digit year)
- Ydm, ydm
- dmY, dmy
- mdY, mdy
- md, dm (2-digit patterns)

### Valid GPU Types
- auto, automatic, automated
- intel
- nvidia, nvenc
- amd, radeon

### Valid Audio Codecs
- AAC, ALAC, DTS, EAC3, FLAC, MP2, MP3, Opus, PCM, TrueHD, Vorbis, WAV

### Valid Video Codecs
- AV1, H264, HEVC, MPEG2, VP8, VP9

### Valid Filename Operations
- append, prefix, replace-prefix, replace-suffix, replace
- date-tag, delete-date-tag, set

### Valid Meta Operations
- append, copy-to, paste-from, prefix
- replace, replace-prefix, replace-suffix, set
- date-tag, delete-date-tag

---

## Current vs Missing Validation in buildSettingsFromInput

### Currently Validated (Lines Shown)
```go
// Line 106-111: FromDate
if input.FromDate != nil && *input.FromDate != "" {
    v, err := validation.ValidateToFromDate(*input.FromDate)
    // ... error handling
    input.FromDate = &v
}

// Line 114-120: ToDate
if input.ToDate != nil && *input.ToDate != "" {
    v, err := validation.ValidateToFromDate(*input.ToDate)
    // ... error handling
    input.ToDate = &v
}

// Line 160-166: YTDLPOutputExt
if input.YTDLPOutputExt != nil && *input.YTDLPOutputExt != "" {
    v := strings.ToLower(*input.YTDLPOutputExt)
    if err := validation.ValidateYtdlpOutputExtension(v); err != nil {
        // ... error handling
    }
    input.YTDLPOutputExt = &v
}
```

### Missing Validation (Should Add)
```go
// ConfigFile - needs ValidateFile()
if input.ConfigFile != nil && *input.ConfigFile != "" {
    if _, err := validation.ValidateFile(*input.ConfigFile, false); err != nil {
        // handle error
    }
}

// MaxFilesize - needs ValidateMaxFilesize()
if input.MaxFilesize != nil && *input.MaxFilesize != "" {
    v, err := validation.ValidateMaxFilesize(*input.MaxFilesize)
    if err != nil {
        // handle error
    }
    input.MaxFilesize = &v
}

// FilterFile - needs ValidateFile()
if input.DLFilterFile != nil && *input.DLFilterFile != "" {
    if _, err := validation.ValidateFile(*input.DLFilterFile, false); err != nil {
        // handle error
    }
}

// MoveOpFile - needs ValidateFile()
if input.MoveOpFile != nil && *input.MoveOpFile != "" {
    if _, err := validation.ValidateFile(*input.MoveOpFile, false); err != nil {
        // handle error
    }
}
```

---

## Current vs Missing Validation in buildMetarrArgsFromInput

### Currently Validated (Lines Shown)
```go
// Line 586-591: RenameStyle
if input.RenameStyle != nil {
    if err := validation.ValidateRenameFlag(*input.RenameStyle); err != nil {
        logging.W("Failed to validate rename style for per-URL settings: %v", err)
    } else {
        metarr.RenameStyle = *input.RenameStyle
    }
}

// Line 595-600: MinFreeMem
if input.MinFreeMem != nil {
    if err := validation.ValidateMinFreeMem(*input.MinFreeMem); err != nil {
        logging.W("Failed to validate min free mem for per-URL settings: %v", err)
    } else {
        metarr.MinFreeMem = *input.MinFreeMem
    }
}

// Lines 604-611: TranscodeGPU + GPUDir
if input.TranscodeGPU != nil {
    g, d, err := validation.ValidateGPU(*input.TranscodeGPU, parsing.NilOrZeroValue(input.GPUDir))
    if err != nil {
        logging.W("Failed to validate GPU settings for per-URL settings: %v", err)
    } else {
        metarr.UseGPU = g
        metarr.GPUDir = d
    }
}
```

### Missing Validation (Should Add)
```go
// OutputExt - needs ValidateOutputFiletype()
if input.MetarrExt != nil && *input.MetarrExt != "" {
    dottedExt, err := validation.ValidateOutputFiletype(*input.MetarrExt)
    if err != nil {
        logging.W("Failed to validate output extension for per-URL settings: %v", err)
    } else {
        metarr.OutputExt = dottedExt
    }
}

// FilenameOpsFile - needs ValidateFile()
if input.FilenameOpsFile != nil && *input.FilenameOpsFile != "" {
    if _, err := validation.ValidateFile(*input.FilenameOpsFile, false); err != nil {
        logging.W("Failed to validate filename ops file for per-URL settings: %v", err)
    }
}

// MetaOpsFile - needs ValidateFile()
if input.MetaOpsFile != nil && *input.MetaOpsFile != "" {
    if _, err := validation.ValidateFile(*input.MetaOpsFile, false); err != nil {
        logging.W("Failed to validate meta ops file for per-URL settings: %v", err)
    }
}

// FilteredMetaOpsFile - needs ValidateFile()
if input.FilteredMetaOpsFile != nil && *input.FilteredMetaOpsFile != "" {
    if _, err := validation.ValidateFile(*input.FilteredMetaOpsFile, false); err != nil {
        logging.W("Failed to validate filtered meta ops file for per-URL settings: %v", err)
    }
}

// FilteredFilenameOpsFile - needs ValidateFile()
if input.FilteredFilenameOpsFile != nil && *input.FilteredFilenameOpsFile != "" {
    if _, err := validation.ValidateFile(*input.FilteredFilenameOpsFile, false); err != nil {
        logging.W("Failed to validate filtered filename ops file for per-URL settings: %v", err)
    }
}

// Concurrency - could use ValidateConcurrencyLimit()
if input.MetarrConcurrency != nil {
    metarr.Concurrency = validation.ValidateConcurrencyLimit(*input.MetarrConcurrency)
}
```

---

## Error Handling Pattern Used in Per-URL Settings

When validation fails in `buildSettingsFromInput` or `buildMetarrArgsFromInput`:

```go
if err != nil {
    logging.W("Failed to validate X for per-URL settings: %v", err)
    // Don't return error - gracefully skip the field
} else {
    // Set the field with validated value
}
```

This is different from `fillChannelFromConfigFile` which uses HTTP error responses:

```go
if err != nil {
    http.Error(w, err.Error(), http.StatusBadRequest)
    return nil, make(map[string]*models.ChannelAccessDetails)
}
```

