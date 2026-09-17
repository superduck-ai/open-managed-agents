package tests

import (
	"context"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func TestGoSDKTunnelCertificateLifecycle(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("tunnel-certificates-sdk-bucket"))
	defer app.close()

	client := anthropic.NewClient(
		option.WithBaseURL(app.baseURL),
		option.WithAPIKey(defaultTestKey),
	)
	ctx := context.Background()
	tunnel, err := client.Beta.Tunnels.New(ctx, anthropic.BetaTunnelNewParams{
		DisplayName: anthropic.String("SDK certificate lifecycle"),
	})
	if err != nil {
		t.Fatalf("SDK create tunnel: %v", err)
	}

	created, err := client.Beta.Tunnels.Certificates.New(ctx, tunnel.ID, anthropic.BetaTunnelCertificateNewParams{
		CACertificatePEM: newAdminTestTunnelCAPEM(t),
	})
	if err != nil {
		t.Fatalf("SDK create tunnel certificate: %v", err)
	}
	if !tunnelCertificateIDPattern.MatchString(created.ID) ||
		created.TunnelID != tunnel.ID ||
		!tunnelFingerprintPattern.MatchString(created.Fingerprint) ||
		created.ExpiresAt.IsZero() ||
		created.JSON.ArchivedAt.Raw() != "null" {
		t.Fatalf("SDK created tunnel certificate = %+v, raw=%s", created, created.RawJSON())
	}

	retrieved, err := client.Beta.Tunnels.Certificates.Get(ctx, created.ID, anthropic.BetaTunnelCertificateGetParams{
		TunnelID: tunnel.ID,
	})
	if err != nil {
		t.Fatalf("SDK retrieve tunnel certificate: %v", err)
	}
	if retrieved.ID != created.ID || retrieved.Fingerprint != created.Fingerprint {
		t.Fatalf("SDK retrieved tunnel certificate = %+v, want %+v", retrieved, created)
	}

	page, err := client.Beta.Tunnels.Certificates.List(ctx, tunnel.ID, anthropic.BetaTunnelCertificateListParams{
		Limit: anthropic.Int(1),
	})
	if err != nil {
		t.Fatalf("SDK list tunnel certificates: %v", err)
	}
	if len(page.Data) != 1 || page.Data[0].ID != created.ID {
		t.Fatalf("SDK certificate page = %+v", page.Data)
	}
	if _, err = client.Beta.Tunnels.Archive(ctx, tunnel.ID, anthropic.BetaTunnelArchiveParams{}); err != nil {
		t.Fatalf("SDK archive tunnel: %v", err)
	}
	retrieved, err = client.Beta.Tunnels.Certificates.Get(ctx, created.ID, anthropic.BetaTunnelCertificateGetParams{
		TunnelID: tunnel.ID,
	})
	if err != nil {
		t.Fatalf("SDK retrieve independent certificate after tunnel archive: %v", err)
	}
	if !retrieved.ArchivedAt.IsZero() || retrieved.JSON.ArchivedAt.Raw() != "null" {
		t.Fatalf("tunnel archive changed independent certificate = %+v", retrieved)
	}

	archived, err := client.Beta.Tunnels.Certificates.Archive(ctx, created.ID, anthropic.BetaTunnelCertificateArchiveParams{
		TunnelID: tunnel.ID,
	})
	if err != nil {
		t.Fatalf("SDK archive tunnel certificate: %v", err)
	}
	if archived.ID != created.ID || archived.ArchivedAt.IsZero() {
		t.Fatalf("SDK archived tunnel certificate = %+v", archived)
	}

	page, err = client.Beta.Tunnels.Certificates.List(ctx, tunnel.ID, anthropic.BetaTunnelCertificateListParams{
		IncludeArchived: anthropic.Bool(true),
	})
	if err != nil {
		t.Fatalf("SDK list archived tunnel certificates: %v", err)
	}
	if len(page.Data) != 1 || page.Data[0].ID != created.ID || page.Data[0].ArchivedAt.IsZero() {
		t.Fatalf("SDK archived certificate page = %+v", page.Data)
	}
}
