package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// EachDamage end-to-end pins on REAL corpus cards (the grammar is measured
// over 29 corpus carriers; the effects-package grammar table lives in
// effects/eachdamage_test.go). No carrier is in any repo deck, so no chain
// head or ratchet entry depends on this primitive. The helpers come from
// search_library_test.go / loyalty_test.go (same package); decks are built
// from compiled corpus cards only, so no Forge script text is committed.

// eachBoard deals seat 0 a deck led by cast0's cards plus board0's and seat
// 1 one led by board1's, moves every board card ONTO its battlefield
// through LOGGED MoveZone events (so entry grants and replays run for
// real), and parks the game at turn 2 seat 0 Main1 with seat 0 to act. The
// cast0 cards stay in the shuffled deck (found in hand/library later).
func eachBoard(t *testing.T, reg *cards.Registry, cast0, board0, board1 []*cards.Card) (*Engine, Config) {
	t.Helper()
	deck0 := append(append(append([]*cards.Card(nil), cast0...), board0...), mountainDeck(t, 40-len(cast0)-len(board0))...)
	deck1 := append(append([]*cards.Card(nil), board1...), mountainDeck(t, 40-len(board1))...)
	cfg := seatZeroStart(Config{Seed: 61, Names: []string{"each", "opponent"},
		Decks: [][]*cards.Card{deck0, deck1}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	move := func(owner state.PlayerID, cs []*cards.Card) {
		for i := range e.G.Objs {
			o := &e.G.Objs[i]
			if o.Owner != owner {
				continue
			}
			for _, c := range cs {
				if o.Card == c {
					e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZBattlefield})
				}
			}
		}
	}
	move(0, board0)
	move(1, board1)
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	// The pending decision Advance left is stale (computed before the board
	// moves): reset it so the next option read sees the moved board, the
	// same reset searchMoveByName performs.
	e.pending = nil
	e.priorityRound()
	return e, cfg
}

// boardID finds the battlefield object whose face name is name, fatal when
// absent.
func boardID(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	for _, id := range append(e.G.Zone(state.ZBattlefield, 0), e.G.Zone(state.ZBattlefield, 1)...) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	t.Fatalf("no battlefield object named %q", name)
	return 0
}

// unimplementedNotes collects any "unimplemented API ..." Note.
func unimplementedNotes(e *Engine) []string {
	var out []string
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API") {
			out = append(out, ev.Text)
		}
	}
	return out
}

// TestWaveOfReckoningSurvivorsAreExactlyToughnessGreaterThanPower is the
// reported symptom end to end: Wave of Reckoning ("Each creature deals
// damage to itself equal to its power") resolves as a REAL simultaneous
// sweep -- creatures with power >= toughness die, power < toughness
// survive. Sedge Scorpion's deathtouch mark proves each hit's SOURCE is the
// creature itself: the deathtouch mark is emitted by the per-damager rider
// reading HasKeyword(DAMAGER, "Deathtouch"), so a wrong source binding
// (the spell, say) would leave no mark anywhere.
func TestWaveOfReckoningSurvivorsAreExactlyToughnessGreaterThanPower(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := eachBoard(t, reg,
		[]*cards.Card{mustCorpusCard(t, reg, "Wave of Reckoning")},
		[]*cards.Card{mustCorpusCard(t, reg, "Grizzly Bears"), // 2/2: dies
			mustCorpusCard(t, reg, "Wall of Roots"),   // 0/5: survives
			mustCorpusCard(t, reg, "Sedge Scorpion")}, // 1/1 deathtouch: dies, marks itself
		[]*cards.Card{mustCorpusCard(t, reg, "Craw Wurm")}) // 6/4: dies
	toMain1(t, e)

	bears := boardID(t, e, "Grizzly Bears")
	wall := boardID(t, e, "Wall of Roots")
	scorpion := boardID(t, e, "Sedge Scorpion")
	wurm := boardID(t, e, "Craw Wurm")
	id := searchMoveByName(t, e, "Wave of Reckoning", state.ZHand)
	addMana(t, e, 0, "WWWWW")
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Wave of Reckoning: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
	if notes := unimplementedNotes(e); len(notes) != 0 {
		t.Fatalf("unimplemented-API notes = %v, want none", notes)
	}

	zones := map[state.ObjID]state.Zone{}
	for _, id := range []state.ObjID{bears, wall, scorpion, wurm} {
		zones[id] = e.G.Obj(id).Zone
	}
	if zones[wall] != state.ZBattlefield {
		t.Fatalf("0/5 Wall of Roots zone = %s, want battlefield (toughness > power survives)", zones[wall])
	}
	for _, id := range []state.ObjID{bears, scorpion, wurm} {
		if zones[id] != state.ZGraveyard {
			t.Fatalf("%s zone = %s, want graveyard (power >= toughness dies)", e.G.Obj(id).Face().Name, zones[id])
		}
	}

	// The source binding: the scorpion's own hit marked IT Deathtouched --
	// the mark event names the scorpion (the mark is cleared when the SBA
	// then moves it to the graveyard, so read the log).
	marks := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.CounterChange && ev.Counter == "Deathtouched" && ev.Obj == scorpion {
			marks++
		}
	}
	if marks != 1 {
		t.Fatalf("Sedge Scorpion Deathtouched marks = %d, want 1 (the per-damager rider read the SCORPION as the damage source)", marks)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.CounterChange && ev.Counter == "Deathtouched" && ev.Obj != scorpion {
			t.Fatalf("obj %d carries a Deathtouch mark with no deathtouch damager", ev.Obj)
		}
	}
	replayCheck(t, e, cfg)
}

// TestGrimContestToEachOtherDealsToughnessBothWays pins the ToEachOther$
// shape end to end on the real card: the announcement names your creature
// and then the opponent's (both targets are chosen on cast, CR 601.2c), and
// each of the two creatures deals damage equal to its TOUGHNESS to the
// other (per-damager amounts: the 0/5 wall deals 5, the 6/4 wurm deals 4).
func TestGrimContestToEachOtherDealsToughnessBothWays(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := eachBoard(t, reg,
		[]*cards.Card{mustCorpusCard(t, reg, "Grim Contest")},
		[]*cards.Card{mustCorpusCard(t, reg, "Wall of Roots")},
		[]*cards.Card{mustCorpusCard(t, reg, "Craw Wurm")})
	toMain1(t, e)

	id := searchMoveByName(t, e, "Grim Contest", state.ZHand)
	addMana(t, e, 0, "BGG")
	wall := boardID(t, e, "Wall of Roots")
	wurm := boardID(t, e, "Craw Wurm")

	// Cast, answer the announcement ask with your own creature.
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Grim Contest: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	if d = e.Pending(); d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending after cast = %+v, want the Creature.YouCtrl target ask", d)
	}
	targetObject(t, e, wall)

	// CR 601.2c: the DB sub's "target creature an opponent controls" is
	// announced on cast too, right after the first target.
	d = castSubAsk(t, e)
	subIdx := -1
	for _, o := range d.Options {
		if o.Obj == wurm {
			subIdx = o.Index
		}
	}
	if subIdx < 0 {
		t.Fatalf("Craw Wurm not offered: %+v", d.Options)
	}
	submitChoices(t, e, subIdx)
	// Both announced: the spell is on the stack. It resolves on two passes
	// and asks nothing more.
	for i := 0; i < 2; i++ {
		if d = e.Pending(); d == nil || d.Kind != decision.KPriority {
			t.Fatalf("pending after the announcement = %+v, want priority", d)
		}
		submitChoices(t, e, passIndex(t, d))
	}
	if d = e.Pending(); d == nil || d.Kind != decision.KPriority || len(e.G.Stack) != 0 {
		t.Fatalf("pending after two passes = %+v (stack %v), want priority with the resolution complete", d, e.G.Stack)
	}

	// Each dealt its OWN toughness to the other: the wall took 4 (survives
	// with 4 marked), the wurm took 5 and died.
	if o := e.G.Obj(wurm); o.Zone != state.ZGraveyard {
		t.Fatalf("Craw Wurm zone = %s, want graveyard (took the wall's toughness 5 >= its 4)", o.Zone)
	}
	if o := e.G.Obj(wall); o.Zone != state.ZBattlefield {
		t.Fatalf("Wall of Roots zone = %s, want battlefield (took the wurm's toughness 4 < its 5)", o.Zone)
	} else if o.Damage != 4 {
		t.Fatalf("Wall of Roots marked damage = %d, want exactly 4 (the WURM's toughness, not its own 5)", o.Damage)
	}
	var dmg [2]int32
	for _, ev := range e.L.Events {
		if ev.Kind == events.Damage && ev.Obj == wall {
			dmg[0] = ev.Amount
		}
		if ev.Kind == events.Damage && ev.Obj == wurm {
			dmg[1] = ev.Amount
		}
	}
	if dmg[0] != 4 || dmg[1] != 5 {
		t.Fatalf("damage log = wall %d / wurm %d, want 4 / 5 (each member's own toughness)", dmg[0], dmg[1])
	}
	if notes := unimplementedNotes(e); len(notes) != 0 {
		t.Fatalf("unimplemented-API notes = %v, want none", notes)
	}
	replayCheck(t, e, cfg)
}

// TestSarkhanTheMadUltimatePinsDamagersAndTargetRecipients pins the
// DefinedDamagers$ + ValidTgts$ shape on a castable card: Sarkhan the
// Mad's [-4] makes each Dragon you control deal damage equal to ITS power
// to target player or planeswalker -- the damagers come from the ValidCards
// sweep, the recipient from the ability's own announcement ask.
func TestSarkhanTheMadUltimatePinsDamagersAndTargetRecipients(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	sarkhanCard := mustCorpusCard(t, reg, "Sarkhan the Mad")
	e, cfg := eachBoard(t, reg, nil,
		[]*cards.Card{sarkhanCard,
			mustCorpusCard(t, reg, "Shivan Dragon")}, // 5/5 Dragon
		[]*cards.Card{mustCorpusCard(t, reg, "Wall of Roots")})
	toMain1(t, e)

	sarkhan := boardID(t, e, "Sarkhan the Mad")
	ult := jaceAbility(t, sarkhanCard, "SubCounter", 4)
	submitChoices(t, e, abilityOption(t, e, sarkhan, ult).Index)
	targetPlayer(t, e, 1)
	passUntilStackEmpty(t, e, 20)

	if notes := unimplementedNotes(e); len(notes) != 0 {
		t.Fatalf("unimplemented-API notes = %v, want none", notes)
	}
	var dmg []int32
	for _, ev := range e.L.Events {
		if ev.Kind == events.Damage && ev.Player == 1 && ev.Obj == 0 {
			dmg = append(dmg, ev.Amount)
		}
	}
	if len(dmg) != 1 || dmg[0] != 5 {
		t.Fatalf("opponent damage events = %v, want one hit of 5 (Shivan Dragon's own power)", dmg)
	}
	if life := e.G.Players[1].Life; life != 15 {
		t.Fatalf("opponent life = %d, want 15 (20 minus the dragon's 5)", life)
	}
	replayCheck(t, e, cfg)
}
