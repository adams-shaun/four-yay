package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMirrorVerdictsIncludedInStatsAndSummary(t *testing.T) {
	counts := map[string]int{"equivalent": 3, "mismatch base_option_window|state_differs": 1}
	if got := formatMirrorVerdicts(counts); got != "equivalent=3, mismatch base_option_window|state_differs=1" {
		t.Fatalf("summary counts = %q", got)
	}
	stats := runStats{MirrorVerdicts: optionalMirrorVerdicts(true, counts)}
	path := filepath.Join(t.TempDir(), "stats.json")
	if err := stats.save(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		MirrorVerdicts map[string]int `json:"mirror_verdicts"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.MirrorVerdicts["equivalent"] != 3 || got.MirrorVerdicts["mismatch base_option_window|state_differs"] != 1 {
		t.Fatalf("stats mirror verdicts = %v", got.MirrorVerdicts)
	}
	if strings.Contains(string(data), `"mirror_verdicts":null`) {
		t.Fatalf("stats omitted enabled mirror verdict map: %s", data)
	}
	if disabled := optionalMirrorVerdicts(false, counts); disabled != nil {
		t.Fatalf("disabled mirror stats = %v, want omitted", disabled)
	}
}
