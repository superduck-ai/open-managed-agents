package transcriptretention

import "errors"

var (
	errStorage   = errors.New("transcript archive storage unavailable")
	errIntegrity = errors.New("transcript archive does not match persisted events")
	errBudget    = errors.New("transcript archive exceeds job row budget")
)
