package sessions

import (
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessioneventfiles"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

// normalizedSessionResource keeps the typed File contract beside the
// persistence row produced from it. The API boundary therefore never has to
// serialize FileSpec and immediately parse it back to derive the DB binding.
type normalizedSessionResource struct {
	resource     db.SessionResource
	fileSpec     *sessionresource.FileSpec
	fileMimeType string
}

func eventFileBindingsFromResources(resources []normalizedSessionResource) ([]sessioneventfiles.Binding, error) {
	bindings := make([]sessioneventfiles.Binding, 0, len(resources))
	for _, resource := range resources {
		if resource.fileSpec == nil {
			continue
		}
		binding, err := resource.fileSpec.SessionFileBinding(resource.resource.ExternalID)
		if err != nil {
			return nil, err
		}
		bindings = append(bindings, sessioneventfiles.Binding{
			FileID:   binding.FileID,
			Path:     binding.Path,
			MimeType: resource.fileMimeType,
		})
	}
	return bindings, nil
}

func eventFileBindingsFromDB(bindings []db.SessionEventFileBinding) []sessioneventfiles.Binding {
	result := make([]sessioneventfiles.Binding, len(bindings))
	for index, binding := range bindings {
		result[index] = sessioneventfiles.Binding{
			FileID:   binding.FileExternalID,
			Path:     binding.Path,
			MimeType: binding.MimeType,
		}
	}
	return result
}

func validateNormalizedSessionResources(resources []normalizedSessionResource) error {
	specs := make([]sessionresource.FileSpec, 0, len(resources))
	for _, resource := range resources {
		if resource.fileSpec != nil {
			specs = append(specs, *resource.fileSpec)
		}
	}
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

func sessionResourceWriteInputs(
	resources []normalizedSessionResource,
) ([]db.CreateSessionResourceInput, error) {
	inputs := make([]db.CreateSessionResourceInput, 0, len(resources))
	for _, resource := range resources {
		input, err := sessionResourceWriteInput(resource)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, input)
	}
	return inputs, nil
}
