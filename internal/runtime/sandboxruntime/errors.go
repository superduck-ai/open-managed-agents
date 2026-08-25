// Package sandboxruntime defines provider-independent sandbox lifecycle errors.
package sandboxruntime

import "errors"

// ErrSandboxNotFound means the provider has permanently removed a sandbox.
var ErrSandboxNotFound = errors.New("sandbox not found")
