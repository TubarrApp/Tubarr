package vars

import "sync"

// UpdateNewVideoURLMutex ensures new video URLs (for notification icons etc.) do not race.
var UpdateNewVideoURLMutex sync.Mutex
