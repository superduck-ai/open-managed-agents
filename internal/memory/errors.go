package memory

import "errors"

var errInvalidMemoryDepth = errors.New("depth must be 0 or 1")
var errUnalignedMemoryPrefix = errors.New("path_prefix must end with /")
