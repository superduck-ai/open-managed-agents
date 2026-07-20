package networkpolicy

import "testing"

// ---- 失败场景 ----

func TestValidateAllowedHostRejectsInvalidEntries(t *testing.T) {
	for _, host := range []string{
		"https://example.com",
		"example.com/path",
		"",
		".",
		"..",
		"example..com",
		"-example.com",
		"example-.com",
		"example.com:0",
		"example.com:65536",
		"exa mple.com",
		" example.com",
		"example.com ",
		"a-b.c_d.com",
	} {
		if err := ValidateAllowedHost(host); err == nil {
			t.Fatalf("host %q: expected error", host)
		}
	}
	long := make([]byte, 254)
	for i := range long {
		long[i] = 'a'
	}
	if err := ValidateAllowedHost(string(long)); err == nil {
		t.Fatal("expected error for host longer than 253 characters")
	}
}

// ---- 成功场景 ----

func TestValidateAllowedHostAcceptsCanonicalizableEntries(t *testing.T) {
	for _, host := range []string{
		"example.com",
		"*.example.com",
		"例子.测试",
		"*.例子.测试",
		"example.com:8443",
		"2606:4700:4700::1111",
		"[2606:4700:4700::1111]:443",
	} {
		if err := ValidateAllowedHost(host); err != nil {
			t.Fatalf("host %q: unexpected error %v", host, err)
		}
	}
}
