// Package sessioncontract contains stable Session resource tokens and limits
// shared by API contract and persistence boundaries.
package sessioncontract

// EventFileBinding maps a Files API ID to one active Session mount. Path is
// the authoritative Filestore path and MimeType supports image validation.
type EventFileBinding struct {
	FileID   string
	Path     string
	MimeType string
}

const (
	FileResourceType = "file"

	// MaxResources is the official Claude managed-agents ceiling for the top-level
	// Session/Deployment resources array (mixed types).
	MaxResources = 500

	// MaxFileResources is the maximum number of file entries in that array.
	// Official docs allow files to fill the full resources budget, so this equals
	// MaxResources. Keep both names so call sites can state which limit they mean.
	MaxFileResources = MaxResources
)
