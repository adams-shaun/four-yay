package rules

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/effects"
)

func TestPlotZoneCarrierCensus(t *testing.T) {
	t.Parallel()
	if !effects.Supported()["stat:PlotZone"] {
		t.Fatal("effects.Supported() lacks stat:PlotZone")
	}
	root := filepath.Join("..", ".cards", "cardsfolder")
	files := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[strings.TrimPrefix(path, root+string(filepath.Separator))] = string(b)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join("testdata", "plot-zone-carriers", "fblthp_lost_on_the_range.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line != "" {
			want = append(want, line)
		}
	}
	var got []string
	for name, content := range files {
		if hasStaticModeLine(content, "PlotZone") {
			got = append(got, name)
		}
	}
	sort.Strings(got)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("Mode$ PlotZone corpus carriers changed: got %d, pinned %d\n got: %v\nwant: %v",
			len(got), len(want), got, want)
	}
}

func hasStaticModeLine(src, mode string) bool {
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "S:Mode$") {
			tail := strings.TrimSpace(strings.TrimPrefix(line, "S:Mode$"))
			if tail == mode || strings.HasPrefix(tail, mode+" ") || strings.HasPrefix(tail, mode+"|") {
				return true
			}
		}
	}
	return false
}
