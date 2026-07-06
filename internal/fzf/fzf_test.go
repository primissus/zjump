package fzf

import (
	"testing"

	"zjump/internal/errs"
)

// TestClassifyExit covers the full fzf exit-code table (R-FZF-5), including the
// silent-130 cancellation path and signal termination (code -1).
func TestClassifyExit(t *testing.T) {
	cases := []struct {
		code       int
		wantErr    string // "" means nil
		wantSilent int    // -1 means not a SilentExit
	}{
		{0, "", -1},
		{1, "no match found", -1},
		{2, "fzf returned an error", -1},
		{130, "", 130}, // silent exit 130
		{143, "fzf was terminated", -1},
		{254, "fzf was terminated", -1},
		{-1, "fzf was terminated", -1}, // signaled
		{3, "fzf returned an unknown error", -1},
		{255, "fzf returned an unknown error", -1},
	}
	for _, tc := range cases {
		err := classifyExit(tc.code)
		if tc.wantSilent >= 0 {
			se, ok := errs.AsSilentExit(err)
			if !ok || se.Code != tc.wantSilent {
				t.Errorf("code %d: want SilentExit{%d}, got %v", tc.code, tc.wantSilent, err)
			}
			continue
		}
		if tc.wantErr == "" {
			if err != nil {
				t.Errorf("code %d: want nil, got %v", tc.code, err)
			}
			continue
		}
		if err == nil || err.Error() != tc.wantErr {
			t.Errorf("code %d: want %q, got %v", tc.code, tc.wantErr, err)
		}
	}
}
