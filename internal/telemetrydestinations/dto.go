package telemetrydestinations

import (
	"encoding/json"
	"time"
)

type stateObject struct {
	Type   string  `json:"type"`
	Reason *string `json:"reason,omitempty"`
}

type tracesObject struct {
	Enabled         bool   `json:"enabled"`
	ContentMode     string `json:"content_mode"`
	EndpointURL     string `json:"endpoint_url"`
	MaxContentBytes int    `json:"max_content_bytes"`
}

type workspacesObject struct {
	Type         string   `json:"type"`
	WorkspaceIDs []string `json:"workspace_ids"`
}

type deliveryHealth struct {
	LastError     *string    `json:"last_error"`
	LastErrorAt   *time.Time `json:"last_error_at"`
	LastSuccessAt *time.Time `json:"last_success_at"`
}

type destinationResponse struct {
	ID             string           `json:"id"`
	Type           string           `json:"type"`
	DisplayName    *string          `json:"display_name"`
	EndpointURL    string           `json:"endpoint_url"`
	HeaderNames    []string         `json:"header_names"`
	State          stateObject      `json:"state"`
	Traces         tracesObject     `json:"traces"`
	Workspaces     workspacesObject `json:"workspaces"`
	DeliveryHealth deliveryHealth   `json:"delivery_health"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
}

type listResponse struct {
	Data     []destinationResponse `json:"data"`
	NextPage *string               `json:"next_page"`
}

type deletedResponse struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type TestResult struct {
	Success    bool    `json:"success"`
	StatusCode *int    `json:"status_code"`
	Error      *string `json:"error"`
}

type testResponse struct {
	Type string `json:"type"`
	TestResult
}

type tracesRequest struct {
	Enabled         *bool           `json:"enabled"`
	ContentMode     *string         `json:"content_mode"`
	EndpointURL     json.RawMessage `json:"endpoint_url"`
	MaxContentBytes *int            `json:"max_content_bytes"`
}

type workspacesRequest struct {
	Type         string   `json:"type"`
	WorkspaceIDs []string `json:"workspace_ids"`
}

type stateRequest struct {
	Type string `json:"type"`
}

type destinationRequest struct {
	DisplayName json.RawMessage    `json:"display_name"`
	EndpointURL *string            `json:"endpoint_url"`
	Headers     json.RawMessage    `json:"headers"`
	Traces      *tracesRequest     `json:"traces"`
	Workspaces  *workspacesRequest `json:"workspaces"`
	State       *stateRequest      `json:"state"`
}
