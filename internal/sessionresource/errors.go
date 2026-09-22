package sessionresource

// ReferenceError identifies a resource whose backing store could not be loaded.
type ReferenceError struct {
	ResourceType string
	ResourceID   string
	Err          error
}

func (e ReferenceError) Error() string { return e.ResourceType + " reference failed: " + e.ResourceID }
func (e ReferenceError) Unwrap() error { return e.Err }
