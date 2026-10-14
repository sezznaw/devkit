package cmd

import "testing"

func TestDevkitBehind(t *testing.T) {
	cases := []struct {
		have, team string
		want       bool
	}{
		{"0.21.15", "v0.21.16", true},
		{"v0.21.16", "v0.21.16", false},
		{"0.22.0", "v0.21.16", false},
		{"0.9.12", "v0.21.16", true},
		{"dev", "v0.21.16", false},
		{"", "v0.21.16", false},
		{"0.21.15", "", false},
	}
	for _, c := range cases {
		if got := devkitBehind(c.have, c.team); got != c.want {
			t.Errorf("devkitBehind(%q, %q) = %v, want %v", c.have, c.team, got, c.want)
		}
	}
}
