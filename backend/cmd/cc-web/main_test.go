package main

import "testing"

func TestBindPolicy(t *testing.T) {
	for _, tc := range []struct {
		addr, allow, db string
		want            bool
	}{
		{"127.0.0.1:8080", "", "", true},
		{"[::1]:8080", "", "", true},
		{"0.0.0.0:8080", "", "", false},
		{"0.0.0.0:8080", "true", "", false},
		{"0.0.0.0:8080", "true", "/data/cc-web.db", true},
	} {
		if got := bindAllowed(tc.addr, tc.allow, tc.db); got != tc.want {
			t.Errorf("bindAllowed(%q,%q,%q)=%v, want %v", tc.addr, tc.allow, tc.db, got, tc.want)
		}
	}
}
