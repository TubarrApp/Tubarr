# Tubarr Validation Functions - Comprehensive Analysis

## Overview
This document provides a complete analysis of all validation functions available in the validation package and identifies which fields in `Settings` and `MetarrArgs` structs should be validated.

---

## Part 1: All Validation Functions Available

### File: `internal/validation/common.go`

| Function | Purpose | Input | Returns | Notes |
|----------|---------|-------|---------|-------|
| `ValidateMetarrOutputDirs()` | Validates output directories for Metarr | defaultDir (string), urlDirs ([]string), channel (*Channel) | map[string]string, error | Parses URL\|directory pairs; deduplicates entries |
| `ValidateDirectory()` | Validates/creates a directory path | dir (string), createIfNotFound (bool) | os.FileInfo, error | Supports template tags ({{...}}); creates if needed |
| `ValidateFile()` | Validates/creates a file path | f (string), createIfNotFound (bool) | os.FileInfo, error | Can auto-create if missing; validates not a directory |
| `ValidateViperFlags()` | Validates all viper flag inputs | none | error | Checks OutputFiletype, MetaPurge, logging level, concurrency |
| `ValidateConcurrencyLimit()` | Ensures concurrency is >= 1 | c (int) | int | Returns max(c, 1) |
| `ValidateNotificationStrings()` | Parses notification pairs | notifications ([]string) | []*Notification, error | Format: 'URL\|Name' or 'ChanURL\|NotifyURL\|Name' |
| `ValidateYtdlpOutputExtension()` | Validates yt-dlp output extension | e (string) | error | Valid: avi, flv, mkv, mov, mp4, webm |
| `ValidateLoggingLevel()` | Validates/sets debug level | none | void | Clamps to 0-5 range |
| `ValidateMaxFilesize()` | Validates max filesize for yt-dlp | input (string) | string, error | Format: number with optional K/M/G suffix |
| `ValidateFilterOps()` | Validates download/operation filters | ops ([]string) | []Filters, error | Format: 'field:contains\|omits:value:must\|any' |
| `ValidateMoveOps()` | Validates metadata filter move operations | ops ([]string) | []MetaFilterMoveOps, error | Format: 'field:value:output_directory' |
| `ValidateFilteredMetaOps()` | Validates filter-based meta operations | filteredMetaOps ([]string) | []FilteredMetaOps, error | Combines filter rules with meta operations |
| `ValidateFilteredFilenameOps()` | Validates filter-based filename operations | filteredFilenameOps ([]string) | []FilteredFilenameOps, error | Combines filter rules with filename operations |
| `ValidateToFromDate()` | Validates date in Ymd or formatted style | d (string) | string, error | Supports 'today' keyword; returns yyyymmdd format |
| `WarnMalformedKeys()` | Warns on mixed-case config keys | none | void | Checks for mixed dashes and underscores |

### File: `internal/validation/metarr.go`

| Function | Purpose | Input | Returns | Notes |
|----------|---------|-------|---------|-------|
| `ValidateFilenameOps()` | Validates filename transformation operations | filenameOps ([]string) | []FilenameOps, error | Valid actions: append, prefix, replace-*, date-tag, delete-date-tag, set |
| `ValidateMetaOps()` | Validates metadata transformation operations | metaOps ([]string) | []MetaOps, error | Valid actions: append, copy-to, paste-from, prefix, replace-*, set, date-tag, delete-date-tag |
| `ValidateRenameFlag()` | Validates rename style flag | flag (string) | error | Valid: 'spaces', 'underscores', 'fixes-only', 'skip' |
| `ValidateDateFormat()` | Validates date format pattern | dateFmt (string) | bool | Valid: Ymd, ymd, Ydm, ydm, dmY, dmy, mdY, mdy, md, dm |
| `ValidateMinFreeMem()` | Validates minimum free memory requirement | minFreeMem (string) | error | Format: number with K/M/G suffix or raw bytes |
| `ValidateOutputFiletype()` | Validates FFmpeg output filetype | o (string) | string, error | Adds leading dot; validates against AllVidExtensions |
| `ValidatePurgeMetafiles()` | Validates metafile purge type | purgeType (string) | bool | Valid: 'all', 'json', 'nfo' |
| `ValidateGPU()` | Validates GPU acceleration selection | g (string), devDir (string) | string, string, error | Supports aliases: automatic, radeon/amd, intel, nvidia/nvenc |
| `ValidateAudioTranscodeCodecSlice()` | Validates audio codec mappings | pairs ([]string) | []string, error | Format: 'codec' or 'input:output' |
| `validateTranscodeAudioCodec()` | Validates individual audio codec | a (string) | string, error | Supported: AAC, ALAC, DTS, EAC3, FLAC, MP2, MP3, Opus, PCM, TrueHD, Vorbis, WAV |
| `ValidateVideoTranscodeCodecSlice()` | Validates video codec mappings | pairs ([]string), accel (string) | []string, error | Format: 'codec' or 'input:output' |
| `validateVideoTranscodeCodec()` | Validates individual video codec | c (string), accel (string) | string, error | Supported: AV1, H264, HEVC, MPEG2, VP8, VP9 |
| `ValidateTranscodeQuality()` | Validates transcode quality preset | q (string) | string, error | Numeric 0-51, clamped and returned as string |
| `ValidateTranscodeVideoFilter()` | Validates transcode video filter | q (string) | string, error | Currently no validation (returns input as-is) |

### File: `internal/validation/db.go`

| Function | Purpose | Input | Returns | Notes |
|----------|---------|-------|---------|-------|
| `ValidateColumnKeyVal()` | Validates database column key-value pair | key (string), val (string) | error | Checks against consts.ValidDBColumns |

### File: `internal/validation/helpers.go`

| Function | Purpose | Input | Returns | Notes |
|----------|---------|-------|---------|-------|
| `EscapedSplit()` | Splits string with escape character support | s (string), desiredSeparator (rune) | []string | Supports backslash escaping of separator |
| `CheckForOpURL()` | Extracts channel URL from operation | op (string) | string, string | Returns (channelURL, operation) pair |
| `DeduplicateSliceEntries()` | Removes duplicate string entries | input ([]string) | []string | Preserves order; logs duplicates as warnings |

---

## Part 2: Settings Struct - Fields and Validation Status

### File: `internal/models/settings.go`

```go
type Settings struct {
    // Configurations
    ConfigFile  string
    Concurrency int
    
    // Download-related operations
    CookiesFromBrowser     string
    CrawlFreq              int
    ExternalDownloader     string
    ExternalDownloaderArgs string
    MaxFilesize            string
    Retries                int
    UseGlobalCookies       bool
    YtdlpOutputExt         string
    
    // Custom args
    ExtraYTDLPVideoArgs string
    ExtraYTDLPMetaArgs  string
    
    // Metadata operations
    Filters    []Filters
    FilterFile string
    MoveOps    []MetaFilterMoveOps
    MoveOpFile string
    FromDate   string
    ToDate     string
    
    // JSON and video directories
    JSONDir  string
    VideoDir string
    
    // Bot blocking elements
    BotBlocked           bool
    BotBlockedHostnames  []string
    BotBlockedTimestamps map[string]time.Time
    
    // Channel toggles
    Paused bool
}
```

### Validation Status by Field

| Field | Type | Currently Validated? | Available Validator | Location | Notes |
|-------|------|---------------------|-------------------|----------|-------|
| **ConfigFile** | string | ❌ NO | `ValidateFile()` | Not checked | Should validate file exists |
| **Concurrency** | int | ✅ YES | `ValidateConcurrencyLimit()` | helpers.go:239 | Ensures >= 1 |
| **CookiesFromBrowser** | string | ❌ NO | None | - | Should validate browser type |
| **CrawlFreq** | int | ❌ NO | None | - | Could validate > 0 |
| **ExternalDownloader** | string | ❌ NO | None | - | Could validate executable exists |
| **ExternalDownloaderArgs** | string | ❌ NO | None | - | No validation needed (free text) |
| **MaxFilesize** | string | ✅ YES | `ValidateMaxFilesize()` | helpers.go:248 | Validates format |
| **Retries** | int | ❌ NO | None | - | Could validate >= 0 |
| **UseGlobalCookies** | bool | ✅ YES | N/A | - | Boolean, no validation needed |
| **YtdlpOutputExt** | string | ✅ YES | `ValidateYtdlpOutputExtension()` | helpers.go:160,509 | Validates against allowed extensions |
| **ExtraYTDLPVideoArgs** | string | ❌ NO | None | - | No validation (user args) |
| **ExtraYTDLPMetaArgs** | string | ❌ NO | None | - | No validation (user args) |
| **Filters** | []Filters | ✅ YES | `ValidateFilterOps()` | helpers.go:37,519 | Validates filter format |
| **FilterFile** | string | ❌ NO | `ValidateFile()` | Not checked | Should validate file exists |
| **MoveOps** | []MetaFilterMoveOps | ✅ YES | `ValidateMoveOps()` | helpers.go:45,529 | Validates format + directory |
| **MoveOpFile** | string | ❌ NO | `ValidateFile()` | Not checked | Should validate file exists |
| **FromDate** | string | ✅ YES | `ValidateToFromDate()` | helpers.go:106,491 | Validates date format |
| **ToDate** | string | ✅ YES | `ValidateToFromDate()` | helpers.go:114,499 | Validates date format |
| **JSONDir** | string | ✅ YES | `ValidateDirectory()` | helpers.go:30 | Validates directory exists/creates |
| **VideoDir** | string | ✅ YES | `ValidateDirectory()` | helpers.go:30,255 | Validates directory exists/creates |
| **BotBlocked** | bool | ✅ YES | N/A | - | Boolean, no validation needed |
| **BotBlockedHostnames** | []string | ❌ NO | None | - | Could validate hostname format |
| **BotBlockedTimestamps** | map[string]time.Time | ✅ YES | N/A | - | Already time.Time type |
| **Paused** | bool | ✅ YES | N/A | - | Boolean, no validation needed |

---

## Part 3: MetarrArgs Struct - Fields and Validation Status

### File: `internal/models/settings.go`

```go
type MetarrArgs struct {
    // Metarr file operations
    OutputExt               string
    FilenameOps             []FilenameOps
    FilenameOpsFile         string
    FilteredFilenameOps     []FilteredFilenameOps
    FilteredFilenameOpsFile string
    RenameStyle             string
    
    // Metarr metadata operations
    MetaOps             []MetaOps
    MetaOpsFile         string
    FilteredMetaOps     []FilteredMetaOps
    FilteredMetaOpsFile string
    
    // Metarr output directories
    OutputDir     string
    OutputDirMap  map[string]string
    URLOutputDirs []string
    
    // Program operations
    Concurrency int
    MaxCPU      float64
    MinFreeMem  string
    
    // FFmpeg transcoding operations
    UseGPU               string
    GPUDir               string
    TranscodeVideoFilter string
    TranscodeVideoCodecs []string
    TranscodeAudioCodecs []string
    TranscodeQuality     string
    ExtraFFmpegArgs      string
}
```

### Validation Status by Field

| Field | Type | Currently Validated? | Available Validator | Location | Notes |
|-------|------|---------------------|-------------------|----------|-------|
| **OutputExt** | string | ❌ NO | `ValidateOutputFiletype()` | Not checked in helpers | Should validate extension |
| **FilenameOps** | []FilenameOps | ✅ YES | `ValidateFilenameOps()` | helpers.go:62,657 | Validates operation format |
| **FilenameOpsFile** | string | ❌ NO | `ValidateFile()` | Not checked | Should validate file exists |
| **FilteredFilenameOps** | []FilteredFilenameOps | ✅ YES | `ValidateFilteredFilenameOps()` | helpers.go:82,677 | Validates combined format |
| **FilteredFilenameOpsFile** | string | ❌ NO | `ValidateFile()` | Not checked | Should validate file exists |
| **RenameStyle** | string | ✅ YES | `ValidateRenameFlag()` | helpers.go:91,586 | Validates rename style |
| **MetaOps** | []MetaOps | ✅ YES | `ValidateMetaOps()` | helpers.go:52,647 | Validates operation format |
| **MetaOpsFile** | string | ❌ NO | `ValidateFile()` | Not checked | Should validate file exists |
| **FilteredMetaOps** | []FilteredMetaOps | ✅ YES | `ValidateFilteredMetaOps()` | helpers.go:72,667 | Validates combined format |
| **FilteredMetaOpsFile** | string | ❌ NO | `ValidateFile()` | Not checked | Should validate file exists |
| **OutputDir** | string | ✅ YES | `ValidateDirectory()` | helpers.go:566 | Validates directory (with create) |
| **OutputDirMap** | map[string]string | ✅ YES | `ValidateMetarrOutputDirs()` | Not directly used | Used when merging URLs with dirs |
| **URLOutputDirs** | []string | ❌ PARTIAL | `ValidateMetarrOutputDirs()` | Not checked | Could validate URLs format |
| **Concurrency** | int | ❌ NO | `ValidateConcurrencyLimit()` | Not checked | Could ensure >= 1 |
| **MaxCPU** | float64 | ✅ YES | N/A (clamped) | helpers.go:582 | Clamped to 0.0-100.0 |
| **MinFreeMem** | string | ✅ YES | `ValidateMinFreeMem()` | helpers.go:98,595 | Validates memory format |
| **UseGPU** | string | ✅ YES | `ValidateGPU()` | helpers.go:123,604 | Validates GPU type |
| **GPUDir** | string | ✅ YES | `ValidateGPU()` | helpers.go:123,604 | Part of GPU validation |
| **TranscodeVideoFilter** | string | ✅ YES | `ValidateTranscodeVideoFilter()` | helpers.go:151,571 | Currently no-op (returns input) |
| **TranscodeVideoCodecs** | []string | ✅ YES | `ValidateVideoTranscodeCodecSlice()` | helpers.go:133,615 | Validates codec format |
| **TranscodeAudioCodecs** | []string | ✅ YES | `ValidateAudioTranscodeCodecSlice()` | helpers.go:142,625 | Validates codec format |
| **TranscodeQuality** | string | ✅ YES | `ValidateTranscodeQuality()` | helpers.go:151,637 | Validates quality (0-51) |
| **ExtraFFmpegArgs** | string | ❌ NO | None | - | No validation (user args) |

---

## Part 4: Validation Gaps & Recommendations

### Critical Gaps (Validation Missing, but Available)

1. **String File Paths** - Not validated:
   - `Settings.ConfigFile` - Should use `ValidateFile()`
   - `Settings.FilterFile` - Should use `ValidateFile()`
   - `Settings.MoveOpFile` - Should use `ValidateFile()`
   - `MetarrArgs.FilenameOpsFile` - Should use `ValidateFile()`
   - `MetarrArgs.FilteredFilenameOpsFile` - Should use `ValidateFile()`
   - `MetarrArgs.MetaOpsFile` - Should use `ValidateFile()`
   - `MetarrArgs.FilteredMetaOpsFile` - Should use `ValidateFile()`

2. **String Validation** - Could be improved:
   - `Settings.CookiesFromBrowser` - No validator exists; could check against known browsers
   - `Settings.ExternalDownloader` - Should validate executable exists
   - `MetarrArgs.OutputExt` - Should use `ValidateOutputFiletype()`
   - `MetarrArgs.URLOutputDirs` - Should validate URL format

3. **Integer Validation** - Missing:
   - `Settings.CrawlFreq` - Could validate > 0
   - `Settings.Retries` - Could validate >= 0
   - `MetarrArgs.Concurrency` - Could validate >= 1

4. **Array Validation** - Missing:
   - `Settings.BotBlockedHostnames` - Could validate hostname format

### Where Validation Currently Happens

**In `buildSettingsFromInput()` (lines 429-538):**
- FromDate, ToDate (date validation)
- YTDLPOutputExt (extension validation)
- DLFilters (filter validation)
- MoveOps (move operation validation)
- Maxfilesize - NOT called here (missing!)

**In `buildMetarrArgsFromInput()` (lines 540-687):**
- RenameStyle, MinFreeMem, TranscodeGPU, VideoCodec, AudioCodec, TranscodeQuality
- MetaOps, FilenameOps, FilteredMetaOps, FilteredFilenameOps
- OutputDir (directory validation)

**In `fillChannelFromConfigFile()` (lines 14-292):**
- All file operations, directory creation/validation
- All codec validations
- All operation validations
- Most comprehensive validation path

---

## Part 5: Validation Patterns Used Elsewhere

### Pattern 1: Directory Validation with Creation
```go
validation.ValidateDirectory(dir, true)  // true = create if not found
```

### Pattern 2: File Validation
```go
validation.ValidateFile(file, false)  // false = don't create
```

### Pattern 3: Slice Deduplication
```go
validation.DeduplicateSliceEntries(slice)
```

### Pattern 4: Error Handling in Helpers
```go
if err := validation.SomeValidator(...); err != nil {
    http.Error(w, err.Error(), http.StatusBadRequest)
    return nil, make(map[string]*models.ChannelAccessDetails)
}
```

### Pattern 5: Per-URL Override Warning
```go
logging.W("Failed to validate X for per-URL settings: %v", err)
```

---

## Summary Statistics

- **Total Validation Functions:** 30+
- **Settings Fields:** 23 total
  - Currently Validated: 11 (48%)
  - Missing Validation: 12 (52%)
- **MetarrArgs Fields:** 23 total
  - Currently Validated: 16 (70%)
  - Missing Validation: 7 (30%)
- **Total Struct Fields:** 46
  - Currently Validated: 27 (59%)
  - Missing Validation: 19 (41%)

---

## Recommendations Priority Order

### High Priority
1. Add `ValidateFile()` calls for all file path fields
2. Add `ValidateMaxFilesize()` in buildSettingsFromInput
3. Add `ValidateOutputFiletype()` for MetarrArgs.OutputExt
4. Add URL validation for MetarrArgs.URLOutputDirs

### Medium Priority
1. Validate integer ranges (CrawlFreq > 0, Retries >= 0, Concurrency >= 1)
2. Add hostname validation for BotBlockedHostnames
3. Add external downloader existence check

### Low Priority
1. Cookie browser validation
2. FFmpeg args validation (likely not needed - user input)
3. YTDLP args validation (user input)

---

