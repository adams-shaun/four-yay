package main

import "testing"

func TestParseBytes(t *testing.T) {
	for in, want := range map[string]int64{"1500MiB": 1500 << 20, "2GiB": 2 << 30, "64KiB": 64 << 10, "1GB": 1e9, "4096": 4096, "10B": 10} {
		if got, err := parseBytes(in); err != nil || got != want {
			t.Fatalf("parseBytes(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "MiB", "-1GiB", "x"} {
		if _, err := parseBytes(bad); err == nil {
			t.Fatalf("parseBytes(%q) accepted", bad)
		}
	}
}
