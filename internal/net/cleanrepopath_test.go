package net

import "testing"

// FR-029: validation is per segment — benign ".."-substring names serve,
// real traversal is still rejected (Clean collapses it to a contained path,
// and the segment check is the backstop).
func TestCleanRepoPath(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "/main.git", want: "/main.git"},
		{in: "/data..git", want: "/data..git"},   // benign substring — was wrongly rejected
		{in: "/v1..v2.git", want: "/v1..v2.git"}, // benign substring
		{in: "/a/b/main.git", want: "/a/b/main.git"},
		{in: "main.git", wantErr: true},               // must be absolute
		{in: "/", wantErr: true},                      // root itself is not a repo
		{in: "", wantErr: true},                       // empty
		{in: "/../etc", wantErr: false, want: "/etc"}, // Clean collapses; stays inside root
	}
	for _, tc := range cases {
		got, err := cleanRepoPath(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("cleanRepoPath(%q) = %q, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("cleanRepoPath(%q) error = %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("cleanRepoPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
