package tests

import "testing"
import (
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func TestFoo(t *testing.T) {
	client := anthropic.NewClient(
		option.WithBaseURL(""),
		option.WithAPIKey(defaultTestKey),
	)
	_ = client

}
