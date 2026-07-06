package db

import (
	"math"
	"testing"
)

// TestScoreBuckets verifies the four decay buckets against fixed durations
// (A-3, R-MATCH-4): ×4 within the hour, ×2 within the day, ×0.5 within the
// week, ×0.25 beyond, using saturating subtraction for clock skew.
func TestScoreBuckets(t *testing.T) {
	const rank = 3.0
	cases := []struct {
		name string
		now  Epoch
		last Epoch
		want Rank
	}{
		{"same instant (<1h)", 1000, 1000, rank * 4.0},
		{"just under 1h", HOUR + 1000 - 1, 1000, rank * 4.0},
		{"exactly 1h -> day bucket", HOUR + 1000, 1000, rank * 2.0},
		{"just under 1d", DAY + 1000 - 1, 1000, rank * 2.0},
		{"exactly 1d -> week bucket", DAY + 1000, 1000, rank * 0.5},
		{"just under 1w", WEEK + 1000 - 1, 1000, rank * 0.5},
		{"exactly 1w -> oldest bucket", WEEK + 1000, 1000, rank * 0.25},
		{"very old", 10 * WEEK, 1000, rank * 0.25},
		{"future last_accessed saturates to <1h", 1000, 5000, rank * 4.0},
	}
	for _, tc := range cases {
		d := Dir{Path: "/x", Rank: rank, LastAccessed: tc.last}
		got := d.Score(tc.now)
		if math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%s: Score(now=%d,last=%d) = %v, want %v", tc.name, tc.now, tc.last, got, tc.want)
		}
	}
}

// TestDisplayScoreFormat checks the fixed 6-char, 1-decimal, clamped field.
func TestDisplayScoreFormat(t *testing.T) {
	cases := []struct {
		rank Rank
		last Epoch
		now  Epoch
		want string
	}{
		{1.0, 1000, 1000, "   4.0\t/x"},      // 1.0 * 4 = 4.0, tab separator
		{0.0, 1000, 1000, "   0.0\t/x"},      // clamp low
		{100000.0, 1000, 1000, "9999.0\t/x"}, // clamp high
	}
	for _, tc := range cases {
		d := Dir{Path: "/x", Rank: tc.rank, LastAccessed: tc.last}
		got := d.DisplayScore(tc.now, "\t")
		if got != tc.want {
			t.Errorf("DisplayScore(rank=%v) = %q, want %q", tc.rank, got, tc.want)
		}
	}
}
