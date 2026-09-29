package view

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// var _ Chars = (*rules.Engine)(nil) is Ruling F2: rules never imports view,
// so this is the only place the shapes are checked against each other.
var _ Chars = (*rules.Engine)(nil)

// flatChars is a stand-in for the engine: printed characteristics only.
type flatChars struct{ g *state.Game }

func (c flatChars) Power(id state.ObjID) int32 {
	if o := c.g.Obj(id); o != nil && o.Face() != nil {
		return int32(o.Face().Power())
	}
	return 0
}
func (c flatChars) Toughness(id state.ObjID) int32 {
	if o := c.g.Obj(id); o != nil && o.Face() != nil {
		return int32(o.Face().Toughness())
	}
	return 0
}
func (c flatChars) Keywords(id state.ObjID) []string {
	if o := c.g.Obj(id); o != nil && o.Face() != nil {
		return o.Face().Keywords
	}
	return nil
}

// PendingTriggers is not exercised by flatChars-driven tests: they build
// their own board directly, never through an Engine, so there is never
// anything queued. The R3 integration tests below drive a real
// *rules.Engine instead, exactly so PendingTriggers is exercised for real.
func (c flatChars) PendingTriggers() []state.PendingTrigger { return nil }

// StackOptional is not exercised by flatChars-driven tests either: it is
// Ruling VW-1's stack-entry answer, and flatChars reports no stack object
// as an optional awaiting trigger.
func (c flatChars) StackOptional(state.ObjID) (bool, state.PlayerID) { return false, 0 }

// AvailableMana is not exercised by flatChars-driven tests that check the
// projection: the real value is the rules.Engine's answer (pinned by
// rules' TestAvailableMana*), and the view's job is only to project whatever
// Chars hands it. flatChars therefore reports zero availability, so every
// existing flatChars-driven view is an empty-availability view; the one test
// that wants a nonzero Available drives the projection through a custom
// Chars instead (see availableProjection below).
func (c flatChars) AvailableMana(state.PlayerID) state.Mana                    { return state.Mana{} }
func (c flatChars) PotentialActions(state.PlayerID) []decision.PotentialAction { return nil }

// OwnDeck is not exercised by flatChars-driven projection tests: they build
// boards directly with no rules.Config, so there is no genesis deck to name.
// The real engine's manifest is pinned by rules' TestEngineOwnDeck* and by
// the view's own-seat tests, which drive a real *rules.Engine.
func (c flatChars) OwnDeck(state.PlayerID) *deck.Manifest { return nil }
func (c flatChars) AbilityCosts(_ state.PlayerID, id state.ObjID) []string {
	if o := c.g.Obj(id); o != nil && o.Face() != nil {
		return printedNonManaAbilityCosts(o.Face())
	}
	return nil
}

// MayLookAtLibraryTop is not exercised by flatChars-driven tests either:
// the real value is the rules.Engine's answer (pinned by rules'
// static-grant tests), and the view's job is only to project whatever
// Chars hands it. flatChars therefore never reveals a library top; the
// MayLookAt tests drive the projection through a real engine.
func (c flatChars) MayLookAtLibraryTop(state.PlayerID) bool { return false }

func fourSeatBoard(t *testing.T) *state.Game {
	t.Helper()
	g := state.NewGame([]string{"alice", "bob", "carol", "dave"})
	c, _ := cards.ParseBytes("b.txt", []byte("Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nK:Trample\nOracle:x\n"))
	c.Link()
	for p := state.PlayerID(0); p < 4; p++ {
		var lib, hand, bf []state.ObjID
		for i := 0; i < 10; i++ {
			lib = append(lib, g.AddObject(c, p).ID)
		}
		for i := 0; i < 3; i++ {
			o := g.AddObject(c, p)
			o.Zone = state.ZHand
			hand = append(hand, o.ID)
		}
		o := g.AddObject(c, p)
		o.Zone = state.ZBattlefield
		bf = append(bf, o.ID)
		g.SetZone(state.ZLibrary, p, lib)
		g.SetZone(state.ZHand, p, hand)
		g.SetZone(state.ZBattlefield, p, bf)
		g.Players[p].Pool[state.MG] = 2
	}
	return g
}

// TestOnlyTheViewerSeesTheirHandButEverySeatsPool pins the corrected split:
// only the hand is gated on "is this the viewer's own seat" (CR 400.2 names
// hand as a hidden zone), while the mana pool is public (CR 106.4a/106.4b) and
// so every seat reads every seat's pool. Each seat in the fixture floats G:2,
// so a reader cannot pass by accident on an empty pool.
func TestOnlyTheViewerSeesTheirHandButEverySeatsPool(t *testing.T) {
	g := fourSeatBoard(t)
	for viewer := state.PlayerID(0); viewer < 4; viewer++ {
		v := Project(g, flatChars{g}, viewer, nil)
		for _, pv := range v.Players {
			// Every seat's pool is public (CR 106.4a/106.4b): the viewer reads
			// the same G:2 for every seat, including seats they do not own.
			if pv.Pool == nil {
				t.Fatalf("viewer %d reads no pool for seat %d: pool is public", viewer, pv.ID)
			}
			if pv.Pool["G"] != 2 {
				t.Fatalf("viewer %d reads seat %d pool = %v, want G:2 (public)", viewer, pv.ID, pv.Pool)
			}
			if pv.ID == viewer {
				if len(pv.Hand) != 3 {
					t.Fatalf("viewer %d cannot see own hand", viewer)
				}
				continue
			}
			if pv.Hand != nil {
				t.Fatalf("viewer %d sees seat %d hand", viewer, pv.ID)
			}
			if pv.HandSize != 3 {
				t.Fatalf("hand size should still be public, got %d", pv.HandSize)
			}
		}
	}
}

// TestLibraryContentsNeverAppearInAnyProjection searches the marshalled
// payload for every OTHER seat's library object ids (the negative checks)
// but also, per Task 23 supplement §5, for the viewer's OWN hand ids (the
// positive control): with Go's default "ID": key capitalisation the
// negative checks would pass for ANY payload, proving nothing. The control
// fails loudly if that regresses.
// collectIDs walks a value decoded by json.Unmarshal into `any` (so a
// map[string]any / []any / float64 / string / bool / nil tree) and returns
// every number found under an id-bearing key: "id", "obj", "source", or
// inside an "ids"/"pairs" array (including nested arrays, which is what
// "pairs" -- []["obj","obj"] pairs -- needs). This walks the WHOLE payload
// structurally, unlike a "id":N, substring search: after PlayerView.ID's
// "seat" rename (Ruling T23-q), the only places a leaked object id can
// surface are exactly these keys, wherever in the tree they occur -- inside
// "stack", "pending", the attached "decision", a nested "targets" array,
// anything. Review finding I-3: a fixed key name misses all of those.
func collectIDs(v any) []int {
	var out []int
	var walk func(v any, idBearing bool)
	walk = func(v any, idBearing bool) {
		switch x := v.(type) {
		case map[string]any:
			for k, val := range x {
				switch k {
				case "id", "obj", "source":
					if n, ok := val.(float64); ok {
						out = append(out, int(n))
					}
				case "ids", "pairs":
					walk(val, true)
				default:
					walk(val, false)
				}
			}
		case []any:
			for _, item := range x {
				if idBearing {
					if n, ok := item.(float64); ok {
						out = append(out, int(n))
						continue
					}
				}
				walk(item, idBearing)
			}
		}
	}
	walk(v, false)
	return out
}

// idSet turns collectIDs' output into a membership set for O(1) lookups.
func idSet(ids []int) map[int]bool {
	m := make(map[int]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

// idsFoundIn marshals v and returns every id collectIDs finds in it.
func idsFoundIn(t *testing.T, v View) map[int]bool {
	t.Helper()
	blob, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err := json.Unmarshal(blob, &decoded); err != nil {
		t.Fatal(err)
	}
	return idSet(collectIDs(decoded))
}

// TestLibraryContentsNeverAppearInAnyProjection is Task 23 review finding
// I-3's fix: rather than searching for one literal substring, it decodes
// the whole payload and walks it (idsFoundIn/collectIDs) so a leak surfacing
// under ANY id-bearing key -- not just CardView.ID -- is still caught. The
// positive control (the viewer's own hand ids must all be findable) proves
// the walk actually reaches into the payload; without it, an empty result
// would prove nothing.
func TestLibraryContentsNeverAppearInAnyProjection(t *testing.T) {
	g := fourSeatBoard(t)
	for viewer := state.PlayerID(0); viewer < 4; viewer++ {
		found := idsFoundIn(t, Project(g, flatChars{g}, viewer, nil))

		for _, id := range g.Zone(state.ZHand, viewer) {
			if !found[int(id)] {
				t.Fatalf("positive control failed: viewer %d's own hand id %d is not findable in its own payload — the walk proves nothing", viewer, id)
			}
		}
		for other := state.PlayerID(0); other < 4; other++ {
			if other == viewer {
				continue
			}
			for _, id := range g.Zone(state.ZLibrary, other) {
				if found[int(id)] {
					t.Fatalf("viewer %d payload contains seat %d's library object %d", viewer, other, id)
				}
			}
			for _, id := range g.Zone(state.ZHand, other) {
				if found[int(id)] {
					t.Fatalf("viewer %d payload contains seat %d's hand object %d", viewer, other, id)
				}
			}
		}
	}
}

func TestDecisionIsAttachedOnlyToItsOwner(t *testing.T) {
	g := fourSeatBoard(t)
	d := &decision.Decision{Seq: 3, Player: 2, Kind: decision.KPriority,
		Options: []decision.Option{{Index: 0, Kind: "pass", Label: "Pass"}}}
	for viewer := state.PlayerID(0); viewer < 4; viewer++ {
		v := Project(g, flatChars{g}, viewer, d)
		if viewer == 2 {
			if v.Decision == nil {
				t.Fatal("owner did not receive their decision")
			}
			continue
		}
		if v.Decision != nil {
			t.Fatalf("viewer %d received seat 2's decision", viewer)
		}
	}
}

func TestCardViewUsesDerivedCharacteristics(t *testing.T) {
	g := fourSeatBoard(t)
	id := g.Zone(state.ZBattlefield, 0)[0]
	g.Obj(id).AddCounter("P1P1", 2)
	v := Project(g, boostedChars{flatChars{g}}, 0, nil)
	cv := v.Players[0].Battlefield[0]
	if cv.Power != 4 || cv.Toughness != 4 {
		t.Fatalf("view P/T = %d/%d, want the derived 4/4", cv.Power, cv.Toughness)
	}
	if len(cv.Keywords) == 0 || cv.Keywords[0] != "Trample" {
		t.Fatalf("keywords = %v", cv.Keywords)
	}
}

// TestCardViewCarriesBattlefieldOnlyState is review finding I-1's mutation
// gap: gutting Tapped/Damage/Attacking/Controller/Owner/SummonSick from
// cardView left the suite green. Controller is deliberately set to a THIRD
// value, neither 0 nor the object's own Owner, to prove the two are read
// independently rather than one accidentally aliasing the other (supplement
// §9: "Controller and Owner can differ").
func TestCardViewCarriesBattlefieldOnlyState(t *testing.T) {
	g := fourSeatBoard(t)
	id := g.Zone(state.ZBattlefield, 0)[0]
	o := g.Obj(id)
	o.Tapped = true
	o.Damage = 3
	o.IsAttacking = true
	o.Controller = 2
	o.Owner = 3
	o.SummonSick = true

	cv := Project(g, flatChars{g}, 0, nil).Players[0].Battlefield[0]
	if !cv.Tapped {
		t.Error("Tapped was not projected")
	}
	if cv.Damage != 3 {
		t.Errorf("Damage = %d, want 3", cv.Damage)
	}
	if !cv.Attacking {
		t.Error("Attacking was not projected")
	}
	// Both non-zero and mutually distinct, so a gutted field can never
	// coincidentally pass by matching the OTHER field's expected value or
	// a shared zero default.
	if cv.Controller != 2 {
		t.Errorf("Controller = %d, want 2 (independent of Owner)", cv.Controller)
	}
	if cv.Owner != 3 {
		t.Errorf("Owner = %d, want 3 (independent of Controller)", cv.Owner)
	}
	if !cv.SummonSick {
		t.Error("SummonSick was not projected")
	}
}

// TestPlayerViewProjectsGraveyardAndExileWithTheirSizes is review finding
// I-1's other mutation gap: LibrarySize/GraveyardSize/Graveyard/Exile had
// no assertion anywhere in the suite. Library is given 5 cards specifically
// so LibrarySize's assertion cannot pass merely because an int field's zero
// value happens to match an empty zone.
func TestPlayerViewProjectsGraveyardAndExileWithTheirSizes(t *testing.T) {
	g := state.NewGame([]string{"alice", "bob"})
	c, _ := cards.ParseBytes("t.txt", []byte("Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"))
	c.Link()

	var libIDs []state.ObjID
	for i := 0; i < 5; i++ {
		libIDs = append(libIDs, g.AddObject(c, 0).ID)
	}
	g.SetZone(state.ZLibrary, 0, libIDs)

	gy := g.AddObject(c, 0)
	gy.Zone = state.ZGraveyard
	g.SetZone(state.ZGraveyard, 0, []state.ObjID{gy.ID})

	ex := g.AddObject(c, 0)
	ex.Zone = state.ZExile
	g.SetZone(state.ZExile, 0, []state.ObjID{ex.ID})

	pv := Project(g, flatChars{g}, 0, nil).Players[0]
	if pv.LibrarySize != 5 {
		t.Errorf("LibrarySize = %d, want 5", pv.LibrarySize)
	}
	if pv.GraveyardSize != 1 {
		t.Errorf("GraveyardSize = %d, want 1", pv.GraveyardSize)
	}
	if len(pv.Graveyard) != 1 || pv.Graveyard[0].ID != gy.ID {
		t.Errorf("Graveyard = %+v, want [%d]", pv.Graveyard, gy.ID)
	}
	if len(pv.Exile) != 1 || pv.Exile[0].ID != ex.ID {
		t.Errorf("Exile = %+v, want [%d]", pv.Exile, ex.ID)
	}
}

// TestPhaseOfCoversEveryStep is review finding I-1's third mutation gap:
// phaseOf had no dedicated test at all. Covers all 12 defined steps plus an
// invalid one (supplement §6's own "unknown step -> \"\"" case).
func TestPhaseOfCoversEveryStep(t *testing.T) {
	g := fourSeatBoard(t)
	for _, tc := range []struct {
		step state.Step
		want string
	}{
		{state.StepUntap, "beginning"},
		{state.StepUpkeep, "beginning"},
		{state.StepDraw, "beginning"},
		{state.StepMain1, "main1"},
		{state.StepBeginCombat, "combat"},
		{state.StepDeclareAttackers, "combat"},
		{state.StepDeclareBlockers, "combat"},
		{state.StepCombatDamage, "combat"},
		{state.StepEndCombat, "combat"},
		{state.StepMain2, "main2"},
		{state.StepEnd, "ending"},
		{state.StepCleanup, "ending"},
		{state.Step(200), ""},
	} {
		g.Step = tc.step
		if got := Project(g, flatChars{g}, 0, nil).Phase; got != tc.want {
			t.Errorf("phaseOf(%v) = %q, want %q", tc.step, got, tc.want)
		}
	}
}

// boostedChars adds counters, standing in for the engine's layer system.
type boostedChars struct{ flatChars }

func (c boostedChars) Power(id state.ObjID) int32 {
	return c.flatChars.Power(id) + c.g.Obj(id).Counter("P1P1")
}
func (c boostedChars) Toughness(id state.ObjID) int32 {
	return c.flatChars.Toughness(id) + c.g.Obj(id).Counter("P1P1")
}

// TestRedactStripsSecretPayloadsFromOtherSeats is the brief's own test,
// updated for RedactEvents' new (g, evs, viewer) signature (review C-1) and
// strengthened per the coordinator's fix-round item 1: the Shuffle event now
// carries Amount/Counter/Pairs too, so the "keep ONLY the shape" allowlist
// (rule 1) is proven to zero every payload field it names, not merely the
// three the brief's own snippet happened to touch (IDs/Obj/Text).
func TestRedactStripsSecretPayloadsFromOtherSeats(t *testing.T) {
	g := fourSeatBoard(t)
	evs := []events.Event{
		{Seq: 0, Kind: events.Shuffle, Player: 1, From: state.ZLibrary, To: state.ZLibrary,
			Step: state.StepUpkeep, Obj: 4, Amount: 7, Counter: "R", Text: "secret",
			IDs: []state.ObjID{4, 5, 6}, Pairs: [][2]state.ObjID{{4, 5}}, Secret: true},
		{Seq: 1, Kind: events.Draw, Player: 1, Obj: 4, Secret: true},
		{Seq: 2, Kind: events.Damage, Player: 1, Amount: 3},
	}
	own := RedactEvents(g, evs, 1)
	if len(own[0].IDs) != 3 || own[1].Obj != 4 {
		t.Fatal("the owning seat lost its own secret payloads")
	}
	if own[0].Amount != 7 || own[0].Counter != "R" || len(own[0].Pairs) != 1 {
		t.Fatalf("the owning seat lost its own Amount/Counter/Pairs: %+v", own[0])
	}

	other := RedactEvents(g, evs, 0)
	if other[0].IDs != nil || other[1].Obj != 0 {
		t.Fatalf("secret payload leaked to another seat: %+v", other[:2])
	}
	if other[0].Obj != 0 || other[0].Amount != 0 || other[0].Counter != "" ||
		other[0].Text != "" || other[0].Pairs != nil {
		t.Fatalf("a Secret event's payload field survived redaction for another seat: %+v", other[0])
	}
	if other[0].Kind != events.Shuffle || other[0].Player != 1 ||
		other[0].From != state.ZLibrary || other[0].To != state.ZLibrary || other[0].Step != state.StepUpkeep {
		t.Fatalf("redaction must keep the event's shape, only drop the payload: %+v", other[0])
	}
	if other[2].Amount != 3 {
		t.Fatal("public events must be untouched")
	}
	// Redaction must not mutate the caller's slice.
	if evs[0].IDs == nil {
		t.Fatal("RedactEvents mutated its input")
	}
}

// ---------------------------------------------------------------------------
// Review finding C-1 / fix-round item 1: RedactEvents is state-aware, not
// merely Secret-flag-aware. Two pre-existing, non-Secret emitters put
// hidden-zone object ids into a payload -- rules/trigger.go's TriggerPush
// (Ctx.Remembered) and effects/cardflow.go's RearrangeTopOfLibrary Note --
// and neither can be fixed by widening Secret+Player at the emitter
// (TriggerPush's Player is the trigger's CONTROLLER, not the remembered
// card's OWNER). These tests build a real board (fourSeatBoard) and
// synthetic events shaped exactly like those two emitters' real output.

// TestRedactEventsStripsTriggerPushRememberingAnotherSeatsHand is the C-1
// leak itself: a "whenever you draw a card" trigger (Mode$ ChangesZone,
// Origin$ Library, Destination$ Hand) remembers the card that was drawn,
// which is often the very card now sitting in ITS OWNER's hand -- a
// different seat from the trigger's controller (Event.Player).
func TestRedactEventsStripsTriggerPushRememberingAnotherSeatsHand(t *testing.T) {
	g := fourSeatBoard(t)
	seat1Hand := g.Zone(state.ZHand, 1)[0]
	seat0Permanent := g.Zone(state.ZBattlefield, 0)[0]
	evs := []events.Event{
		// Player 0 controls the trigger; the remembered card belongs to
		// player 1 -- the exact conflation RedactEvents' doc describes.
		{Seq: 0, Kind: events.TriggerPush, Player: 0, Obj: seat0Permanent,
			IDs: []state.ObjID{seat1Hand}},
	}
	for _, viewer := range []state.PlayerID{0, 200} { // the controller (not the owner), and a spectator
		out := RedactEvents(g, evs, viewer)
		if len(out[0].IDs) != 0 {
			t.Fatalf("viewer %d: TriggerPush still names seat 1's hand card: %v", viewer, out[0].IDs)
		}
		if out[0].Kind != events.TriggerPush || out[0].Player != 0 {
			t.Fatalf("viewer %d: the event's own shape was lost: %+v", viewer, out[0])
		}
	}
	owner := RedactEvents(g, evs, 1)
	if len(owner[0].IDs) != 1 || owner[0].IDs[0] != seat1Hand {
		t.Fatalf("the owner lost their own remembered card: %v", owner[0].IDs)
	}
}

// TestRedactEventsPassesANonSecretNoteThroughUnchanged is Ruling T23-w
// (fix round 2): a Note is the engine's explicit "tell everyone" channel,
// so rule 3 exempts it entirely -- only Secret opts a Note out of that, and
// rule 1 already handles Secret. This is effReveal's own shape (Reveal/
// RevealHand/PeekAndReveal): a non-Secret Note naming cards that are still
// physically sitting in another seat's hand must reach every viewer whole,
// including a spectator, because the whole point of a reveal is that
// everyone now knows what those cards are.
func TestRedactEventsPassesANonSecretNoteThroughUnchanged(t *testing.T) {
	g := fourSeatBoard(t)
	handIDs := append([]state.ObjID(nil), g.Zone(state.ZHand, 1)...)
	evs := []events.Event{
		{Seq: 0, Kind: events.Note, Player: 1, Text: "reveals cards", IDs: handIDs},
	}
	for _, viewer := range []state.PlayerID{0, 200} { // a non-owner, and a spectator
		out := RedactEvents(g, evs, viewer)
		if len(out[0].IDs) != len(handIDs) {
			t.Fatalf("viewer %d: a revealed Note was altered: got %v, want %v", viewer, out[0].IDs, handIDs)
		}
		for i, id := range out[0].IDs {
			if id != handIDs[i] {
				t.Fatalf("viewer %d: a revealed Note's ids changed: got %v, want %v", viewer, out[0].IDs, handIDs)
			}
		}
		if out[0].Text != "reveals cards" || out[0].Player != 1 {
			t.Fatalf("viewer %d: a revealed Note's shape changed: %+v", viewer, out[0])
		}
	}
}

// TestRedactEventsReducesASecretNoteToItsShape is Ruling T23-w's other
// half: effRearrangeTopOfLibrary's private look opts a Note OUT of the
// "public by default" rule by setting Secret, so it is governed entirely
// by rule 1 (the same shape-only reduction any other Secret event gets),
// not by rule 3's Note exemption.
func TestRedactEventsReducesASecretNoteToItsShape(t *testing.T) {
	g := fourSeatBoard(t)
	libIDs := append([]state.ObjID(nil), g.Zone(state.ZLibrary, 1)[:3]...)
	evs := []events.Event{
		{Seq: 0, Kind: events.Note, Player: 1,
			Text: "looks at the top of the library, order unchanged", IDs: libIDs, Secret: true},
	}
	for _, viewer := range []state.PlayerID{0, 200} {
		out := RedactEvents(g, evs, viewer)
		if len(out[0].IDs) != 0 || out[0].Text != "" {
			t.Fatalf("viewer %d: a Secret Note kept its payload: %+v", viewer, out[0])
		}
		if out[0].Kind != events.Note || out[0].Player != 1 || !out[0].Secret {
			t.Fatalf("viewer %d: a Secret Note's own shape was lost: %+v", viewer, out[0])
		}
	}
	owner := RedactEvents(g, evs, 1)
	if len(owner[0].IDs) != 3 {
		t.Fatalf("the owner lost their own library ids: %v", owner[0].IDs)
	}
}

// TestRedactEventsKeepsAPublicOriginMoveVisibleToEveryone is rule (2)'s own
// boundary: a move FROM a public zone (battlefield) stays visible to
// everyone, even though it lands IN a hidden one (hand) -- everybody already
// saw the permanent bounce, so hiding the destination would hide something
// that already happened in full view.
func TestRedactEventsKeepsAPublicOriginMoveVisibleToEveryone(t *testing.T) {
	g := fourSeatBoard(t)
	bf := g.Zone(state.ZBattlefield, 1)[0]
	evs := []events.Event{
		{Seq: 0, Kind: events.MoveZone, Player: 1, Obj: bf, From: state.ZBattlefield, To: state.ZHand},
	}
	for _, viewer := range []state.PlayerID{0, 200} {
		out := RedactEvents(g, evs, viewer)
		if out[0].Obj != bf {
			t.Fatalf("viewer %d: a move FROM a public zone was hidden: %+v", viewer, out[0])
		}
	}
}

// TestRedactEventsWithNilGameAppliesOnlyTheSecretRule is the documented
// degrade: with no game state to check rules (2)/(3) against, only rule (1)
// (Secret && Player != viewer) applies, and everything else passes through
// unaltered rather than panicking.
func TestRedactEventsWithNilGameAppliesOnlyTheSecretRule(t *testing.T) {
	evs := []events.Event{
		{Seq: 0, Kind: events.Shuffle, Player: 1, IDs: []state.ObjID{1, 2, 3}, Secret: true},
		{Seq: 1, Kind: events.TriggerPush, Player: 0, IDs: []state.ObjID{99}},
	}
	out := RedactEvents(nil, evs, 0)
	if len(out[0].IDs) != 0 {
		t.Fatalf("Secret event not redacted with a nil game: %+v", out[0])
	}
	if len(out[1].IDs) != 1 || out[1].IDs[0] != 99 {
		t.Fatalf("non-Secret event was altered with a nil game (want unchanged, no rule 2/3 to check): %+v", out[1])
	}
}

// TestRedactEventsDoesNotAliasTheEngineLog is the final whole-branch
// review's Important finding I2: RedactEvents' own doc comment promises "the
// input slice, and every event in it, is never mutated", but before this fix
// three branches appended the loop's e (a struct copy whose IDs/Pairs slice
// HEADERS still pointed at the caller's backing arrays) unchanged -- the
// owner's own Secret event, the g == nil degrade, and (via the Note case's
// intentional no-op and the zone-move case only ever touching Obj) the final
// catch-all append. Reviewer-measured: redacting a real game's log for one
// seat returned events whose IDs aliased the engine's own logged Shuffle;
// mutating the client's copy permanently desynced Log.Head() (the rolling
// chain, computed once) from Log.HeadAt(len(Log.Events)) (recomputed from
// the stored events), breaking replay of that match for good, from a pure
// read path that never should have been able to touch it.
//
// Redacts a real 2-seat engine's whole log for seat 1 -- a real seat, not a
// spectator, so it exercises rule 1's OWNER branch on seat 1's own Shuffle
// (the exact shape the reviewer measured) as well as the non-owner and
// default branches on seat 0's events in the same pass -- mutates every
// IDs/Pairs slice the redacted copy holds, and checks the live engine's log
// both internally (Head still equals a fresh HeadAt) and externally
// (replay.Replay, a wholly separate reconstruction from cfg+Log, still
// verifies with no error).
func TestRedactEventsDoesNotAliasTheEngineLog(t *testing.T) {
	cfg := rules.Config{Seed: 21, Names: []string{"alice", "bob"},
		Decks: [][]*cards.Card{r3Filler(t, 40), r3Filler(t, 40)}}
	e := rules.New(cfg)
	e.Advance()

	beforeHead := e.L.Head()
	if want := e.L.HeadAt(len(e.L.Events)); beforeHead != want {
		t.Fatalf("engine log already inconsistent before redaction: Head=%s HeadAt=%s", beforeHead, want)
	}

	redacted := RedactEvents(e.G, e.L.Events, 1)
	mutated := 0
	for i := range redacted {
		for j := range redacted[i].IDs {
			redacted[i].IDs[j] = 999999
			mutated++
		}
		for j := range redacted[i].Pairs {
			redacted[i].Pairs[j][0] = 999999
			redacted[i].Pairs[j][1] = 999999
			mutated++
		}
	}
	if mutated == 0 {
		t.Fatal("no IDs/Pairs in the redacted copy to mutate -- this test would pass vacuously")
	}

	if got := e.L.Head(); got != beforeHead {
		t.Fatalf("mutating the redacted copy changed the engine's own rolling Head: before=%s after=%s",
			beforeHead, got)
	}
	if got := e.L.HeadAt(len(e.L.Events)); got != beforeHead {
		t.Fatalf("mutating the redacted copy desynced Head from a fresh HeadAt: Head=%s HeadAt=%s",
			beforeHead, got)
	}
	if _, err := replay.Replay(e.L, cfg); err != nil {
		t.Fatalf("replay no longer verifies after mutating a redacted copy: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Fix-round item 1's gate: reproduce the reviewer's own probe-C measurement
// (incremental log, real engine) and show the count of hidden-id references
// drop from a genuine leak to zero, with a positive control proving the
// redaction is actually selective rather than merely deleting everything.

// drawWatcherSrc is a permanent whose trigger fires on ANY card drawn while
// it is on the battlefield (Mode$ ChangesZone, Origin$ Library,
// Destination$ Hand, ValidCard$ Card -- no controller restriction: real
// "whenever you draw a card" text). LifeAmount$ 0 keeps the effect an inert
// no-op; only the TriggerPush this trigger emits is under test.
//
// drawSpellSrc is what generates many draws to measure against, cast
// repeatedly rather than relying on the ordinary per-turn draw step. That
// is a deliberate choice, not a simplification: driving this through actual
// per-turn draws (Turn > 1's automatic draw) hits a genuine, pre-existing,
// separate defect in rules/turn.go's priorityRound -- a mandatory trigger
// that fires AND resolves during the draw step itself re-enters
// priorityRound with Passes reset to 0 while Step is still StepDraw, so its
// "draw once per step" guard (the one TestOrderingDecisionInTheDrawStepDoes
// NotDrawTwice exists to pin) fires again, and again, drawing the whole
// library. Confirmed by instrumentation while building this test (one
// draw-step draw ballooned to 20 in a single step, entirely within turn 2,
// with turn 1's caster never receiving turn 3). That defect is in
// rules/turn.go, which is not on this fix round's file list and not
// something this round's brief asked for -- noted in the fix report as a
// new finding, not touched here. Repeated Sorcery casts, entirely within
// Main1, produce the identical event shape (a ChangesZone Library->Hand
// Draw, watched by the same trigger) without ever touching StepDraw, so the
// measurement below is unaffected by it.
const drawWatcherSrc = `Name:DrawWatcher
Types:Enchantment
T:Mode$ ChangesZone | Origin$ Library | Destination$ Hand | ValidCard$ Card | Execute$ TrigNoop | TriggerDescription$ noted a draw
SVar:TrigNoop:DB$ GainLife | LifeAmount$ 0 | Defined$ You
Oracle:x
`

const drawSpellSrc = `Name:DrawSpell
Types:Sorcery
A:SP$ Draw | NumCards$ 1 | Defined$ You | SpellDescription$ Draw a card.
Oracle:x
`

// countPushes counts TriggerPush events in a log.
func countPushes(evs []events.Event) int {
	n := 0
	for _, ev := range evs {
		if ev.Kind == events.TriggerPush {
			n++
		}
	}
	return n
}

// countHiddenRefs is the reviewer's own probe-C measurement, reproduced: it
// walks every event's Obj/IDs/Pairs and counts references to an object
// CURRENTLY (per g) sitting in a hidden zone not owned by exclude.
func countHiddenRefs(g *state.Game, evs []events.Event, exclude state.PlayerID) int {
	n := 0
	count := func(id state.ObjID) {
		if id == 0 {
			return
		}
		o := g.Obj(id)
		if o == nil || !o.Zone.Hidden() || o.Owner == exclude {
			return
		}
		n++
	}
	for _, e := range evs {
		count(e.Obj)
		for _, id := range e.IDs {
			count(id)
		}
		for _, p := range e.Pairs {
			count(p[0])
			count(p[1])
		}
	}
	return n
}

// driveRepeatedCastsWithinMainPhase casts permanent once (whenever it
// becomes castable) and then casts repeatable every single time it is
// offered, falling back to pass otherwise, until at least minPushes
// TriggerPush events have accumulated in the log or the game ends. Driven
// through New/Advance/Pending/Submit only, same as every other
// engine-backed test in this file. See drawSpellSrc's own doc for why this
// stays inside one main phase rather than advancing turns.
func driveRepeatedCastsWithinMainPhase(t *testing.T, e *rules.Engine, permanent, repeatable string, minPushes, iterLimit int) {
	t.Helper()
	for i := 0; i < iterLimit; i++ {
		if e.G.Over || countPushes(e.L.Events) >= minPushes {
			return
		}
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("unexpected pending decision: %+v", d)
		}
		idx := -1
		if permanent != "" {
			for _, o := range d.Options {
				if o.Kind == "cast" && strings.Contains(o.Label, permanent) {
					idx = o.Index
					break
				}
			}
			if idx >= 0 {
				permanent = ""
			}
		}
		if idx < 0 {
			for _, o := range d.Options {
				if o.Kind == "cast" && strings.Contains(o.Label, repeatable) {
					idx = o.Index
					break
				}
			}
		}
		if idx < 0 {
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
					break
				}
			}
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	t.Fatalf("did not accumulate %d TriggerPush events within %d iterations", minPushes, iterLimit)
}

// TestRedactEventsClosesTheTriggerPushLeakMeasured is the fix-round gate: a
// real game accumulates >=20 TriggerPush events (seat 0 casts DrawSpell
// repeatedly, drawSpellSrc's own doc says why this stays within turn 1's
// Main1), each one remembering that draw's card -- the same event shape as
// the reviewer's own measurement (which used the ordinary per-turn draw
// step and found 46 in one short game; this reproduces the property with a
// different, unaffected driver for the reason documented on drawSpellSrc).
// The RAW log contains many references to ids hidden from seat 1; the
// REDACTED-for-seat-1 stream contains zero; a spectator's is the same; and
// the positive control -- redacting the identical log for the actual
// owner, seat 0 -- shows the owner still sees their own remembered ids, so
// the zero above is redaction actually working, not the ids being absent
// to begin with.
func TestRedactEventsClosesTheTriggerPushLeakMeasured(t *testing.T) {
	deck0 := append([]*cards.Card{r3Card(t, drawWatcherSrc)}, nCopies(t, drawSpellSrc, 39)...)
	deck1 := r3Filler(t, 40)
	e := rules.New(rules.Config{Seed: 20, Names: []string{"alice", "bob"},
		Decks: [][]*cards.Card{deck0, deck1}})
	e.Advance()

	driveRepeatedCastsWithinMainPhase(t, e, "DrawWatcher", "DrawSpell", 20, 4000)

	pushes := countPushes(e.L.Events)
	if pushes < 20 {
		t.Fatalf("only %d TriggerPush events accumulated, want at least 20 to reproduce the measurement", pushes)
	}

	rawHidden := countHiddenRefs(e.G, e.L.Events, 1)
	if rawHidden == 0 {
		t.Fatal("test setup: the raw log has no hidden-to-seat-1 references -- this proves nothing")
	}

	for _, viewer := range []state.PlayerID{1, 200} {
		redacted := RedactEvents(e.G, e.L.Events, viewer)
		after := countHiddenRefs(e.G, redacted, viewer)
		if after != 0 {
			t.Fatalf("redacted stream for viewer %d still contains %d hidden-id references (raw was %d), want 0",
				viewer, after, rawHidden)
		}
	}
	t.Logf("TriggerPush events: %d; hidden-to-seat-1 references: raw=%d redacted=0", pushes, rawHidden)

	// Positive control: the ids are not simply gone -- the owner's own
	// redacted stream still names their own remembered cards.
	redactedForOwner := RedactEvents(e.G, e.L.Events, 0)
	ownFound := 0
	for _, ev := range redactedForOwner {
		if ev.Kind != events.TriggerPush {
			continue
		}
		for _, id := range ev.IDs {
			if o := e.G.Obj(id); o != nil && o.Owner == 0 {
				ownFound++
			}
		}
	}
	if ownFound == 0 {
		t.Fatal("positive control failed: the owner's own redacted stream lost every remembered id of their own")
	}
	t.Logf("positive control: seat 0's own redacted stream still names %d of its own remembered ids (of %d pushes)",
		ownFound, pushes)
}

// nCopies is r3Filler's own pattern for an arbitrary source: one parsed
// *cards.Card shared across n deck slots. g.AddObject (called once per
// slot when the deck is dealt) gives each slot its own independent Object
// and ObjID regardless of how many deck entries point at the same *Card.
func nCopies(t *testing.T, src string, n int) []*cards.Card {
	t.Helper()
	c := r3Card(t, src)
	out := make([]*cards.Card, n)
	for i := range out {
		out[i] = c
	}
	return out
}

// ---------------------------------------------------------------------------
// Supplement §4: Draw before Winner.

// TestWinnerDistinguishesAWinFromADrawFromSeatZero is Task 22 finding 5:
// Winner's zero value is PlayerID(0), a real seat, so Over alone cannot say
// whether nobody won (a draw) or seat 0 did. A JSON null must be the only
// way a client can tell "no winner" from "seat 0 won".
func TestWinnerDistinguishesAWinFromADrawFromSeatZero(t *testing.T) {
	drawn := fourSeatBoard(t)
	drawn.Over, drawn.Draw = true, true
	dv := Project(drawn, flatChars{drawn}, 0, nil)
	if dv.Winner != nil {
		t.Fatalf("Winner = %v, want nil for a draw", *dv.Winner)
	}
	blob, err := json.Marshal(dv)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(blob), `"winner":null`) {
		t.Fatalf("drawn game JSON = %s, want \"winner\":null", blob)
	}
	if !strings.Contains(string(blob), `"draw":true`) {
		t.Fatalf("drawn game JSON = %s, want \"draw\":true", blob)
	}

	wonBySeatZero := fourSeatBoard(t)
	wonBySeatZero.Over, wonBySeatZero.Winner = true, 0
	wv := Project(wonBySeatZero, flatChars{wonBySeatZero}, 0, nil)
	if wv.Winner == nil || *wv.Winner != 0 {
		t.Fatal("seat 0 winning must not be confused with Winner's zero value")
	}
	blob2, err := json.Marshal(wv)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(blob2), `"winner":0`) {
		t.Fatalf("won game JSON = %s, want \"winner\":0", blob2)
	}

	wonBySeatTwo := fourSeatBoard(t)
	wonBySeatTwo.Over, wonBySeatTwo.Winner = true, 2
	wv2 := Project(wonBySeatTwo, flatChars{wonBySeatTwo}, 0, nil)
	if wv2.Winner == nil || *wv2.Winner != 2 {
		t.Fatalf("Winner = %v, want 2", wv2.Winner)
	}
}

// ---------------------------------------------------------------------------
// Supplement §5: the decision-marshal test the brief's prose promises but
// its code omits.

func TestDecisionMarshalOmitsServerOnlyFields(t *testing.T) {
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KTarget, Source: 42,
		Options: []decision.Option{{Index: 0, Kind: "permanent", Label: "x", Obj: 7,
			Player: 0, Attacker: 9, AltCostIndex: 3, Mode: "kicked", Amount: 2, Ability: 1}}}
	blob, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	s := string(blob)
	// ResumeKind and ResumeSA are the only fields left server-only: they
	// carry a *cards.SA and are the engine's mid-resolution bookkeeping
	// (how it re-enters a suspended Ask). The five Part-A discriminators and
	// the block option's attacker are on the wire since Task U0/W1.
	for _, forbidden := range []string{"resume_kind", "resume_sa", "ResumeKind", "ResumeSA"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("marshalled decision leaked a server-only field %q: %s", forbidden, s)
		}
	}
	for _, want := range []string{`"source":42`, `"alt_cost_index":3`, `"mode":"kicked"`, `"amount":2`, `"ability":1`, `"attacker":9`, `"player":0`} {
		if !strings.Contains(s, want) {
			t.Fatalf("marshalled decision missing %s: %s", want, s)
		}
	}
}

// TestCardViewAttachedToRoundTripsJSON is the Part-B contract: a permanent
// an Aura or Equipment is attached to carries the attachment on the wire
// (attached_to), so a client can render it beneath the permanent it
// modifies. omitempty keeps the field off an unattached permanent -- 0
// means "not attached", the same zero-value convention Obj uses -- so
// today's payloads are unchanged for it.
func TestCardViewAttachedToRoundTripsJSON(t *testing.T) {
	g := state.NewGame([]string{"alice"})
	c, _ := cards.ParseBytes("a.txt", []byte("Name:Aura\nTypes:Enchantment Aura\nOracle:x\n"))
	c.Link()
	ench := g.AddObject(c, 0)
	bear := g.AddObject(c, 0)
	ench.Zone = state.ZBattlefield
	bear.Zone = state.ZBattlefield
	g.SetZone(state.ZBattlefield, 0, []state.ObjID{ench.ID, bear.ID})
	// Re-fetch: AddObject appends to g.Objs, so the earlier pointer goes
	// stale (backing-array reallocation) once the later object is added.
	g.Obj(ench.ID).AttachedTo = bear.ID

	v := Project(g, flatChars{g}, 0, nil)
	cv := v.Players[0].Battlefield[0]
	if cv.AttachedTo != bear.ID {
		t.Fatalf("AttachedTo = %d, want %d", cv.AttachedTo, bear.ID)
	}
	blob, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if s := string(blob); !strings.Contains(s, `"attached_to":`+strconv.FormatUint(uint64(bear.ID), 10)) {
		t.Fatalf("attached permanent JSON missing attached_to: %s", s)
	}
	var back View
	if err := json.Unmarshal(blob, &back); err != nil {
		t.Fatal(err)
	}
	if back.Players[0].Battlefield[0].AttachedTo != bear.ID {
		t.Fatalf("round-trip lost AttachedTo: got %d, want %d", back.Players[0].Battlefield[0].AttachedTo, bear.ID)
	}

	g.Obj(ench.ID).AttachedTo = 0
	plain := Project(g, flatChars{g}, 0, nil)
	pb, err := json.Marshal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(pb), "attached_to") {
		t.Fatalf("an unattached permanent leaked an attached_to field: %s", pb)
	}
}

// ---------------------------------------------------------------------------
// Supplement §7: totality. Project and RedactEvents must never panic.

func TestProjectAndRedactEventsAreTotal(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(t *testing.T)
	}{
		{"a nil game yields a zero view carrying only Viewer", func(t *testing.T) {
			v := Project(nil, flatChars{}, 2, nil)
			if v.Viewer != 2 {
				t.Fatalf("Viewer = %d, want 2", v.Viewer)
			}
			if v.Players != nil || v.Stack != nil || v.Pending != nil || v.Decision != nil {
				t.Fatalf("nil game produced a non-zero view: %+v", v)
			}
		}},
		{"a nil Chars degrades to zero P/T, no keywords, no pending", func(t *testing.T) {
			g := fourSeatBoard(t)
			v := Project(g, nil, 0, nil)
			cv := v.Players[0].Battlefield[0]
			if cv.Power != 0 || cv.Toughness != 0 {
				t.Fatalf("card view with nil Chars = %+v, want zero P/T", cv)
			}
			if cv.Keywords != nil {
				t.Fatalf("card view with nil Chars has keywords: %v", cv.Keywords)
			}
			if len(v.Pending) != 0 {
				t.Fatalf("Pending = %v, want none with nil Chars", v.Pending)
			}
		}},
		{"an out-of-range viewer is a spectator: hand hidden, pool public", func(t *testing.T) {
			g := fourSeatBoard(t)
			d := &decision.Decision{Player: 0, Kind: decision.KPriority,
				Options: []decision.Option{{Index: 0, Kind: "pass", Label: "Pass"}}}
			v := Project(g, flatChars{g}, state.PlayerID(99), d)
			for _, pv := range v.Players {
				// Hand stays redacted (CR 400.2: hand is a hidden zone), but
				// the pool is public (CR 106.4a/106.4b) and so is present.
				if pv.Hand != nil {
					t.Fatalf("spectator saw seat %d's hand", pv.ID)
				}
				if pv.Pool == nil {
					t.Fatalf("spectator saw no pool for seat %d: pool is public", pv.ID)
				}
				if pv.Pool["G"] != 2 {
					t.Fatalf("spectator reads seat %d pool = %v, want G:2", pv.ID, pv.Pool)
				}
			}
			if v.Decision != nil {
				t.Fatal("spectator received a decision")
			}
		}},
		{"a zone entry whose object no longer exists is skipped, not panicked on", func(t *testing.T) {
			g := fourSeatBoard(t)
			g.SetZone(state.ZBattlefield, 0, append(g.Zone(state.ZBattlefield, 0), state.ObjID(999999)))
			v := Project(g, flatChars{g}, 0, nil)
			if len(v.Players[0].Battlefield) != 1 {
				t.Fatalf("battlefield = %d entries, want the one real object with the dangling id skipped",
					len(v.Players[0].Battlefield))
			}
		}},
		{"an out-of-range Winner projects as nil, not a wraparound seat", func(t *testing.T) {
			g := fourSeatBoard(t)
			g.Over, g.Winner = true, state.PlayerID(200)
			v := Project(g, flatChars{g}, 0, nil)
			if v.Winner != nil {
				t.Fatalf("Winner = %v, want nil for an out-of-range seat", *v.Winner)
			}
		}},
		{"RedactEvents(nil) is an empty, non-nil slice", func(t *testing.T) {
			out := RedactEvents(nil, nil, 0)
			if out == nil {
				t.Fatal("RedactEvents(nil, ...) returned nil, want an empty slice")
			}
			if len(out) != 0 {
				t.Fatalf("len = %d, want 0", len(out))
			}
		}},
	} {
		t.Run(tc.name, tc.run)
	}
}

// ---------------------------------------------------------------------------
// Review finding M-4 / fix-round item 4, Ruling T23-u: wire shape.

// TestEmptyPublicListsMarshalAsEmptyArraysNeverNull covers every public
// list this package produces except Hand/Pool (which have their own test
// below, because THEIR empty shape needs to coexist with a genuinely
// different, privacy-driven null for every other seat) and Targets (which
// TestR3StackViewShowsTextAndTargetsForATargetedSpell already covers, on a
// real spell with no target chosen yet). A brand-new two-seat game with no
// objects at all gives every list here its genuinely empty case.
func TestEmptyPublicListsMarshalAsEmptyArraysNeverNull(t *testing.T) {
	g := state.NewGame([]string{"alice", "bob"})
	v := Project(g, flatChars{g}, 0, nil)
	blob, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	s := string(blob)
	for _, key := range []string{"players", "battlefield", "graveyard", "exile", "stack", "pending", "command", "commanders", "commander_casts"} {
		if strings.Contains(s, `"`+key+`":null`) {
			t.Fatalf("%q marshalled as null, want []: %s", key, s)
		}
	}
	// players is non-empty here (two seats, so this alone would not
	// distinguish [] from omitted); battlefield/graveyard/exile/stack/
	// pending are all genuinely empty in this fixture, which is what
	// actually exercises non-nil-when-empty rather than non-nil-because-
	// populated.
	for _, key := range []string{"battlefield", "graveyard", "exile", "stack", "pending", "command", "commanders", "commander_casts"} {
		if !strings.Contains(s, `"`+key+`":[]`) {
			t.Fatalf("%q did not marshal as an explicit empty array: %s", key, s)
		}
	}
}

// TestEmptyHandMarshalsEmptyArrayForViewerAndNullForOthersButPoolAlwaysObject
// is Ruling T23-u's other half, now narrowed to the hand. Hand cannot use
// plain non-nil-vs-nil the way the public lists above do, because it carries
// a THIRD state a public list never needs to -- "hidden from you entirely" --
// so it drops omitempty instead and relies on null (another seat) vs [] (the
// viewer's own, even when it is genuinely empty) meaning two different things
// on the wire. The pool, in contrast, is public (CR 106.4a/106.4b) and has no
// hidden state: it is always a non-nil object, "{}" when empty, for every
// seat. Neither player has a single card in this fixture, so alice's hand and
// every pool are the interesting "present but empty" cases.
func TestEmptyHandMarshalsEmptyArrayForViewerAndNullForOthersButPoolAlwaysObject(t *testing.T) {
	g := state.NewGame([]string{"alice", "bob"})
	v := Project(g, flatChars{g}, 0, nil)
	blob, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	s := string(blob)
	if !strings.Contains(s, `"hand":[]`) {
		t.Fatalf("the viewer's empty hand did not marshal as [], got: %s", s)
	}
	if !strings.Contains(s, `"hand":null`) {
		t.Fatalf("another seat's hand did not marshal as null, got: %s", s)
	}
	// The pool is public: both seats marshal the SAME empty object, never a
	// null. Checking the null case is gone is the discriminating assertion --
	// "{}:{} present" would pass even for a leaked hidden pool.
	if !strings.Contains(s, `"pool":{}`) {
		t.Fatalf("an empty pool did not marshal as {}, got: %s", s)
	}
	if strings.Contains(s, `"pool":null`) {
		t.Fatalf("a pool marshalled as null; the pool is public (CR 106.4a/106.4b) and always an object: %s", s)
	}
}

// ---------------------------------------------------------------------------
// Supplement §10: no aliasing.

// TestProjectDoesNotAliasEngineOrDecisionState mutates the returned view's
// keyword slice and the attached decision's options, then asserts the
// engine's own card face and the caller's own decision are untouched. A
// Seat (Task 25) holds a View in-process; it must not be able to corrupt
// live state through it.
func TestProjectDoesNotAliasEngineOrDecisionState(t *testing.T) {
	g := fourSeatBoard(t)
	id := g.Zone(state.ZBattlefield, 0)[0]
	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority,
		Options: []decision.Option{{Index: 0, Kind: "pass", Label: "Pass"}}}
	v := Project(g, flatChars{g}, 0, d)

	cv := v.Players[0].Battlefield[0]
	if len(cv.Keywords) == 0 {
		t.Fatal("test setup: expected the bear's Trample keyword in the view")
	}
	cv.Keywords[0] = "MUTATED"
	if v.Decision == nil {
		t.Fatal("test setup: expected the owner's decision to be attached")
	}
	v.Decision.Options[0].Label = "MUTATED"

	if kw := g.Obj(id).Face().Keywords; len(kw) == 0 || kw[0] != "Trample" {
		t.Fatalf("mutating the view's Keywords corrupted the card's own face: %v", kw)
	}
	if d.Options[0].Label != "Pass" {
		t.Fatalf("mutating the view's Decision corrupted the caller's own decision: %q", d.Options[0].Label)
	}
}

// ---------------------------------------------------------------------------
// Supplement §12: the R3 integration test, driven through a real engine.

// r3Card parses and links a standalone card the same two-step way rules'
// own test fixtures do (rules/turn_test.go's card helper) -- view_test.go
// cannot import that unexported helper, so it is reproduced here.
func r3Card(t *testing.T, src string) *cards.Card {
	t.Helper()
	c, diags := cards.ParseBytes("t.txt", []byte(src))
	if len(diags) != 0 {
		t.Fatalf("diags: %v", diags)
	}
	c.Link()
	return c
}

// r3Filler is deck padding: a basic land, never cast in these tests, whose
// only job is to keep each deck at or above the seven-card opening hand so
// genesis (rules.New) does not deck anyone out.
func r3Filler(t *testing.T, n int) []*cards.Card {
	m := r3Card(t, "Name:Filler\nTypes:Basic Land Mountain\nOracle:x\n")
	out := make([]*cards.Card, n)
	for i := range out {
		out[i] = m
	}
	return out
}

const r3GainerSrc = `Name:R3Gainer
Types:Enchantment
T:Mode$ Phase | Phase$ Upkeep | Execute$ TrigGain | TriggerDescription$ gain 5 life
SVar:TrigGain:DB$ GainLife | LifeAmount$ 5 | Defined$ You
Oracle:x
`

const r3DrainerSrc = `Name:R3Drainer
Types:Enchantment
T:Mode$ Phase | Phase$ Upkeep | Execute$ TrigDrain | TriggerDescription$ lose life equal to your life total
SVar:TrigDrain:DB$ LoseLife | LifeAmount$ X | Defined$ You
SVar:X:Count$YourLifeTotal
Oracle:x
`

const r3OptionalSrc = `Name:R3Almsgiver
Types:Enchantment
T:Mode$ Phase | Phase$ Upkeep | OptionalDecider$ You | Execute$ TrigGain | TriggerDescription$ you may gain 4 life
SVar:TrigGain:DB$ GainLife | LifeAmount$ 4 | Defined$ You
Oracle:x
`

// r3ShockSrc is a targeted instant, free to cast: review finding I-1's
// remaining StackView gap. StackView.Targets and the spell-side half of
// Text (SpellDescription$, not a T: line's TriggerDescription$) had no
// assertion anywhere in the suite.
const r3ShockSrc = `Name:R3Shock
Types:Instant
A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 2 | SpellDescription$ Deal 2 damage to any target.
Oracle:x
`

// driveThroughPriority passes priority for whichever seat currently holds
// it, casting each name in toCast (in order) the moment it becomes a legal
// option, until the pending decision stops being a priority decision or the
// game ends. It drives the engine through nothing but New/Advance/Pending/
// Submit -- Task 23 supplement §12 forbids poking e.pendingTriggers.
func driveThroughPriority(t *testing.T, e *rules.Engine, toCast []string) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if e.G.Over {
			t.Fatal("the game ended before reaching the decision under test")
		}
		d := e.Pending()
		if d == nil {
			t.Fatal("nothing pending and the game is not over")
		}
		if d.Kind != decision.KPriority {
			return
		}
		idx := -1
		if len(toCast) > 0 {
			for _, o := range d.Options {
				if o.Kind == "cast" && strings.Contains(o.Label, toCast[0]) {
					idx = o.Index
					break
				}
			}
			if idx >= 0 {
				toCast = toCast[1:]
			}
		}
		if idx < 0 {
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
					break
				}
			}
		}
		if idx < 0 {
			t.Fatalf("no pass option offered: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	t.Fatal("driver did not reach the decision under test in time")
}

func submitChoices(t *testing.T, e *rules.Engine, choices ...int) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
		t.Fatalf("submit %v: %v", choices, err)
	}
}

// TestR3PendingTriggersAndStackAreObservableThroughAFullEngine is the R3
// integration test: two of the controller's own permanents trigger
// simultaneously off a real upkeep, reached by playing the game forward
// through the engine's public surface (never by poking pendingTriggers
// directly, per §12). It asserts the whole observable chain: every seat's
// Pending lists both triggers, only the controller sees the ordering
// Decision (whose Options line up with Pending by position), and once that
// decision is answered the queue empties and the Stack shows both abilities
// in the chosen order.
func TestR3PendingTriggersAndStackAreObservableThroughAFullEngine(t *testing.T) {
	deck0 := append([]*cards.Card{r3Card(t, r3GainerSrc), r3Card(t, r3DrainerSrc)}, r3Filler(t, 5)...)
	deck1 := r3Filler(t, 7)
	e := rules.New(rules.Config{Seed: 7, Names: []string{"alice", "bob"},
		Decks: [][]*cards.Card{deck0, deck1}})
	// New does not itself run the engine forward to the first decision —
	// rules/turn_test.go's own newSeats helper does the same explicit
	// Advance() immediately after New.
	e.Advance()

	driveThroughPriority(t, e, []string{"R3Gainer", "R3Drainer"})

	d := e.Pending()
	if d == nil || d.Kind != decision.KTriggerOrder {
		t.Fatalf("pending = %+v, want a trigger-order decision", d)
	}
	if d.Player != 0 {
		t.Fatalf("the ordering decision is for player %d, want the controller (0)", d.Player)
	}

	var gainerID, drainerID state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if f := e.G.Obj(id).Face(); f != nil {
			switch f.Name {
			case "R3Gainer":
				gainerID = id
			case "R3Drainer":
				drainerID = id
			}
		}
	}
	if gainerID == 0 || drainerID == 0 {
		t.Fatalf("both enchantments must be on the battlefield: gainer=%d drainer=%d", gainerID, drainerID)
	}

	// Review finding I-3's own remedy applied here too: the same
	// collectIDs-based walk the (now-rewritten) leak test uses, run
	// against a projection with Pending, an attached Decision and a
	// non-empty Stack all populated at once -- the richest shape this
	// package produces -- to prove the leak property still holds at
	// engine scale, not merely in the flatChars-driven fixture.
	for _, viewer := range []state.PlayerID{0, 1} {
		other := (viewer + 1) % 2
		found := idsFoundIn(t, Project(e.G, e, viewer, e.Pending()))
		for _, id := range e.G.Zone(state.ZLibrary, other) {
			if found[int(id)] {
				t.Fatalf("viewer %d payload contains seat %d's library object %d", viewer, other, id)
			}
		}
		for _, id := range e.G.Zone(state.ZHand, other) {
			if found[int(id)] {
				t.Fatalf("viewer %d payload contains seat %d's hand object %d", viewer, other, id)
			}
		}
	}

	for viewer := state.PlayerID(0); viewer < 2; viewer++ {
		v := Project(e.G, e, viewer, e.Pending())
		if len(v.Pending) != 2 {
			t.Fatalf("viewer %d: Pending = %d entries, want 2", viewer, len(v.Pending))
		}
		for i, pv := range v.Pending {
			if pv.Controller != 0 {
				t.Errorf("viewer %d: Pending[%d].Controller = %d, want 0 (the actual controller)", viewer, i, pv.Controller)
			}
			if pv.Label == "" {
				t.Errorf("viewer %d: Pending[%d].Label is empty", viewer, i)
			}
		}
		if viewer != 0 {
			if v.Decision != nil {
				t.Fatalf("viewer %d received seat 0's ordering decision", viewer)
			}
			continue
		}
		if v.Decision == nil {
			t.Fatal("the controller did not receive the ordering decision")
		}
		if len(v.Decision.Options) != len(v.Pending) {
			t.Fatalf("Decision has %d options, Pending has %d entries", len(v.Decision.Options), len(v.Pending))
		}
		for i, opt := range v.Decision.Options {
			if opt.Obj != v.Pending[i].Source {
				t.Errorf("Decision.Options[%d].Obj = %d, want Pending[%d].Source = %d",
					i, opt.Obj, i, v.Pending[i].Source)
			}
		}
	}

	// Gainer's option is index 0 (discovery order): choose it first, so it
	// is pushed first, sits at the bottom of the stack, and resolves last.
	submitChoices(t, e, 0, 1)

	if pts := e.PendingTriggers(); len(pts) != 0 {
		t.Fatalf("PendingTriggers = %v, want none after the order was submitted", pts)
	}
	v := Project(e.G, e, 0, e.Pending())
	if len(v.Pending) != 0 {
		t.Fatalf("v.Pending = %v, want none", v.Pending)
	}
	if len(v.Stack) != 2 {
		t.Fatalf("Stack = %d entries, want 2", len(v.Stack))
	}
	want := []struct {
		source state.ObjID
		name   string
		text   string // TriggerDescription$, by Effect pointer identity (supplement §3c)
	}{
		{gainerID, "R3Gainer", "gain 5 life"},
		{drainerID, "R3Drainer", "lose life equal to your life total"},
	}
	for i, w := range want {
		sv := v.Stack[i]
		// Both are TriggerPush objects (gainer/drainer are triggered
		// abilities the engine pushed via the ordering decision above), so
		// Kind is "trigger", not the generic "ability" (Task 4).
		if sv.Kind != "trigger" {
			t.Errorf("Stack[%d].Kind = %q, want \"trigger\"", i, sv.Kind)
		}
		if sv.Source != w.source {
			t.Errorf("Stack[%d].Source = %d, want %d", i, sv.Source, w.source)
		}
		if sv.Name != w.name {
			t.Errorf("Stack[%d].Name = %q, want %q", i, sv.Name, w.name)
		}
		// Review finding I-1: Text (abilityText's Effect-pointer lookup, the
		// most intricate logic in view.go) had no assertion anywhere.
		if sv.Text != w.text {
			t.Errorf("Stack[%d].Text = %q, want %q", i, sv.Text, w.text)
		}
		if sv.Targets == nil {
			t.Errorf("Stack[%d].Targets is nil, want a non-nil empty slice (Ruling T23-u)", i)
		}
	}
}

// TestR3StackTriggerReportsOptionalityAtResolution is Ruling VW-1's first
// half: an optional triggered ability on the stack, awaiting its
// resolution-time yes/no (CR 603.5), reports Optional and its Decider on the
// STACK entry. The queue is empty at that moment — under CR 603.5 the
// ability was pushed unconditionally and the question is posed as it
// resolves — so a test that looked for the optionality on a Pending entry
// would find nothing.
func TestR3StackTriggerReportsOptionalityAtResolution(t *testing.T) {
	deck0 := append([]*cards.Card{r3Card(t, r3OptionalSrc)}, r3Filler(t, 6)...)
	deck1 := r3Filler(t, 7)
	e := rules.New(rules.Config{Seed: 3, Names: []string{"alice", "bob"},
		Decks: [][]*cards.Card{deck0, deck1}})
	e.Advance()

	driveThroughPriority(t, e, []string{"R3Almsgiver"})

	d := e.Pending()
	if d == nil || d.Kind != decision.KTriggerOptional {
		t.Fatalf("pending = %+v, want an optional-trigger decision", d)
	}
	// CR 603.5: the ability is already on the stack, so the queue is empty
	// at the moment the yes/no is posed.
	if pts := e.PendingTriggers(); len(pts) != 0 {
		t.Fatalf("PendingTriggers = %d entries, want none (the optional ability is on the stack)", len(pts))
	}
	v := Project(e.G, e, 0, d)
	if len(v.Pending) != 0 {
		t.Fatalf("Pending = %d entries, want none", len(v.Pending))
	}
	if len(v.Stack) != 1 {
		t.Fatalf("Stack = %d entries, want the optional trigger", len(v.Stack))
	}
	sv := v.Stack[0]
	if sv.Kind != "trigger" {
		t.Fatalf("Stack[0].Kind = %q, want \"trigger\"", sv.Kind)
	}
	if !sv.Optional {
		t.Fatal("Stack[0].Optional = false, want true")
	}
	if sv.Decider == nil || *sv.Decider != 0 {
		t.Fatalf("Stack[0].Decider = %v, want a pointer to the controller (0)", sv.Decider)
	}
	if v.Decision == nil || v.Decision.Player != 0 {
		t.Fatalf("Decision = %+v, want the yes/no question for player 0", v.Decision)
	}
}

// TestR3QueuedTriggerStillReportsOptionalityOnThePendingEntry is Ruling
// VW-1's second half — the one that keeps the first honest. An optional
// triggered ability that is still genuinely in the trigger queue (here,
// awaiting its CR 603.3b ordering decision alongside its mandatory sibling)
// still reports Optional and its Decider on the PENDING entry. The only
// thing that changed under CR 603.5 is that an optional ability no longer
// waits in the queue for its yes/no (it asks at resolution, on the stack);
// something still queued for another reason keeps reporting its optionality
// where it sits.
func TestR3QueuedTriggerStillReportsOptionalityOnThePendingEntry(t *testing.T) {
	deck0 := append([]*cards.Card{r3Card(t, r3OptionalSrc), r3Card(t, r3DrainerSrc)}, r3Filler(t, 5)...)
	deck1 := r3Filler(t, 7)
	e := rules.New(rules.Config{Seed: 17, Names: []string{"alice", "bob"},
		Decks: [][]*cards.Card{deck0, deck1}})
	e.Advance()

	driveThroughPriority(t, e, []string{"R3Almsgiver", "R3Drainer"})

	d := e.Pending()
	if d == nil || d.Kind != decision.KTriggerOrder {
		t.Fatalf("pending = %+v, want a trigger-order decision", d)
	}
	v := Project(e.G, e, 0, d)
	if len(v.Pending) != 2 {
		t.Fatalf("Pending = %d entries, want both queued triggers", len(v.Pending))
	}
	found := false
	for _, pv := range v.Pending {
		if strings.Contains(pv.Label, "you may gain 4 life") {
			found = true
			if !pv.Optional {
				t.Fatal("queued optional trigger's Pending[].Optional = false, want true")
			}
			if pv.Decider == nil || *pv.Decider != 0 {
				t.Fatalf("queued optional trigger's Pending[].Decider = %v, want the controller (0)", pv.Decider)
			}
		}
	}
	if !found {
		t.Fatal("the optional trigger was not in the pending queue")
	}
}

// TestR3StackViewShowsTextAndTargetsForATargetedSpell is review finding
// I-1's remaining StackView gap: a real cast spell awaiting a chosen
// target, observed mid-decision (R3: castSpell emits PutOnStack before
// askTarget, so the spell is on the stack before it has any target at
// all), then again once the target is chosen.
func TestR3StackViewShowsTextAndTargetsForATargetedSpell(t *testing.T) {
	deck0 := append([]*cards.Card{r3Card(t, r3ShockSrc)}, r3Filler(t, 6)...)
	deck1 := r3Filler(t, 7)
	e := rules.New(rules.Config{Seed: 5, Names: []string{"alice", "bob"},
		Decks: [][]*cards.Card{deck0, deck1}})
	e.Advance()

	driveThroughPriority(t, e, []string{"R3Shock"})

	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want a target decision", d)
	}

	// Before the target is chosen: the spell is already observable (R3),
	// with an empty (non-nil) Targets.
	before := Project(e.G, e, 0, d)
	if len(before.Stack) != 1 {
		t.Fatalf("Stack = %d entries, want the spell awaiting its target", len(before.Stack))
	}
	if before.Stack[0].Targets == nil {
		t.Fatal("Targets is nil before a target is chosen, want a non-nil empty slice (Ruling T23-u)")
	}
	if len(before.Stack[0].Targets) != 0 {
		t.Fatalf("Targets = %+v before any target was chosen, want none yet", before.Stack[0].Targets)
	}

	idx := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no option to target player 1: %+v", d.Options)
	}
	submitChoices(t, e, idx)

	after := Project(e.G, e, 0, e.Pending())
	if len(after.Stack) != 1 {
		t.Fatalf("Stack = %d entries, want the spell still awaiting resolution", len(after.Stack))
	}
	sv := after.Stack[0]
	if sv.Kind != "spell" {
		t.Fatalf("Kind = %q, want \"spell\"", sv.Kind)
	}
	if sv.Text != "Deal 2 damage to any target." {
		t.Fatalf("Text = %q, want the SpellDescription$", sv.Text)
	}
	if len(sv.Targets) != 1 || !sv.Targets[0].IsPlayer || sv.Targets[0].Player != 1 {
		t.Fatalf("Targets = %+v, want a single player-1 target", sv.Targets)
	}
	if sv.Card == nil || sv.Card.Name != "R3Shock" {
		t.Fatalf("Card = %+v, want the spell's own CardView", sv.Card)
	}
}

// TestStackViewSpellTextFallsBackToOracleWithNoSpellDescription is the
// other half of spellText's fallback chain: with no SpellDescription$ at
// all, Text falls back to the face's own Oracle wording. A hand-built
// board and a stack object placed directly, rather than a full engine —
// spellText does not depend on anything an engine would add.
func TestStackViewSpellTextFallsBackToOracleWithNoSpellDescription(t *testing.T) {
	g := state.NewGame([]string{"alice", "bob"})
	c, _ := cards.ParseBytes("t.txt", []byte(
		"Name:PlainBolt\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:Deal 1 damage to any target.\n"))
	c.Link()
	o := g.AddObject(c, 0)
	o.Zone = state.ZStack
	g.SetZone(state.ZStack, 0, []state.ObjID{o.ID})

	v := Project(g, flatChars{g}, 0, nil)
	if len(v.Stack) != 1 {
		t.Fatalf("Stack = %d entries, want 1", len(v.Stack))
	}
	if v.Stack[0].Text != "Deal 1 damage to any target." {
		t.Fatalf("Text = %q, want the Oracle fallback", v.Stack[0].Text)
	}
}
