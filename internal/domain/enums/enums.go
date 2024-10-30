package domain

// User selection of filetypes to convert from
type ConvertFromFiletype int

const (
	IN_ALL_EXTENSIONS ConvertFromFiletype = iota
	IN_MKV            ConvertFromFiletype = iota
	IN_MP4            ConvertFromFiletype = iota
	IN_WEBM           ConvertFromFiletype = iota
	IN_MKVWEBM        ConvertFromFiletype = iota
)

// User system graphics hardware for transcoding
type SysGPU int

const (
	NVIDIA      SysGPU = iota
	AMD         SysGPU = iota
	INTEL       SysGPU = iota
	NO_HW_ACCEL SysGPU = iota
)

// Naming syle
type ReplaceToStyle int

const (
	SPACES      ReplaceToStyle = iota
	UNDERSCORES ReplaceToStyle = iota
	SKIP        ReplaceToStyle = iota
)

// Date formats
type FilenameDateFormat int

const (
	FILEDATE_YYYY_MM_DD FilenameDateFormat = iota
	FILEDATE_YY_MM_DD   FilenameDateFormat = iota
	FILEDATE_YYYY_DD_MM FilenameDateFormat = iota
	FILEDATE_YY_DD_MM   FilenameDateFormat = iota
	FILEDATE_DD_MM_YYYY FilenameDateFormat = iota
	FILEDATE_DD_MM_YY   FilenameDateFormat = iota
	FILEDATE_MM_DD_YYYY FilenameDateFormat = iota
	FILEDATE_MM_DD_YY   FilenameDateFormat = iota
	FILEDATE_SKIP       FilenameDateFormat = iota
)

// Web tags
type MetaFileTypeEnum int

const (
	METAFILE_JSON MetaFileTypeEnum = iota
	METAFILE_NFO  MetaFileTypeEnum = iota
	WEBCLASS_XML  MetaFileTypeEnum = iota
)

// Viper variable types
type ViperVarTypes int

const (
	VIPER_ANY          ViperVarTypes = iota
	VIPER_BOOL         ViperVarTypes = iota
	VIPER_INT          ViperVarTypes = iota
	VIPER_STRING       ViperVarTypes = iota
	VIPER_STRING_SLICE ViperVarTypes = iota
)

// Web tags
type WebClassTags int

const (
	WEBCLASS_DATE        WebClassTags = iota
	WEBCLASS_TITLE       WebClassTags = iota
	WEBCLASS_DESCRIPTION WebClassTags = iota
	WEBCLASS_CREDITS     WebClassTags = iota
	WEBCLASS_WEBINFO     WebClassTags = iota
)

// Presets
type SitePresets int

const (
	PRESET_CENSOREDTV SitePresets = iota
)
