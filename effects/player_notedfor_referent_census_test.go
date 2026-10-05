package effects

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The control/ownership `Player.NotedFor<label>` referent census: every corpus
// file whose text carries an `OwnedBy` or `ControlledBy` token immediately
// followed by a `Player.NotedFor<label>` argument. That predicate is resolved
// by the shared control/ownership grammar (effects/trigger_referents.go:
// controlReferent -> controlReferentPlayers -> MatchesPlayerSpecCtx), so a NEW
// carrier fails this census loudly and its author must confirm the shared arm
// covers it -- the same shape as the AnimateAll player-kind ValidTgts$ census
// (effects/animateall_player_tgts_ratchet_test.go).
//
// The five files are the WHOLE measured population at the ticket's base
// (2026-10-05):
//
//	archangel_of_strife.txt   S:Mode$ Continuous | Affected$ Creature.ControlledBy Player.NotedForWar ...
//	decoy_gambit.txt          ChangeType$ Card.targetedBy+ControlledBy Player.NotedForDiversion ...
//	step_between_worlds.txt   ChangeType$ Card.OwnedBy Player.NotedForStargate ...
//	turtles_in_time.txt       ChangeType$ Card.OwnedBy Player.NotedForStargate ...
//	two_streams_facility.txt  S:Mode$ Continuous | Affected$ Creature.ControlledBy Player.NotedForRedWaterfall ...
//
// Two consumers use the predicate: a ChangeZoneAll sweep (3 files) and a
// Continuous static's Affected$ (2 files), both through the SAME
// MatchesSpecCtx path -- which is why the fix lives in the shared referent
// grammar and not in a ChangeZoneAll-only read (a ChangeZoneAll-only patch
// would leave the two statics broken).
//
// The corpus is gitignored (GPL-3.0): the test Skips when it is absent so a
// clean clone still passes.
func TestPlayerNotedForControlReferentCensus(t *testing.T) {
	dir := "../.cards/cardsfolder"
	if _, err := os.Stat(dir); err != nil {
		t.Skip("corpus not fetched; run `make fetch-cards`")
	}
	got := map[string]bool{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".txt") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if corpusHasNotedForControlReferent(string(b)) {
			got[filepath.Base(path)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk corpus: %v", err)
	}
	want := []string{
		"archangel_of_strife.txt",
		"decoy_gambit.txt",
		"step_between_worlds.txt",
		"turtles_in_time.txt",
		"two_streams_facility.txt",
	}
	var names []string
	for n := range got {
		names = append(names, n)
	}
	sort.Strings(names)
	sort.Strings(want)
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("Player.NotedFor control/ownership referent carriers = %v, want %v\n"+
			"a new carrier must be confirmed against controlReferent/controlReferentPlayers and this list updated", names, want)
	}
}

// notedForControlReferentOps are the two operators the shared grammar
// classifies (controlReferent's op cut).
var notedForControlReferentOps = []string{"OwnedBy", "ControlledBy"}

// corpusHasNotedForControlReferent reports whether any line of the corpus body
// carries an `OwnedBy`/`ControlledBy` token followed by a `Player.NotedFor
// <label>` argument with a non-empty label. It scans every occurrence so a
// line with both operators is caught, requires the argument immediately after
// the operator (whitespace-separated, not running past a parameter pipe), and
// shares controlReferentNotedForLabel with the matcher so the census cannot
// recognise a different shape than the grammar does.
func corpusHasNotedForControlReferent(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		if lineHasNotedForControlReferent(line) {
			return true
		}
	}
	return false
}

func lineHasNotedForControlReferent(line string) bool {
	for _, op := range notedForControlReferentOps {
		rest := line
		for {
			i := strings.Index(rest, op)
			if i < 0 {
				break
			}
			// The operator must be a whole token: not preceded by an
			// identifier character (so `xOwnedBy` does not count).
			if i > 0 && isIdentByte(rest[i-1]) {
				rest = rest[i+len(op):]
				continue
			}
			after := strings.TrimLeft(rest[i+len(op):], " \t")
			arg, _, _ := strings.Cut(after, "|")
			arg = strings.TrimSpace(arg)
			if controlReferentNotedForLabel(arg) != "" {
				return true
			}
			rest = rest[i+len(op):]
		}
	}
	return false
}

// isIdentByte reports whether b can appear inside a Forge identifier/word,
// so a substring operator match is rejected.
func isIdentByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
