// Package sessioncontract contains stable Session resource tokens and limits
// shared by API contract and persistence boundaries.
package sessioncontract

const (
	FileResourceType        = "file"
	MemoryStoreResourceType = "memory_store"

	// MaxResources is the official Claude managed-agents ceiling for the top-level
	// Session/Deployment resources array (mixed types).
	MaxResources = 500

	// MaxFileResources is the maximum number of file entries in that array.
	// Official docs allow files to fill the full resources budget, so this equals
	// MaxResources. Keep both names so call sites can state which limit they mean.
	MaxFileResources = MaxResources

	// MaxMemoryStores is the number of memory_store entries allowed on one
	// Session or Deployment. It is independent of MaxResources.
	MaxMemoryStores = 8

	// MaxMemoryInstructionsRunes is the Unicode code-point limit for attach
	// instructions. Over-limit requests are rejected; values are not truncated.
	MaxMemoryInstructionsRunes = 500
)
