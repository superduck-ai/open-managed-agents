package analytics

import "fmt"

func invalidQuery(field string) error { return fmt.Errorf("invalid analytics query: %s", field) }
