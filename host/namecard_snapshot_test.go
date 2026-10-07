package host

import (
	"testing"
	"time"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/state"
)

// TestNameUniverseSnapshotReplaysAcrossCorpusChange proves that the sidecar
// pins the ordered labels behind a NameCard decision. The extra nonland sorts
// into the list and would shift the bot's recorded numeric choice if replay
// rebuilt options from the updated Options.NameUniverse.
func TestNameUniverseSnapshotReplaysAcrossCorpusChange(t *testing.T) {
	t.Parallel()
	takeMatchSlot(t)
	reg := testutil.CorpusRegistry(t)
	r, err := New(Options{
		LoadDeck:     nameLandLoader(t),
		NameUniverse: reg.Universe(),
		Sleep:        func(time.Duration, <-chan struct{}) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.AddTable(nameUniverseTable("t1")); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	r.Wait("t1")

	r.mu.RLock()
	tb := r.tables["t1"]
	r.mu.RUnlock()
	tb.mu.RLock()
	m := tb.history[0]
	tb.mu.RUnlock()
	if m == nil || m.state != protocol.MatchFinished {
		t.Fatalf("precondition: name-universe match did not finish: %+v", m)
	}
	nameChoices := 0
	for _, ev := range m.e.L.Events {
		if ev.Counter == "name" {
			nameChoices++
		}
	}
	if nameChoices == 0 {
		t.Fatal("precondition: live match never resolved a NameCard choice")
	}
	sc := m.sidecar()
	if len(sc.NameUniverseNames) < 1000 {
		t.Fatalf("precondition: persisted name universe has only %d names", len(sc.NameUniverseNames))
	}

	// "!" sorts before every corpus name, forcing a changed option index
	// rather than merely appending an unused label.
	added := parseNameLand(t, "Name:! Replay Sentinel\nTypes:Creature\nOracle:x\n")
	changed := append(append([]*cards.Card(nil), reg.AllCards()...), added)
	probe := state.NewGame([]string{"a", "b"})
	probe.NameUniverse = cards.UniverseOf(changed)
	changedNames := effects.NameChoices(probe, "Card.nonLand", "")
	if len(changedNames) == 0 || changedNames[0] != added.Faces[0].Name {
		t.Fatalf("precondition: corpus addition did not become the first nonland name: %q", changedNames)
	}
	r.opts.NameUniverse = cards.UniverseOf(changed)

	replayed, err := r.matchForLog(tb, sc, m.e.L)
	if err != nil {
		t.Fatalf("snapshot-backed replay changed under a corpus addition: %v", err)
	}
	if got, want := replayed.e.L.Head(), m.e.L.Head(); got != want {
		t.Fatalf("snapshot replay head %s, live head %s", got, want)
	}
	if len(replayed.cfg.NameUniverseNames) != len(sc.NameUniverseNames) {
		t.Fatalf("replay name snapshot length = %d, sidecar = %d", len(replayed.cfg.NameUniverseNames), len(sc.NameUniverseNames))
	}

	// This is the same post-feature sidecar except for the new immutable
	// list. It models the prior implementation and must diverge now that the
	// current corpus has an earlier legal name.
	unpinned := sc
	unpinned.NameUniverseNames = nil
	if unpinnedReplay, err := r.matchForLog(tb, unpinned, m.e.L); err == nil {
		t.Fatalf("replay without the persisted name list survived a corpus addition (first replay name %q, added %q)", unpinnedReplay.cfg.NameUniverseNames[0], added.Faces[0].Name)
	}
}
