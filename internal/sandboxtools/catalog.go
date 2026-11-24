package sandboxtools

import (
	_ "embed"
	"encoding/json"
)

//go:embed definitions.json
var catalog []byte

type Definition struct {
	Name        string          `json:"name"`
	RemoteName  string          `json:"remoteName"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

func Definitions() ([]Definition, error) {
	var tools []Definition
	err := json.Unmarshal(catalog, &tools)
	return tools, err
}
