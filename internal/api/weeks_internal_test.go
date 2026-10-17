package api

import "testing"

// TestDecimalsFollowTheShortestText: the decimals of a reading are those of its shortest decimal text, so float
// noise does not count. A reading with more than maxDecimals has maxDecimals.
func TestDecimalsFollowTheShortestText(t *testing.T) {
	tenth, fifth := 0.1, 0.2
	noisy := tenth + fifth // 0.30000000000000004: a constant sum would be exact
	for _, tc := range []struct {
		x    float64
		want int
	}{
		{58, 0}, {-58, 0}, {0, 0}, {71.9, 1}, {-2.5, 1}, {60.29, 2}, {0.125, 3}, {5432.123456, 6},
		{5432.1234567, maxDecimals}, {noisy, maxDecimals}, {1e-300, maxDecimals}, {1.7976931348623157e308, 0},
	} {
		if got := decimals(tc.x); got != tc.want {
			t.Errorf("decimals(%v) = %d, want %d", tc.x, got, tc.want)
		}
	}
}

// TestAMeanShowsOneDecimalAtLeast: the readings of both weeks count, and a whole-number metric still gets one place.
func TestAMeanShowsOneDecimalAtLeast(t *testing.T) {
	for _, tc := range []struct {
		weeks [][]float64
		want  int
	}{
		{[][]float64{{58, 60}, nil}, 1},
		{[][]float64{{58, 60}, {60.29}}, 2},
		{[][]float64{{78.5}, {58}}, 1},
		{[][]float64{{1.123456789}}, maxDecimals},
	} {
		if got := meanDecimals(tc.weeks...); got != tc.want {
			t.Errorf("meanDecimals(%v) = %d, want %d", tc.weeks, got, tc.want)
		}
	}
}
