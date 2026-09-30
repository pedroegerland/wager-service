package main

import "testing"

func TestNormalizeAmount(t *testing.T) {
	cases := map[string]string{
		"25":     "25.00",
		"25.5":   "25.50",
		"25.50":  "25.50",
		"0":      "0.00",
		"25,5":   "25.50",
		"25,50":  "25.50",
		"25.555": "25.555",
		"abc":    "abc",
		"-1":     "-1",
	}
	for in, want := range cases {
		if got := normalizeAmount(in); got != want {
			t.Errorf("%q -> %q want %q", in, got, want)
		}
	}
}
