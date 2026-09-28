package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Task replplay1: a DB$ Play SA's ReplaceGraveyard$ Exile rider ("If that
// spell would be put into your graveyard this turn, exile it instead" —
// Goblin Dark-Dwellers' ETB) was never read: the played spell resolved and
// rested in the graveyard. The fix stamps the played spell's pay-time
// CastInfo with the new state.FlagReplaceGraveyard (provenance of the Play
// SA, plumbed from rules/resolution.go's "play" arm through beginPlay and
// pendingCast), and spellRestZone/spellFizzleZone read the bit to send the
// card to exile.
//
// Prevalence at the corpus pin: all 37 raw `ReplaceGraveyard$` occurrences
// are `Exile`; 34 files carry one on a Play-API line (the scope here), 2 on
// a static `S:` MayPlay$ grant (Kess, Dissident Mage; Maestros Ascendancy —
// out of scope, see the report), and the conditional sibling
// ReplaceGraveyardValid$ appears on exactly 2 of the 34 (Bilbo, Thief in the
// Night; Scholar of the Lost Trove) — left fail-closed: no flag, graveyard
// resting place.
//
// TestHeads safety: the 36 ReplaceGraveyard$ CARRIERS are in NO repo deck and
// NO legacy golden deck (measured: grepping all 36 carrier card names against
// internal/testutil/decks/*.json returns nothing), so no golden game exercises
// the flag; verified unmoved below anyway. (Lightning Bolt itself IS in repo
// decks — mono-red-goblins and ur-delver — but no deck plays it through a
// ReplaceGraveyard$ Play, so its resting zone there is unchanged.)

// negMoveByName moves the named card from player p's hand/library to `to`.
// searchMoveByName is player-0-only; the negative and fizzle scenarios need
// a card parked in seat 1's zones / on seat 1's battlefield.
func negMoveByName(t *testing.T, e *Engine, p state.PlayerID, name string, to state.Zone) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, p) {
			o := e.G.Obj(id)
			if o != nil && o.Face() != nil && o.Face().Name == name {
				if z != to {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: to})
				}
				e.pending = nil
				e.priorityRound()
				return id
			}
		}
	}
	t.Fatalf("corpus fixture %q absent from player %d hand/library", name, p)
	return 0
}

// gddEngine deals seat 0 a deck whose only non-mountain cards are a Goblin
// Dark-Dwellers and a Lightning Bolt, seat 1 all mountains.
func gddEngine(t *testing.T, reg *cards.Registry) (*Engine, Config) {
	t.Helper()
	gdd := searchCorpusCard(t, reg, "Goblin Dark-Dwellers")
	bolt := searchCorpusCard(t, reg, "Lightning Bolt")
	mountain := searchCorpusCard(t, reg, "Mountain")
	deck0 := []*cards.Card{gdd, bolt}
	for len(deck0) < 40 {
		deck0 = append(deck0, mountain)
	}
	deck1 := make([]*cards.Card, 40)
	for i := range deck1 {
		deck1[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: 7402, Names: []string{"dwellers", "opponent"},
		Decks: [][]*cards.Card{deck0, deck1}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// gddDrive walks the whole Goblin Dark-Dwellers scenario: the ETB trigger's
// target ask (the graveyard's Bolt is the only option), the may-cast
// KModes/"play" ask answered "yes" (option 0), the played Bolt's own damage
// target, and the priority passes until everything settles. The walk stops
// once the Bolt has reached its resting zone (the scenario's own end) at a
// turn boundary — never mid-ask — rather than playing on into the
// opponent's next turn.
func gddDrive(t *testing.T, e *Engine, boltID state.ObjID, final state.Zone) {
	t.Helper()
	for i := 0; i < 40; i++ {
		d := e.Pending()
		if d == nil {
			return
		}
		if o := e.G.Obj(boltID); o != nil && o.Zone == final &&
			(d.Kind == decision.KPriority || d.ResumeKind == "") && d.Kind != decision.KModes {
			return
		}
		switch d.Kind {
		case decision.KPriority:
			submitPass(t, e)
		case decision.KTarget:
			submitChoices(t, e, 0)
		case decision.KModes:
			if d.ResumeKind == "play" {
				submitChoices(t, e, 0)
			} else {
				submitChoices(t, e)
			}
		default:
			t.Fatalf("unexpected decision %d: kind=%s resume=%q", i, d.Kind, d.ResumeKind)
		}
	}
	t.Fatal("the Goblin Dark-Dwellers drive did not settle within 40 decisions")
}

// TestGoblinDarkDwellersExilesThePlayedCard: the ETB trigger targets the
// graveyard's Lightning Bolt, the may-cast is taken, the free cast resolves
// — and the Bolt must rest in EXILE (the rider), not the graveyard. Before
// the fix the played card landed in the graveyard, because a free Play cast
// carried no CastFlags bit at all and spellRestZone fell to its
// ZGraveyard default.
func TestGoblinDarkDwellersExilesThePlayedCard(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := gddEngine(t, reg)
	boltID := searchMoveByName(t, e, "Lightning Bolt", state.ZGraveyard)
	searchMoveByName(t, e, "Goblin Dark-Dwellers", state.ZBattlefield)
	gddDrive(t, e, boltID, state.ZExile)

	if o := e.G.Obj(boltID); o == nil || o.Zone != state.ZExile {
		t.Fatalf("the played Bolt rests in %v, want exile (the ReplaceGraveyard$ Exile rider)", o)
	}
	// The provenance rides the pay-time CastInfo (the exact event a replay
	// marks the spell with): one cast_info carrying the new flag, nothing
	// else — a free Play cast emits no other CastInfo.
	infos := castInfosFor(e, boltID)
	if len(infos) != 1 {
		t.Fatalf("%d cast_info events for the played Bolt, want exactly 1", len(infos))
	}
	if infos[0][0] != "replacegraveyard" {
		t.Fatalf("cast_info counter = %q, want %q", infos[0][0], "replacegraveyard")
	}
	if o := e.G.Obj(boltID); o != nil && o.CastFlags&state.FlagReplaceGraveyard != 0 {
		t.Fatalf("the flag leaked past the resting move: castflags=%x", o.CastFlags)
	}
	replayCheck(t, e, cfg)
}

// TestGoblinDarkDwellersFizzleExilesThePlayedCard: the fizzle half of the
// reader. A played spell that never resolves (CR 608.2b: every target illegal
// at resolution) must also be exiled when its Play SA carried the rider —
// spellFizzleZone reads the same flag. The played Bolt is aimed at a creature,
// which leaves the battlefield before the Bolt resolves.
func TestGoblinDarkDwellersFizzleExilesThePlayedCard(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	gdd := searchCorpusCard(t, reg, "Goblin Dark-Dwellers")
	bolt := searchCorpusCard(t, reg, "Lightning Bolt")
	bears := searchCorpusCard(t, reg, "Grizzly Bears")
	mountain := searchCorpusCard(t, reg, "Mountain")
	deck0 := []*cards.Card{gdd, bolt}
	for len(deck0) < 40 {
		deck0 = append(deck0, mountain)
	}
	deck1 := []*cards.Card{bears}
	for len(deck1) < 40 {
		deck1 = append(deck1, mountain)
	}
	cfg := seatZeroStart(Config{Seed: 7404, Names: []string{"dwellers", "opponent"},
		Decks: [][]*cards.Card{deck0, deck1}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	boltID := searchMoveByName(t, e, "Lightning Bolt", state.ZGraveyard)
	// A creature for the Bolt to target, on seat 1's battlefield.
	bearID := negMoveByName(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	searchMoveByName(t, e, "Goblin Dark-Dwellers", state.ZBattlefield)

	// Walk to the played Bolt's own target ask, choosing the Bears.
	chosen := false
	for i := 0; i < 40 && !chosen; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision pending before the Bolt's target ask")
		}
		switch d.Kind {
		case decision.KPriority:
			submitPass(t, e)
		case decision.KTarget:
			idx := -1
			for _, o := range d.Options {
				if o.Obj == bearID {
					idx = o.Index
				}
			}
			if idx < 0 {
				// The ETB trigger's graveyard target ask: pick the Bolt.
				submitChoices(t, e, 0)
				continue
			}
			submitChoices(t, e, idx)
			chosen = true
		case decision.KModes:
			submitChoices(t, e, 0)
		default:
			t.Fatalf("unexpected decision %d: kind=%s resume=%q", i, d.Kind, d.ResumeKind)
		}
	}
	if !chosen {
		t.Fatal("the played Bolt never posed its target ask")
	}
	if o := e.G.Obj(boltID); o == nil || o.Zone != state.ZStack {
		t.Fatalf("the played Bolt is in %v, want the stack after targeting", o)
	}
	// The target leaves; the Bolt fizzles at resolution.
	e.emit(events.Event{Kind: events.MoveZone, Obj: bearID, From: state.ZBattlefield, To: state.ZGraveyard})
	e.resolveTop()
	if o := e.G.Obj(boltID); o == nil || o.Zone != state.ZExile {
		t.Fatalf("the fizzled played Bolt rests in %v, want exile", o)
	}
	replayCheck(t, e, cfg)
}

// TestPlayReplaceGraveyardDoesNotLeak: the flag is per-SA provenance, never
// a Play-mode-wide default. Two shapes on real corpus cards must still rest
// in the graveyard:
//  1. an ORDINARY hand cast of the same Lightning Bolt (no Play SA, no flag);
//  2. a FREE Play cast whose SA carries NO ReplaceGraveyard$ — Chancellor of
//     the Spires' ETB ("you may cast target instant or sorcery card from an
//     opponent's graveyard without paying its mana cost"), the exact mirror
//     of Goblin Dark-Dwellers' trigger shape with the rider omitted. The
//     scenario ASSERTS a ResumeKind=="play" ask actually occurred, so a dead
//     Play path can never masquerade as a passing negative (the round-1
//     negative half used Jace's Mindseeker, whose RememberMilled$
//     dependency is unimplemented, so its Play was never offered at all).
func TestPlayReplaceGraveyardDoesNotLeak(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)

	// (1) the ordinary hand cast: funded {R}, cast at seat 1, resolves, and
	// the Bolt rests in the graveyard with no CastInfo at all (a plain cast
	// carries no flag and no X).
	e, cfg := gddEngine(t, reg)
	boltID := searchMoveByName(t, e, "Lightning Bolt", state.ZHand)
	addMana(t, e, 0, "R")
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("after funding: %+v, want priority", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == boltID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for the Bolt: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		submitChoices(t, e, 0)
	}
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(boltID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the hand-cast Bolt rests in %v, want the graveyard", o)
	}
	if n := len(castInfosFor(e, boltID)); n != 0 {
		t.Fatalf("%d cast_info event(s) on an ordinary hand cast, want 0 (no flag, no X)", n)
	}
	replayCheck(t, e, cfg)

	// (2) Chancellor of the Spires: the ETB trigger poses a real KTarget ask
	// over seat 1's graveyard (the Bolt is the only instant/sorcery there),
	// then a free DB$ Play | TgtZone$ Graveyard | ValidTgts$ Instant.OppOwn,
	// Sorcery.OppOwn | WithoutManaCost$ True | Optional$ True with NO
	// ReplaceGraveyard$. The played Bolt resolves and must rest in the
	// graveyard, and the scenario must have seen the Play ask at all.
	chanc := searchCorpusCard(t, reg, "Chancellor of the Spires")
	mountain := searchCorpusCard(t, reg, "Mountain")
	deck0 := make([]*cards.Card, 40)
	for i := range deck0 {
		deck0[i] = mountain
	}
	deck0[0] = chanc
	deck1 := make([]*cards.Card, 40)
	for i := range deck1 {
		deck1[i] = mountain
	}
	deck1[0] = searchCorpusCard(t, reg, "Lightning Bolt")
	cfg2 := seatZeroStart(Config{Seed: 7403, Names: []string{"chancellor", "opponent"},
		Decks: [][]*cards.Card{deck0, deck1}, Tokens: reg.Tokens})
	e2 := New(cfg2)
	e2.Advance()
	// Chancellor's MayEffectFromOpeningHand reveal: decline the pregame ask.
	if d := e2.Pending(); d != nil && d.Kind == decision.KChoose {
		submitChoices(t, e2, 1)
	}
	toMain1(t, e2)
	// Park the Bolt in seat 1's graveyard (the Play's only legal target).
	bolt2 := negMoveByName(t, e2, 1, "Lightning Bolt", state.ZGraveyard)
	// Put the Chancellor onto seat 0's battlefield (may have been drawn).
	searchMoveByName(t, e2, "Chancellor of the Spires", state.ZBattlefield)
	playAsks := 0
	for i := 0; i < 60; i++ {
		d := e2.Pending()
		if d == nil {
			break
		}
		if o := e2.G.Obj(bolt2); o != nil && playAsks > 0 &&
			(o.Zone == state.ZGraveyard || o.Zone == state.ZExile || o.Zone == state.ZHand) &&
			(d.Kind == decision.KPriority || d.ResumeKind == "") && d.Kind != decision.KModes {
			break
		}
		switch d.Kind {
		case decision.KPriority:
			submitPass(t, e2)
		case decision.KTarget:
			submitChoices(t, e2, 0)
		case decision.KModes:
			if d.ResumeKind == "play" {
				playAsks++
			}
			submitChoices(t, e2, 0)
		case decision.KChoose:
			submitChoices(t, e2, 0)
		default:
			t.Fatalf("unexpected decision %d: kind=%s resume=%q", i, d.Kind, d.ResumeKind)
		}
	}
	// The negative is only meaningful if the Play path actually ran: assert
	// the ResumeKind=="play" ask fired (round-1's Jace's Mindseeker half
	// never offered one, so it could not fail).
	if playAsks == 0 {
		t.Fatal("the rider-less Play never posed a ResumeKind==\"play\" ask")
	}
	if o := e2.G.Obj(bolt2); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the rider-less Play's Bolt rests in %v, want the graveyard", o)
	}
	for _, info := range castInfosFor(e2, bolt2) {
		if strings.Contains(info[0], "replacegraveyard") {
			t.Fatalf("the rider-less Play stamped the flag: counter=%q", info[0])
		}
	}
	replayCheck(t, e2, cfg2)
}
