package admin

import "testing"

func TestEnvNonNegInt(t *testing.T) {
	for _, tc := range []struct {
		val  string
		want int
	}{
		{"", 15},
		{"0", 0},
		{"20", 20},
		{"-1", 15},
		{"abc", 15},
	} {
		t.Setenv("ZV_TEST_NONNEG", tc.val)
		if got := envNonNegInt("ZV_TEST_NONNEG", 15); got != tc.want {
			t.Errorf("envNonNegInt(%q) = %d, want %d", tc.val, got, tc.want)
		}
	}
}
