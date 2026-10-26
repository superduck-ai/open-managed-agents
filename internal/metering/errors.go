package metering

import "errors"

func missingRequestBody() error { return errors.New("metered request requires a replayable body") }
