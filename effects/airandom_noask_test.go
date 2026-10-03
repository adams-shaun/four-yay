package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// noAskRngHost is the no-ask fakeHost (every Ask degrades to the R-9
// stand-in) whose Rand replays a scripted sequence of raw draws; an
// exhausted script degrades to 0.
type noAskRngHost struct {
	fakeHost
	seq   []int
	i     int
	draws int
}

func (h *noAskRngHost) Rand(n int) int {
	h.draws++
	if h.i < len(h.seq) {
		v := h.seq[h.i]
		h.i++
		return v % n
	}
	return 0
}

// TestFaceToFaceNoAskHostRethrowsATie resolves the REAL Face to Face on a
// host that cannot ask. Its throws carry AILogic$ Random, so the R-9 answer
// is an engine-rng draw, never the fixed first option (Rock against Rock
// for every pass of the Repeat, up to its 1000-iteration cap). The script
// ties twice (Rock/Rock, Paper/Paper), then the caster wins two rounds
// (Rock beats Scissors): the match must end after six rounds with the 5
// damage dealt, and every throw must have drawn from the rng.
func TestFaceToFaceNoAskHostRethrowsATie(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Face to Face")
	if !ok {
		t.Fatal("corpus has no Face to Face")
	}
	face := card.Faces[0]
	if play := cards.ResolveSVar(face.SVars, "Play"); play == nil || !aiLogicRandom(play) {
		t.Fatalf("Face to Face's throw lacks AILogic$ Random: %+v", play)
	}
	fh, _ := fixtureHost(t)
	h := &noAskRngHost{fakeHost: *fh,
		// Caster index (YouRock=0, YouPape=1), then the opponent's
		// (OppRock=0, OppPape=1, OppScis=2): tie, tie, win, win.
		seq: []int{0, 0, 1, 1, 0, 2, 0, 2}}
	src := h.g.AddObject(card, 0)
	events.Move(h.g, src.ID, state.ZLibrary, state.ZStack)
	before := h.g.Players[1].Life
	c := &Ctx{Source: src.ID, Controller: 0, SVars: face.SVars,
		Targets: []state.Target{{Player: 1, IsPlayer: true}}, TargetsOffered: true}
	Resolve(h, c, face.Abilities[0])
	if got := h.g.Players[1].Life; got != before-5 {
		t.Fatalf("opponent life = %d, want %d (two wins after two ties)", got, before-5)
	}
	if h.draws != 8 {
		t.Fatalf("rng draws = %d, want 8 (two throws per round, four rounds)", h.draws)
	}
}

// TestAIRandomNoAskPickOnlyDrawsForRandomAsks pins the helper's contract:
// a non-random or one-option ask keeps the first option and consumes no
// rng, so every other R-9 answer (and its replay) is unchanged.
func TestAIRandomNoAskPickOnlyDrawsForRandomAsks(t *testing.T) {
	fh, _ := fixtureHost(t)
	h := &noAskRngHost{fakeHost: *fh, seq: []int{2}}
	plain := &cards.SA{Kind: "DB", API: "GenericChoice", Params: map[string]string{"Choices": "A,B,C"}}
	if got := aiRandomNoAskPick(h, plain, 3); got != 0 || h.draws != 0 {
		t.Fatalf("non-random ask: pick %d draws %d, want 0 and 0", got, h.draws)
	}
	random := &cards.SA{Kind: "DB", API: "GenericChoice", Params: map[string]string{"Choices": "A,B,C", "AILogic": "Random"}}
	if got := aiRandomNoAskPick(h, random, 1); got != 0 || h.draws != 0 {
		t.Fatalf("one-option random ask: pick %d draws %d, want 0 and 0", got, h.draws)
	}
	if got := aiRandomNoAskPick(h, random, 3); got != 2 || h.draws != 1 {
		t.Fatalf("random ask: pick %d draws %d, want 2 and 1", got, h.draws)
	}
}
