package main

import "testing"

func TestParseByteSize(t *testing.T) {
	for in, want := range map[string]int64{"1500MiB": 1500 << 20, "2GiB": 2 << 30, "4096": 4096, "8KiB": 8 << 10, "10B": 10} {
		if got, err := parseByteSize(in); err != nil || got != want {
			t.Errorf("parseByteSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "x", "-1", "1.5GiB"} {
		if _, err := parseByteSize(bad); err == nil {
			t.Errorf("parseByteSize(%q) accepted", bad)
		}
	}
}
