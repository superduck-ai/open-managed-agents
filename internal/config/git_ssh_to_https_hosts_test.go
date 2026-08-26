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

	t.Run("accepts host", func(t *testing.T) {
		t.Parallel()
		if err := validateGitSSHtoHTTPSHosts([]string{" gitlab.xxxx.cn "}); err != nil {
			t.Fatalf("valid host: %v", err)
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

	t.Run("rejects duplicate", func(t *testing.T) {
		t.Parallel()
		err := validateGitSSHtoHTTPSHosts([]string{"gitlab.xxxx.cn", "GITLAB.XXXX.CN"})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
