package parsing

import "tubarr/internal/models"

// BuildMetaOpsKey creates a unique key for meta operations
func BuildMetaOpsKey(mo models.MetaOps) string {
	var key string
	switch mo.OpType {
	case "date-tag":
		key = mo.Field + ":" + mo.OpType + ":" + mo.OpLoc + ":" + mo.DateFormat
	default:
		key = mo.Field + ":" + mo.OpType + ":" + mo.OpValue
	}
	return key
}

// BuildMetaOpsKeyWithChannel creates a unique key for meta operations
func BuildMetaOpsKeyWithChannel(mo models.MetaOps) string {
	var key string
	switch mo.OpType {
	case "date-tag":
		key = mo.Field + ":" + mo.OpType + ":" + mo.OpLoc + ":" + mo.DateFormat
	default:
		key = mo.Field + ":" + mo.OpType + ":" + mo.OpValue
	}

	if mo.ChannelURL != "" {
		key = mo.ChannelURL + "|" + key
	}
	return key
}
