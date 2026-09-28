package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The TriggersWhenSpent$ rider (mtsp1): a mana ability's "when that mana is
// spent to cast a <spec> spell, <effect>". Path of Ancestry is the corpus
// pin: its Combo ColorIdentity mana is unattributable today, and its
// SVar:TrigScry trigger body is never scanned (it lives in the face's SVar
// table, not its printed T: lines), so the scry never happens.
//
// The fixture is a real Commander game with a Goblin commander and a Goblin
// creature: Path's mana (a provenance-only restriction batch, empty Valid)
// pays for the cast, and the batch's Source is what the rider fires on.

// moveSeededToHand moves a named card seeded into seat p's deck from the
// library to its hand through a LOGGED MoveZone (moveToBattlefieldByName's
// hand-side twin, replay-reconstructable). A card already in hand is
// returned as-is.
func moveSeededToHand(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, p) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				if z == state.ZHand {
					return id
				}
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZHand})
				e.pending = nil
				return id
			}
		}
	}
	t.Fatalf("card %q not in seat %d's library or hand", name, p)
	return 0
}

// castSeeded re-asks priority and submits the "cast" option for id. It leaves
// the spell wherever the cast flow brings it (target ask, payment, stack).
func castSeeded(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	e.priorityRound()
	d := e.Pending()
	if d == nil {
		t.Fatalf("no decision pending before casting %d", id)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %d: %+v", id, d.Options)
	}
	submitChoices(t, e, idx)
}

// pathManaGame seeds a Commander game whose seat 0 commander is a Goblin
// (Wort, Boggart Auntie, identity B/R) with the named extra corpus cards, puts
// Path of Ancestry onto the battlefield untapped through logged events, and
// returns the engine, config and Path's id. The ETB-tapped replacement runs on
// the logged move, so a logged Untap follows (a legitimate untap effect; the
// test's subject is the spend trigger, not the ETB).
func pathManaGame(t *testing.T, seed uint64, commander string, extraNames ...string) (*Engine, Config, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	cmdr := corpusCommander(t, reg, commander)
	path := corpusCommander(t, reg, "Path of Ancestry")
	seedCards := []*cards.Card{path}
	for _, name := range extraNames {
		seedCards = append(seedCards, corpusCommander(t, reg, name))
	}
	e, cfg := colourIdentityGame(t, seed, FormatCommander, cmdr, nil, seedCards...)
	pathID := moveToBattlefieldByName(t, e, 0, "Path of Ancestry")
	if o := e.G.Obj(pathID); o == nil || !o.Tapped {
		t.Fatalf("Path of Ancestry did not enter tapped (its own ETB replacement): %+v", o)
	}
	e.emit(events.Event{Kind: events.Untap, Obj: pathID})
	e.pending = nil
	return e, cfg, pathID
}

// tapPathAndChoose taps Path of Ancestry and answers the two-colour identity
// ask with want, returning Path's mana to the pool as one provenance batch.
func tapPathAndChoose(t *testing.T, e *Engine, pathID state.ObjID, want string) {
	t.Helper()
	e.priorityRound()
	activateMana(t, e, pathID)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("Path identity ask = %+v, want a KChoose over the commander's colours", d)
	}
	submitChoices(t, e, manaOption(t, d, want))
}

// drainUntilArrange passes priority (and answers any other non-arrange ask
// with its first option) until a KArrange scry ask is pending or the stack
// empties. It returns the scry ask, or nil when none ever appeared.
func drainUntilArrange(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	for i := 0; i < 40; i++ {
		if len(e.G.Stack) == 0 {
			return nil
		}
		d := e.Pending()
		if d == nil {
			return nil
		}
		if d.Kind == decision.KArrange {
			return d
		}
		idx := -1
		if d.Kind == decision.KPriority {
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
		} else if len(d.Options) > 0 {
			idx = d.Options[0].Index
		}
		if idx < 0 {
			t.Fatalf("no answer for %+v", d)
		}
		submitChoices(t, e, idx)
	}
	t.Fatal("drain budget exhausted without an arrange ask")
	return nil
}

// TestPathOfAncestryScryOnMatchingCast is the full pin: Path of Ancestry's
// mana pays for a creature spell that shares a creature type with the Goblin
// commander, so its TriggersWhenSpent$ TrigScry rider fires and poses scry 1's
// KArrange ask.
func TestPathOfAncestryScryOnMatchingCast(t *testing.T) {
	t.Parallel()
	e, cfg, pathID := pathManaGame(t, 91, "Wort, Boggart Auntie", "Goblin Guide")
	guideID := moveSeededToHand(t, e, 0, "Goblin Guide")
	tapPathAndChoose(t, e, pathID, "R")
	if got := e.G.Players[0].Pool[state.MR]; got != 1 {
		t.Fatalf("pool R = %d, want 1 from Path of Ancestry", got)
	}
	if n := len(e.G.Players[0].RestrictedMana); n != 1 {
		t.Fatalf("RestrictedMana batches = %d, want Path's one provenance batch", n)
	} else if src := e.G.Players[0].RestrictedMana[0].Source; src != pathID {
		t.Fatalf("provenance batch Source = %d, want Path of Ancestry %d", src, pathID)
	}

	castSeeded(t, e, guideID)

	d := drainUntilArrange(t, e)
	if d == nil {
		t.Fatalf("no scry KArrange ask after the matching cast (Path's rider)")
	}
	// Path's mana was consumed paying for the cast (the batch is empty-Valid,
	// spendable anywhere), and the scry looked at exactly one card.
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("pool = %v, want the provenance batch spent on the cast", e.G.Players[0].Pool)
	}
	submitChoices(t, e, d.Options[0].Index) // keep the top card on top
	commanderReplayCheck(t, e, cfg)
}

// TestPathOfAncestrySpendGateNoPathMana proves the spend half of the gate: the
// SAME matching creature cast paid from ordinary mana fires nothing -- the
// rider needs the provenance batch, not merely a matching spell.
func TestPathOfAncestrySpendGateNoPathMana(t *testing.T) {
	t.Parallel()
	e, _, _ := pathManaGame(t, 92, "Wort, Boggart Auntie", "Goblin Guide")
	guideID := moveSeededToHand(t, e, 0, "Goblin Guide")
	addMana(t, e, 0, "R") // ordinary pool mana, no provenance batch
	castSeeded(t, e, guideID)
	if d := drainUntilArrange(t, e); d != nil {
		t.Fatalf("a matching cast paid WITHOUT Path's mana fired the rider: %+v", d)
	}
}

// TestPathOfAncestrySpendGateNonMatchingCast proves the ValidCard$ half: a
// non-matching creature cast paid WITH Path's provenance mana fires nothing,
// AND the empty-Valid batch is still spendable anywhere (the cast succeeds and
// the mana is not dead).
func TestPathOfAncestrySpendGateNonMatchingCast(t *testing.T) {
	t.Parallel()
	e, cfg, pathID := pathManaGame(t, 93, "Wort, Boggart Auntie", "Monastery Swiftspear")
	spID := moveSeededToHand(t, e, 0, "Monastery Swiftspear")
	tapPathAndChoose(t, e, pathID, "R")
	castSeeded(t, e, spID)
	if d := drainUntilArrange(t, e); d != nil {
		t.Fatalf("a non-matching cast paid WITH Path's mana fired the rider: %+v", d)
	}
	// The provenance batch paid the cast: spendable anywhere, then consumed.
	if total := e.G.Players[0].Pool.Total(); total != 0 {
		t.Fatalf("pool = %v, want Path's empty-Valid batch spent on the non-matching cast", e.G.Players[0].Pool)
	}
	if n := len(e.G.Players[0].RestrictedMana); n != 0 {
		t.Fatalf("RestrictedMana batches = %d, want the batch consumed", n)
	}
	commanderReplayCheck(t, e, cfg)
}
