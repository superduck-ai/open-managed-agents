package agentsnapshot

import "errors"

var errRosterInferenceGeoMismatch = errors.New("multiagent referenced agents must have the same inference_geo as the parent")
