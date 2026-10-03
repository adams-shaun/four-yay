package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// The deterministic acceptance game's finished chain head, one file per seat
// count: rules/testdata/heads/<seats>.txt holds the 16-hex-digit head and a
// newline, nothing else (R-14). A change to one is a change to what the 12
// repo decks do; the commit that makes it names the first diverging event
// and the behaviour that moved it IN THE COMMIT MESSAGE -- the files carry no
// prose, so two branches re-pinning heads conflict only when they really pin
// different values. The prose these goldens used to carry is frozen in
// docs/agents/heads-history.md. The game is rules/acceptance_game.go's,
// shared with TestRepoDecksPlayAtEverySeatCount and cmd/headdiff.
const (
	headsDir       = "testdata/heads"
	updateHeadsEnv = "GORGE_UPDATE_HEADS"
)

// headPath is seats' golden file, relative to the rules package directory.
func headPath(seats int) string {
	return filepath.Join(headsDir, fmt.Sprintf("%d.txt", seats))
}

// pinnedHead reads seats' golden head.
func pinnedHead(t *testing.T, seats int) string {
	t.Helper()
	raw, err := os.ReadFile(headPath(seats))
	if err != nil {
		t.Fatalf("%d seats: reading the pinned head: %v (re-pin with %s)", seats, err, rePinHint)
	}
	return strings.TrimSpace(string(raw))
}

// rePinHint is how to re-pin, quoted by every failure here.
const rePinHint = updateHeadsEnv + "=1 go test ./rules/ -run '^TestHeads$'"

func TestHeads(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("long")
	}
	reg := testutil.CorpusRegistry(t)
	update := os.Getenv(updateHeadsEnv) == "1"
	for _, seats := range AcceptanceSeatCounts() {
		got := acceptanceHead(t, reg, seats)
		if update {
			if err := os.WriteFile(headPath(seats), []byte(got+"\n"), 0o644); err != nil {
				t.Fatalf("%d seats: writing %s: %v", seats, headPath(seats), err)
			}
			t.Logf("%d seats: pinned %s in rules/%s", seats, got, headPath(seats))
			continue
		}
		// The "<n> seats: chain head <got>, golden <want>" prefix is parsed by
		// orchestrator/hooks.py (_HEAD_RE); keep it.
		if want := pinnedHead(t, seats); got != want {
			t.Errorf("%d seats: chain head %s, golden %s -- if this move is intended, re-pin with `%s` "+
				"and name the first diverging event and its cause in the COMMIT MESSAGE (not in a file). "+
				"Find that event with cmd/headdiff: dump main's streams with `headdiff -dump` and run "+
				"`headdiff -against` on this tree (see `go doc ./cmd/headdiff`)",
				seats, got, want, rePinHint)
		}
	}
}
