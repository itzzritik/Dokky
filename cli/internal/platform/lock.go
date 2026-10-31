package platform

import "errors"

var ErrLocked = errors.New("lock is held by another process")
