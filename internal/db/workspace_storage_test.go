package db

import (
	"errors"
	"math"
	"testing"
)

func TestAddWorkspaceStorageDelta(t *testing.T) {
	t.Run("rejects underflow and overflow", func(t *testing.T) {
		for _, testCase := range []struct {
			name    string
			current int64
			delta   int64
			wantErr error
		}{
			{name: "underflow", current: 2, delta: -3},
			{name: "minimum delta", current: math.MaxInt64, delta: math.MinInt64},
			{name: "overflow", current: math.MaxInt64, delta: 1, wantErr: ErrStorageLimitExceeded},
		} {
			t.Run(testCase.name, func(t *testing.T) {
				_, err := addWorkspaceStorageDelta(testCase.current, testCase.delta)
				if err == nil {
					t.Fatal("addWorkspaceStorageDelta() error = nil")
				}
				if testCase.wantErr != nil && !errors.Is(err, testCase.wantErr) {
					t.Fatalf("addWorkspaceStorageDelta() error = %v, want %v", err, testCase.wantErr)
				}
			})
		}
	})

	t.Run("applies positive zero and negative deltas", func(t *testing.T) {
		for _, testCase := range []struct {
			current int64
			delta   int64
			want    int64
		}{
			{current: 2, delta: 3, want: 5},
			{current: 2, delta: 0, want: 2},
			{current: 5, delta: -3, want: 2},
		} {
			got, err := addWorkspaceStorageDelta(testCase.current, testCase.delta)
			if err != nil {
				t.Fatalf("addWorkspaceStorageDelta(%d, %d): %v", testCase.current, testCase.delta, err)
			}
			if got != testCase.want {
				t.Fatalf("addWorkspaceStorageDelta(%d, %d) = %d, want %d", testCase.current, testCase.delta, got, testCase.want)
			}
		}
	})
}
