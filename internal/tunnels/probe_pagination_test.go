package tunnels

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestProbePaginationRejectsIncompleteResults(t *testing.T) {
	failure := errors.New("page unavailable")
	for _, mode := range []string{"page failure", "cursor cycle", "page limit", "tool limit", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "canceled" {
				cancel()
			}
			calls := 0
			tools, err := collectProbeTools(ctx, func(context.Context, string, int) (tunnelProbeRPCResponse, error) {
				calls++
				var result tunnelProbeRPCResponse
				result.Result.Tools = []TunnelProbeTool{{Name: "tool"}}
				result.Result.NextCursor = fmt.Sprint(calls)
				switch mode {
				case "page failure":
					if calls == 2 {
						return result, failure
					}
				case "cursor cycle":
					result.Result.NextCursor = "same"
				case "tool limit":
					result.Result.Tools = make([]TunnelProbeTool, maxTunnelProbeTools+1)
				}
				return result, nil
			})
			if err == nil || tools != nil {
				t.Fatalf("incomplete result = %v, %v", tools, err)
			}
			if calls > maxTunnelProbePages {
				t.Fatal("page budget exceeded")
			}
			if mode == "canceled" && calls != 0 {
				t.Fatal("fetched after cancellation")
			}
			if mode == "page failure" && !errors.Is(err, failure) {
				t.Fatal(err)
			}
		})
	}
}

func TestProbePaginationPreservesOpaqueCursorAndEmptyPages(t *testing.T) {
	calls := 0
	tools, err := collectProbeTools(t.Context(), func(_ context.Context, cursor string, page int) (tunnelProbeRPCResponse, error) {
		calls++
		var result tunnelProbeRPCResponse
		if page == 0 {
			result.Result.NextCursor = "opaque +/== "
			return result, nil
		}
		if cursor != "opaque +/== " {
			t.Fatalf("cursor changed: %q", cursor)
		}
		result.Result.Tools = []TunnelProbeTool{{Name: "last-page"}}
		return result, nil
	})
	if err != nil || calls != 2 || len(tools) != 1 || tools[0].Name != "last-page" {
		t.Fatalf("result: %v %v calls=%d", tools, err, calls)
	}
}

func TestProbePaginationAcceptsExactLimitsAndZeroTools(t *testing.T) {
	for _, size := range []int{0, maxTunnelProbeTools} {
		tools, err := collectProbeTools(t.Context(), func(context.Context, string, int) (tunnelProbeRPCResponse, error) {
			var r tunnelProbeRPCResponse
			r.Result.Tools = make([]TunnelProbeTool, size)
			return r, nil
		})
		if err != nil || tools == nil || len(tools) != size {
			t.Fatalf("size=%d tools=%v err=%v", size, tools, err)
		}
	}
}
