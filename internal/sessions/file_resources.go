package sessions

import (
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"

	"github.com/samber/lo"
)

// normalizedSessionResource keeps the typed File contract beside the
// persistence row produced from it. The API boundary therefore never has to
// serialize FileSpec and immediately parse it back to derive the DB binding.
type normalizedSessionResource struct {
	resource     db.SessionResource
	fileSpec     *sessionresource.FileSpec
	fileMimeType string
}

type sessionResourceWritePlan struct {
	inputs        []db.CreateSessionResourceInput
	eventBindings []sessioncontract.EventFileBinding
}

func validateNormalizedSessionResources(resources []normalizedSessionResource) error {
	specs := lo.FilterMap(resources, func(resource normalizedSessionResource, _ int) (sessionresource.FileSpec, bool) {
		return lo.FromPtr(resource.fileSpec), resource.fileSpec != nil
	})
	return sessionresource.ValidateFileSpecs(specs)
}

func sessionResourceWriteInput(resource normalizedSessionResource) (db.CreateSessionResourceInput, error) {
	input := db.CreateSessionResourceInput{Resource: resource.resource}
	if resource.fileSpec == nil {
		return input, nil
	}
	binding, err := resource.fileSpec.SessionFileBinding(resource.resource.ExternalID)
	if err != nil {
		return db.CreateSessionResourceInput{}, err
	}
	input.FileMount = &db.SessionFileMount{
		ResourceExternalID: binding.ResourceID,
		FileExternalID:     binding.FileID,
		Path:               binding.Path,
	}
	return input, nil
}

func planSessionResourceWrites(
	resources []normalizedSessionResource,
) (sessionResourceWritePlan, error) {
	plan := sessionResourceWritePlan{
		inputs:        make([]db.CreateSessionResourceInput, 0, len(resources)),
		eventBindings: make([]sessioncontract.EventFileBinding, 0, len(resources)),
	}
	for _, resource := range resources {
		input, err := sessionResourceWriteInput(resource)
		if err != nil {
			return sessionResourceWritePlan{}, err
		}
		plan.inputs = append(plan.inputs, input)
		if input.FileMount != nil {
			plan.eventBindings = append(plan.eventBindings, sessioncontract.EventFileBinding{
				FileID:   input.FileMount.FileExternalID,
				Path:     input.FileMount.Path,
				MimeType: resource.fileMimeType,
			})
		}
	}
	return plan, nil
}
