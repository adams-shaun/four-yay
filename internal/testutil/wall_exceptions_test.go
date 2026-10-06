package testutil

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// wallExceptionPins is the shrink-only inventory of tests over the 1-minute
// per-test wall budget. Delete an entry when its test fits; adding one raises
// the ratchet. It is empty until the first post-merge batch seeds the list
// (see the !unseeded note in testdata/wall_exceptions.txt).
var wallExceptionPins = map[string]int{}

// wallBudgetS is the per-test wall budget in seconds.
const wallBudgetS = 60

func TestWallExceptionsOnlyShrink(t *testing.T) {
	data, err := os.ReadFile("testdata/wall_exceptions.txt")
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	seen := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") || line == "!unseeded" {
			continue
		}
		rows++
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			t.Fatalf("row %q: want package<TAB>test<TAB>wall_s", line)
		}
		key := f[0] + "\t" + f[1]
		if seen[key] {
			t.Fatalf("duplicate wall exception %q", key)
		}
		seen[key] = true
		wall, err := strconv.Atoi(f[2])
		if err != nil {
			t.Fatalf("row %q: wall_s: %v", line, err)
		}
		if wall <= wallBudgetS {
			t.Errorf("row %q: %ds fits the %ds budget; delete the row", line, wall, wallBudgetS)
		}
		want, ok := wallExceptionPins[key]
		if !ok {
			t.Errorf("unexpected wall exception %q: inventory only shrinks", key)
		} else if wall != want {
			t.Errorf("row %q: measured %ds, want pinned %ds", key, wall, want)
		}
	}
	if rows != len(wallExceptionPins) {
		t.Fatalf("%d exception rows, pinned inventory has %d", rows, len(wallExceptionPins))
	}
}
