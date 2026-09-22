package environments

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

type cubeSandboxTemplateBuilder struct {
	http   buildHTTP
	apiURL string
	cfg    config.TemplateBuildConfig
}
type cubeSandboxBuildRef struct {
	TemplateID string `json:"template_id"`
	JobID      string `json:"job_id"`
}

func (c *cubeSandboxTemplateBuilder) Start(ctx context.Context, imageRef string) (buildJobRef, error) {
	payload := struct {
		Image               string   `json:"image"`
		RegistryUsername    string   `json:"registryUsername,omitempty"`
		RegistryPassword    string   `json:"registryPassword,omitempty"`
		WritableLayerSize   string   `json:"writableLayerSize"`
		CPU                 uint32   `json:"cpu,omitempty"`
		Memory              uint32   `json:"memory,omitempty"`
		DNS                 []string `json:"dns,omitempty"`
		AllowOut            []string `json:"allowOut,omitempty"`
		DenyOut             []string `json:"denyOut,omitempty"`
		AllowInternetAccess bool     `json:"allowInternetAccess"`
		WithCubeCA          bool     `json:"with_cube_ca"`
		ExposedPorts        []int    `json:"exposedPorts"`
		ProbePort           int      `json:"probePort"`
		ProbePath           string   `json:"probePath"`
	}{
		Image: imageRef, RegistryUsername: c.cfg.RegistryAuth.Username, RegistryPassword: c.cfg.RegistryAuth.Password,
		WritableLayerSize: c.cfg.DiskSize, CPU: c.cfg.CPU, Memory: c.cfg.Memory,
		DNS: c.cfg.Network.DNSServers, AllowOut: c.cfg.Network.AllowOutboundCIDRs, DenyOut: c.cfg.Network.DenyOutboundCIDRs,
		AllowInternetAccess: c.cfg.Network.AllowInternet, WithCubeCA: c.cfg.Network.InjectEgressCA,
		ExposedPorts: []int{49983}, ProbePort: 49983, ProbePath: "/health",
	}
	var result struct {
		JobID      string `json:"jobID"`
		TemplateID string `json:"templateID"`
	}
	if err := c.http.call(ctx, http.MethodPost, c.apiURL+"/templates", payload, &result); err != nil {
		return "", err
	}
	if result.JobID == "" || result.TemplateID == "" {
		return "", fmt.Errorf("CubeSandbox returned an empty build or template ID")
	}
	return encodeJobRef(cubeSandboxBuildRef{result.TemplateID, result.JobID}), nil
}

func (c *cubeSandboxTemplateBuilder) GetStatus(ctx context.Context, ref buildJobRef) (templateBuildStatus, error) {
	var job cubeSandboxBuildRef
	if err := json.Unmarshal([]byte(ref), &job); err != nil {
		return templateBuildStatus{}, err
	}
	endpoint := c.apiURL + "/templates/" + url.PathEscape(job.TemplateID) + "/builds/" + url.PathEscape(job.JobID) + "/status"
	var result struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if err := c.http.call(ctx, http.MethodGet, endpoint, nil, &result); err != nil {
		return templateBuildStatus{}, err
	}
	status := templateBuildStatus{buildJobStatus: buildJobStatus{State: "running", Message: result.Message}}
	switch result.Status {
	case "ready":
		status.State = "succeeded"
		status.TemplateID = job.TemplateID
	case "error":
		status.State = "failed"
	}
	return status, nil
}

func (*cubeSandboxTemplateBuilder) ReadLogs(context.Context, buildJobRef, string) (buildLogChunk, error) {
	return buildLogChunk{}, errPrebuildUnsupported
}
func (*cubeSandboxTemplateBuilder) Cancel(context.Context, buildJobRef) error {
	return errPrebuildUnsupported
}

func (*cubeSandboxTemplateBuilder) Capabilities() buildCapabilities { return buildCapabilities{} }
