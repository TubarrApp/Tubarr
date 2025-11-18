package keys

import (
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
)

// BuildMetaOpsKey creates a unique key for meta operations
func BuildMetaOpsKey(mo models.MetaOps) string {
	var key string
	switch mo.OpType {
	case consts.OpDateTag, consts.OpDeleteDateTag:
		key = mo.Field + ":" + mo.OpType + ":" + mo.OpLoc + ":" + mo.DateFormat
	case consts.OpReplace:
		key = mo.Field + ":" + mo.OpType + ":" + mo.OpFindString + ":" + mo.OpValue
	default:
		key = mo.Field + ":" + mo.OpType + ":" + mo.OpValue
	}
	return key
}

// BuildMetaOpsKeyWithChannel creates a unique key for meta operations
func BuildMetaOpsKeyWithChannel(mo models.MetaOps) string {
	var key string
	switch mo.OpType {
	case consts.OpDateTag, consts.OpDeleteDateTag:
		key = mo.Field + ":" + mo.OpType + ":" + mo.OpLoc + ":" + mo.DateFormat
	case consts.OpReplace:
		key = mo.Field + ":" + mo.OpType + ":" + mo.OpFindString + ":" + mo.OpValue
	default:
		key = mo.Field + ":" + mo.OpType + ":" + mo.OpValue
	}

	if mo.ChannelURL != "" {
		key = mo.ChannelURL + "|" + key
	}
	return key
}

// BuildFilenameOpsKey creates a unique key for filename operations
func BuildFilenameOpsKey(fo models.FilenameOps) string {
	var key string
	switch fo.OpType {
	case consts.OpDateTag, consts.OpDeleteDateTag:
		key = fo.OpType + ":" + fo.OpLoc + ":" + fo.DateFormat
	case consts.OpReplace, consts.OpReplaceSuffix, consts.OpReplacePrefix:
		key = fo.OpType + ":" + fo.OpFindString + ":" + fo.OpValue
	default:
		key = fo.OpType + ":" + fo.OpValue
	}
	return key
}

// BuildFilenameOpsKeyWithChannel creates a unique key for filename operations
func BuildFilenameOpsKeyWithChannel(fo models.FilenameOps) string {
	var key string
	switch fo.OpType {
	case consts.OpDateTag, consts.OpDeleteDateTag:
		key = fo.OpType + ":" + fo.OpLoc + ":" + fo.DateFormat
	case consts.OpReplace, consts.OpReplaceSuffix, consts.OpReplacePrefix:
		key = fo.OpType + ":" + fo.OpFindString + ":" + fo.OpValue
	default:
		key = fo.OpType + ":" + fo.OpValue
	}

	if fo.ChannelURL != "" {
		key = fo.ChannelURL + "|" + key
	}
	return key
}
