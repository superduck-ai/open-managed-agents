package config

import "testing"

func TestValidateGitSSHtoHTTPSHosts(t *testing.T) {
	t.Parallel()

	t.Run("empty ok", func(t *testing.T) {
		t.Parallel()
		if err := validateGitSSHtoHTTPSHosts(nil); err != nil {
			t.Fatalf("nil: %v", err)
		}
		if err := validateGitSSHtoHTTPSHosts([]string{}); err != nil {
			t.Fatalf("empty: %v", err)
		}
	})

	t.Run("normalizes in place", func(t *testing.T) {
		t.Parallel()
		hosts := []string{" GitLab.XXXX.CN "}
		if err := validateGitSSHtoHTTPSHosts(hosts); err != nil {
			t.Fatalf("valid host: %v", err)
		}
		if hosts[0] != "gitlab.xxxx.cn" {
			t.Fatalf("hosts[0] = %q, want gitlab.xxxx.cn", hosts[0])
		}
	})

	t.Run("rejects empty entry", func(t *testing.T) {
		t.Parallel()
		err := validateGitSSHtoHTTPSHosts([]string{""})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("rejects url", func(t *testing.T) {
		t.Parallel()
		err := validateGitSSHtoHTTPSHosts([]string{"https://gitlab.xxxx.cn"})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("rejects shell metacharacters", func(t *testing.T) {
		t.Parallel()
		for _, host := range []string{
			"example.com;id",
			"example.com$(id)",
			"example.com`id`",
			"example.com|id",
			"example.com&id",
		} {
			if err := validateGitSSHtoHTTPSHosts([]string{host}); err == nil {
				t.Fatalf("expected error for %q", host)
			}
		}
	})

	t.Run("rejects hyphen-edged labels", func(t *testing.T) {
		t.Parallel()
		for _, host := range []string{
			"-gitlab.example.com",
			"gitlab-.example.com",
			"gitlab.-example.com",
			"gitlab.example.com-",
		} {
			if err := validateGitSSHtoHTTPSHosts([]string{host}); err == nil {
				t.Fatalf("expected error for %q", host)
			}
		}
	})

	t.Run("rejects duplicate", func(t *testing.T) {
		t.Parallel()
		err := validateGitSSHtoHTTPSHosts([]string{"gitlab.xxxx.cn", "GITLAB.XXXX.CN"})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
