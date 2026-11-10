package db

import (
	"errors"
	"testing"
)

func TestEmptyOrganizationMemberActorRequiresValidator(t *testing.T) {
	denied := errors.New("denied")
	if err := validateOrganizationMemberActor(t.Context(), nil, "org", "", func(role string) error {
		if role != "" {
			t.Fatal("不得伪造管理员角色")
		}
		return denied
	}); !errors.Is(err, denied) {
		t.Fatalf("err=%v", err)
	}
	if err := validateOrganizationMemberActor(t.Context(), nil, "org", "", func(role string) error { return nil }); err != nil {
		t.Fatal(err)
	}
}
