package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// commanderCastCountDrakeSrc is Thunderclap Drake's verbatim corpus shape:
// the AB$ DelayedTrigger registers a one-shot SpellCast delayed trigger whose
// Execute$ copies the cast instant/sorcery `Amount$ X` times, with
// SVar:X:Count$TotalCommanderCastFromCommandZone. Before the head existed the
// amount degraded to 0 and the whole trigger resolved as a no-op copy.
const commanderCastCountDrakeSrc = `Name:Thunderclap Drake
ManaCost:1 U
Types:Creature Drake
PT:2/1
K:Flying
A:AB$ DelayedTrigger | Cost$ 2 U Sac<1/CARDNAME> | AILogic$ SpellCopy | Execute$ EffTrigCopy | ThisTurn$ True | Mode$ SpellCast | ValidCard$ Instant,Sorcery | ValidActivatingPlayer$ You | SpellDescription$ When you cast your next instant or sorcery spell this turn, copy it for each time you've cast your commander from the command zone this game.
SVar:EffTrigCopy:DB$ CopySpellAbility | Defined$ TriggeredSpellAbility | Amount$ X | MayChooseTarget$ True
SVar:X:Count$TotalCommanderCastFromCommandZone
Oracle:x
`

// commanderCastCountBoltSrc is a targetless instant with a real resolution
// effect (one draw), so the copies' resolution is observable in the hand and
// not just as StackCopy events.
const commanderCastCountBoltSrc = `Name:Count Quell
ManaCost:U
Types:Instant
A:SP$ Draw | NumCards$ 1 | SpellDescription$ Draw a card.
Oracle:x
`

// commanderCastCountGame builds a two-seat Commander-format game where seat 0
// leads its deck with a decision-free Beatstick commander (cast from the
// command zone), carries Thunderclap Drake and a targetless drawing instant
// among its deck cards, and seat 1 leads a decision-free Battle Golem
// commander. Driven to turn 1 seat 0 Main1, the point both a command-zone
// cast and a later instant cast are offered.
func commanderCastCountGame(t *testing.T, seed uint64) (*Engine, Config) {
	t.Helper()
	deck0 := append([]*cards.Card{
		card(t, commanderBeatstickSrc),
		card(t, commanderCastCountDrakeSrc),
		card(t, commanderCastCountBoltSrc),
	}, mountainDeck(t, 37)...)
	deck1 := append([]*cards.Card{card(t, commanderBattleGolemSrc)}, mountainDeck(t, 39)...)
	cfg := Config{Seed: seed, Names: []string{"a", "b"},
		Decks:      [][]*cards.Card{deck0, deck1},
		Commanders: [][]int{{0}, {0}}, StartingLife: 40, Format: FormatCommander}
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	e.Advance()
	driveToStep(t, e, 1, 0, state.StepMain1)
	return e, cfg
}

// TestThunderclapDrakeCopiesOncePerCommanderCast is the brief's end-to-end
// pin on the Count$TotalCommanderCastFromCommandZone head: cast the commander
// from the command zone twice, resolve Thunderclap Drake's {2}{U} ability,
// then cast the instant — the delayed SpellCast trigger copies it exactly
// twice. Before the head existed SVar:X degraded to 0 and the trigger copied
// nothing.
func TestThunderclapDrakeCopiesOncePerCommanderCast(t *testing.T) {
	t.Parallel()
	e, cfg := commanderCastCountGame(t, 71)
	cmd0 := e.G.Players[0].Commanders[0]

	// Precondition: no commander casts yet — the head is a resolved zero,
	// and the maintained CmdCasts slice agrees (the two reads share one
	// emitter site, so they can never disagree).
	if got := e.CommanderCastsFromCommandZone(0); got != 0 {
		t.Fatalf("head before any cast = %d, want 0", got)
	}
	if e.G.Players[0].CmdCasts[0] != 0 {
		t.Fatalf("CmdCasts[0] before any cast = %d, want 0", e.G.Players[0].CmdCasts[0])
	}

	// Cast the commander from the command zone twice (the second pays its
	// {2} CR 903.8 tax).
	castCommanderAndReturn(t, e, cmd0)
	// Event-backed funding (addMana), never a direct pool write: the whole
	// flow must replay from the log alone, and a direct Pool write the fold
	// never saw makes the replayed pool diverge.
	addMana(t, e, 0, "CC")
	castCommanderAndReturn(t, e, cmd0)
	if got := e.CommanderCastsFromCommandZone(0); got != 2 {
		t.Fatalf("head after two command-zone casts = %d, want 2", got)
	}
	if got := e.G.Players[0].CmdCasts[0]; got != 2 {
		t.Fatalf("CmdCasts[0] after two command-zone casts = %d, want 2", got)
	}
	// Per-player scoping: seat 1 has cast nothing, so their count stays 0
	// even though seat 0's is 2.
	if got := e.CommanderCastsFromCommandZone(1); got != 0 {
		t.Fatalf("seat 1 head = %d, want 0 (a seat's count never sees another seat's casts)", got)
	}

	// Negative: casting the commander from the HAND is a cast whose
	// PutOnStack origin is ZHand — it must not count. CR 903.9 poses the
	// owner's command-zone choice for a commander moving to the hand; answer
	// "leave" so the verbatim move completes and the card really is cast from
	// the hand through the real offer.
	e.pending = nil // park-ask arms only when no decision is pending
	e.emit(events.Event{Kind: events.MoveZone, Obj: cmd0, From: state.ZCommand, To: state.ZHand})
	cz := e.Pending()
	if cz == nil || cz.Kind != decision.KCommanderZone {
		t.Fatalf("no CR 903.9 command-zone choice after the fixture move: %+v", cz)
	}
	submitChoices(t, e, cz.Options[1].Index) // "Let it go to the hand"
	e.pending = nil
	e.Advance()
	opt := commanderCastOption(e, cmd0)
	if opt == nil {
		t.Fatalf("hand cast of the commander not offered: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 30)
	if got := e.CommanderCastsFromCommandZone(0); got != 2 {
		t.Fatalf("head after a hand cast = %d, want 2 (only command-zone casts count)", got)
	}
	if got := e.G.Players[0].CmdCasts[0]; got != 2 {
		t.Fatalf("CmdCasts[0] after a hand cast = %d, want 2", got)
	}

	// Resolve Thunderclap Drake's {2}{U}, Sac ability.
	drake := moveSeeded(t, e, 0, commanderCastCountDrakeSrc, state.ZBattlefield)
	addMana(t, e, 0, "CCU")
	abil := -1
	d := e.Pending()
	if d == nil || d.Kind != "priority" {
		t.Fatalf("no priority after funding: %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == drake {
			abil = o.Index
		}
	}
	if abil < 0 {
		t.Fatalf("no activate option for Thunderclap Drake: %+v", d.Options)
	}
	submitChoices(t, e, abil)
	passUntilStackEmpty(t, e, 30)
	if o := e.G.Obj(drake); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("drake zone after the Sac cost = %+v, want the graveyard (precondition: it was sacrificed)", o)
	}

	// Cast the drawing instant: the trigger copies it exactly twice.
	bolt := moveSeeded(t, e, 0, commanderCastCountBoltSrc, state.ZHand)
	addMana(t, e, 0, "U")
	castOpt := -1
	d = e.Pending()
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bolt {
			castOpt = o.Index
		}
	}
	if castOpt < 0 {
		t.Fatalf("no cast option for the instant: %+v", d.Options)
	}
	submitChoices(t, e, castOpt)
	handAfterCast := len(e.G.Zone(state.ZHand, 0))
	passUntilStackEmpty(t, e, 60)
	if got := copyCount(e, bolt); got != 2 {
		t.Fatalf("StackCopy of the cast instant = %d, want 2 (one per command-zone commander cast; got %v)",
			got, allStackCopies(e))
	}
	// The copies RESOLVED, not just got minted: the spell and each copy draw
	// one — hand = post-cast hand + 3.
	if got := len(e.G.Zone(state.ZHand, 0)); got != handAfterCast+3 {
		t.Fatalf("hand %d, want %d (the spell and its two copies each drew 1)", got, handAfterCast+3)
	}
	commanderReplayCheck(t, e, cfg)
}

// TestCommanderCastsFromCommandZoneScopesToTheOwner pins the engine read's
// filters directly, without the copy machinery: only p's OWN command-zone
// casts count, a hand-origin cast of the same object does not, and a
// non-commander card's command-zone... a non-commander seat reads 0.
func TestCommanderCastsFromCommandZoneScopesToTheOwner(t *testing.T) {
	t.Parallel()
	e, _ := commanderCastCountGame(t, 72)
	cmd0 := e.G.Players[0].Commanders[0]

	castCommanderAndReturn(t, e, cmd0)
	if got := e.CommanderCastsFromCommandZone(0); got != 1 {
		t.Fatalf("head after one command-zone cast = %d, want 1", got)
	}
	if got := e.CommanderCastsFromCommandZone(1); got != 0 {
		t.Fatalf("seat 1 head = %d, want 0 (owner scoping)", got)
	}
	if got := e.CommanderCastsFromCommandZone(state.PlayerID(9)); got != 0 {
		t.Fatalf("out-of-range seat head = %d, want 0", got)
	}

	// A hand-origin cast of the SAME object does not count (the From filter).
	// CR 903.9 poses the owner's choice for a commander moving to the hand;
	// decline the park so the verbatim move completes.
	e.pending = nil // park-ask arms only when no decision is pending
	e.emit(events.Event{Kind: events.MoveZone, Obj: cmd0, From: state.ZCommand, To: state.ZHand})
	cz := e.Pending()
	if cz == nil || cz.Kind != decision.KCommanderZone {
		t.Fatalf("no CR 903.9 command-zone choice after the fixture move: %+v", cz)
	}
	submitChoices(t, e, cz.Options[1].Index) // "Let it go to the hand"
	e.pending = nil
	e.Advance()
	opt := commanderCastOption(e, cmd0)
	if opt == nil {
		t.Fatalf("hand cast of the commander not offered: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 30)
	if got := e.CommanderCastsFromCommandZone(0); got != 1 {
		t.Fatalf("head after a hand cast = %d, want 1 (only command-zone casts count)", got)
	}
}
