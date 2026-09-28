package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The Dig variant params (task inbox-paramcensus-dig-variants): the reveal
// family (Reveal$/NoReveal$/ForceRevealToController$), the second
// destination (DestinationZone2$/LibraryPosition2$) and the movement riders
// (Tapped$/SkipReorder$), each pinned end to end through the real engine on
// synthetic fixtures imitating the corpus carriers' scripts (Ad Nauseam,
// Impulse, Ancient Stirrings, Chaos Warp, Matter Reshaper, Through the
// Forest Gate -- the corpus lines themselves are GPL and never committed).
//
// The no-variant byte-identity contract (a Dig without these params emits
// exactly what it did before) is pinned by dig_ask_test.go and the chain
// heads; every fixture here carries the param it exercises.

// The six fixture scripts, each the carrier's shape with Defined$ You and a
// one-mana cost so the tests fund and cast them without tap-to-pay.
const (
	digRevealSrc = "Name:Digr\nManaCost:U\nTypes:Instant\n" +
		"A:SP$ Dig | Defined$ You | DigNum$ 1 | Reveal$ True | ChangeNum$ All | ChangeValid$ Card | DestinationZone$ Hand\n" +
		"Oracle:reveal the top card and put it into your hand\n"
	digNoRevealSrc = "Name:Dign\nManaCost:U\nTypes:Instant\n" +
		"A:SP$ Dig | Defined$ You | DigNum$ 4 | ChangeNum$ 1 | NoReveal$ True | DestinationZone$ Hand\n" +
		"Oracle:look at the top four, put one into your hand\n"
	digForceRevealSrc = "Name:Digf\nManaCost:U\nTypes:Instant\n" +
		"A:SP$ Dig | Defined$ You | DigNum$ 5 | ChangeNum$ 1 | Optional$ True | ForceRevealToController$ True | ChangeValid$ Card.Colorless | DestinationZone$ Hand\n" +
		"Oracle:you may reveal a colorless card and put it into your hand\n"
	digSplitLibrarySrc = "Name:Digs\nManaCost:R\nTypes:Instant\n" +
		"A:SP$ Dig | Defined$ You | DigNum$ 1 | Reveal$ True | DestinationZone$ Battlefield | DestinationZone2$ Library | LibraryPosition2$ 0 | ChangeNum$ All | ChangeValid$ Permanent\n" +
		"Oracle:if it's a permanent card, put it onto the battlefield\n"
	digSplitHandSrc = "Name:Digd\nManaCost:R\nTypes:Instant\n" +
		"A:SP$ Dig | Defined$ You | DigNum$ 1 | Reveal$ True | Optional$ True | ChangeNum$ 1 | ChangeValid$ Permanent.cmcLE3 | DestinationZone$ Battlefield | DestinationZone2$ Hand\n" +
		"Oracle:otherwise, put that card into your hand\n"
	digTappedSrc = "Name:Digt\nManaCost:G\nTypes:Instant\n" +
		"A:SP$ Dig | Defined$ You | DigNum$ 3 | ChangeValid$ Land | DestinationZone$ Battlefield | Tapped$ True | ChangeNum$ Any | SkipReorder$ True\n" +
		"Oracle:put any number of lands onto the battlefield tapped\n"

	// digBear is the split tests' creature card; digBoon the non-permanent
	// the hand split moves.
	digBear = "Name:Digr Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"
	digBoon = "Name:Digr Boon\nManaCost:U\nTypes:Instant\nOracle:x\n"
)

// digReorder pins seat 0's library so the given face names lead it, in
// order, moving any hand-borne copy back to the library first -- the same
// event-path arrangement dig_ask_test.go's digFixture uses (a complete
// LibraryOrder, since Apply's case sets the whole zone).
func digReorder(t *testing.T, e *Engine, names ...string) []state.ObjID {
	t.Helper()
	// Hand-borne copies of the pinned names go back to the library first:
	// the MoveZone appends at the bottom and the LibraryOrder below fixes
	// the final order either way.
	for _, oid := range e.G.Zone(state.ZHand, 0) {
		o := e.G.Obj(oid)
		if o == nil || o.Face() == nil {
			continue
		}
		for _, name := range names {
			if o.Face().Name == name {
				e.emit(events.Event{Kind: events.MoveZone, Obj: oid, From: state.ZHand, To: state.ZLibrary, Player: 0})
				break
			}
		}
	}
	lib := e.G.Zone(state.ZLibrary, 0)
	order := make([]state.ObjID, 0, len(lib))
	used := make(map[state.ObjID]bool, len(names))
	for _, name := range names {
		for _, oid := range lib {
			if used[oid] {
				continue
			}
			o := e.G.Obj(oid)
			if o != nil && o.Face() != nil && o.Face().Name == name {
				order = append(order, oid)
				used[oid] = true
				break
			}
		}
	}
	for _, oid := range lib {
		if !used[oid] {
			order = append(order, oid)
		}
	}
	e.emit(events.Event{Kind: events.LibraryOrder, Player: 0, IDs: order, Secret: true})
	e.pending = nil
	e.priorityRound()
	return order
}

// digCast funds the fixture's single mana and casts it (the fixtures carry
// no target decision). wantAsk selects the flow: an asking shape returns
// the dig KChoose the mid-resolution suspension poses; a no-ask shape
// drains the stack and returns nil.
func digCast(t *testing.T, e *Engine, id state.ObjID, symbols string, wantAsk bool) *decision.Decision {
	t.Helper()
	addMana(t, e, 0, symbols)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("not at priority: %+v", d)
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
	if wantAsk {
		return passUntilNonPriority(t, e, 20)
	}
	passUntilStackEmpty(t, e, 20)
	return nil
}

// digPublicRevealNote returns the log's non-Secret Note whose ids are
// exactly want, or nil.
func digPublicRevealNote(e *Engine, want ...state.ObjID) *events.Event {
	for i := range e.L.Events {
		ev := &e.L.Events[i]
		if ev.Kind != events.Note || ev.Secret || len(ev.IDs) != len(want) {
			continue
		}
		same := true
		for j, id := range want {
			if ev.IDs[j] != id {
				same = false
				break
			}
		}
		if same {
			return ev
		}
	}
	return nil
}

// TestDigRevealRevealsTheWindow is Ad Nauseam's DBDig shape: the dug window
// is revealed to the whole table (a non-Secret Note carrying the window's
// ids) before the Secret move puts the card into the owner's hand.
func TestDigRevealRevealsTheWindow(t *testing.T) {
	t.Parallel()
	e, cfg, id := newFixtureDeck(t, 4111, digRevealSrc)
	libBefore := digReorder(t, e, "Mountain")
	top := libBefore[0]

	if d := digCast(t, e, id, "U", false); d != nil {
		t.Fatalf("ChangeNum$ All over a one-card window must not ask, got %+v", d)
	}
	note := digPublicRevealNote(e, top)
	if note == nil {
		t.Fatal("no public Note revealed the dug window")
	}
	if note.Player != 0 {
		t.Fatalf("reveal Note Player = %d, want the library's owner", note.Player)
	}
	if o := e.G.Obj(top); o == nil || o.Zone != state.ZHand {
		t.Fatalf("revealed card zone = %v, want Hand", o)
	}
	if lib := e.G.Zone(state.ZLibrary, 0); len(lib) != len(libBefore)-1 {
		t.Fatalf("library size %d, want %d", len(lib), len(libBefore)-1)
	}
	replayCheck(t, e, cfg)
}

// TestDigNoRevealKeepsTheWindowPrivate is Impulse's shape: the ask fires
// (four eligible in a four-card window, ChangeNum 1), but nothing is
// revealed -- no non-Secret Note carries library ids anywhere in the log,
// the look stays a Secret owner's Note, and the answered move is Secret.
func TestDigNoRevealKeepsTheWindowPrivate(t *testing.T) {
	t.Parallel()
	e, cfg, id := newFixtureDeck(t, 4112, digNoRevealSrc)

	d := digCast(t, e, id, "U", true)
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "dig" {
		t.Fatalf("expected a pending dig KChoose, got %+v", d)
	}
	if d.Min != 1 || d.Max != 1 {
		t.Fatalf("Min/Max = %d/%d, want 1/1 (a mandatory take of one)", d.Min, d.Max)
	}
	for i := range e.L.Events {
		ev := &e.L.Events[i]
		if ev.Kind == events.Note && !ev.Secret && len(ev.IDs) > 0 {
			t.Fatalf("NoReveal dig emitted a public Note: %+v", ev)
		}
	}
	look := false
	for i := range e.L.Events {
		ev := &e.L.Events[i]
		if ev.Kind == events.Note && ev.Secret && ev.Player == 0 && len(ev.IDs) == 4 {
			look = true
		}
	}
	if !look {
		t.Fatal("the chooser's private look Note is missing")
	}
	picked := d.Options[0].Obj
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(picked); o == nil || o.Zone != state.ZHand {
		t.Fatalf("taken card zone = %v, want Hand", o)
	}
	replayCheck(t, e, cfg)
}

// TestDigForceRevealRevealsTheTakenCard is Ancient Stirrings's shape on the
// ask path: five colorless cards in the window make the optional take a real
// KChoose, and the answered card is revealed publicly (ForceRevealToController$)
// before its Secret move, while the window itself was never revealed and the
// untaken cards go to the library's BOTTOM in their existing relative order
// (the default remainder destination; the drain answers the ordered-bottom
// ask in the offered order).
func TestDigForceRevealRevealsTheTakenCard(t *testing.T) {
	t.Parallel()
	e, cfg, id := newFixtureDeck(t, 4113, digForceRevealSrc)
	libBefore := digReorder(t, e, "Mountain")

	d := digCast(t, e, id, "U", true)
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 5 || d.Options[0].Kind != "dig" {
		t.Fatalf("expected the dig KChoose over the five-card window, got %+v", d)
	}
	if d.Min != 0 || d.Max != 1 {
		t.Fatalf("Min/Max = %d/%d, want 0/1 (Optional take of one)", d.Min, d.Max)
	}
	picked := d.Options[0].Obj
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)

	if note := digPublicRevealNote(e, picked); note == nil {
		t.Fatal("no public Note revealed the taken card (ForceRevealToController$ unread?)")
	}
	// Only the taken card: the window's other four were never revealed.
	if note := digPublicRevealNote(e, libBefore[:5]...); note != nil {
		t.Fatalf("the whole window was revealed, want only the taken card: %+v", note)
	}
	if o := e.G.Obj(picked); o == nil || o.Zone != state.ZHand {
		t.Fatalf("taken card zone = %v, want Hand", o)
	}
	wantRest := libBefore[1:5]
	libAfter := e.G.Zone(state.ZLibrary, 0)
	if len(libAfter) != len(libBefore)-1 {
		t.Fatalf("library size %d, want %d", len(libAfter), len(libBefore)-1)
	}
	base := len(libAfter) - len(wantRest)
	for i, oid := range wantRest {
		if libAfter[base+i] != oid {
			t.Fatalf("library bottom[%d] = %v, want %v (the untaken cards went to the bottom in order)", i, libAfter[base+i], oid)
		}
	}
	replayCheck(t, e, cfg)
}

// TestDigDestinationZone2SplitsThePiles is the two-destination shape on both
// corpus carriers' scripts. Chaos Warp's: the window card matching
// ChangeValid$ goes to DestinationZone$, the rest to DestinationZone2$ -- a
// library at LibraryPosition2$ 0 (top), which is exactly the engine's
// stay-in-place default, so an unmatched card emits no move at all. Matter
// Reshaper's: the unmatched card goes to DestinationZone2$ Hand.
func TestDigDestinationZone2SplitsThePiles(t *testing.T) {
	t.Parallel()
	t.Run("chaos warp: unmatched card stays on the library top", func(t *testing.T) {
		e, cfg, id := newFixtureDeck(t, 4114, digSplitLibrarySrc, digBear)
		libBefore := digReorder(t, e, "Digr Bear", "Mountain")
		bear := libBefore[0]

		if d := digCast(t, e, id, "R", false); d != nil {
			t.Fatalf("one-card window with ChangeNum$ All must not ask, got %+v", d)
		}
		if note := digPublicRevealNote(e, bear); note == nil {
			t.Fatal("no public Note revealed the window card (Reveal$ unread?)")
		}
		if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("matched card zone = %v, want Battlefield", o)
		}
		if o := e.G.Obj(bear); o != nil && o.Tapped {
			t.Fatal("Chaos Warp's battlefield entry must not be tapped")
		}
		// The unmatched case is the NEXT test; here the window held the
		// matched card and the library keeps the rest in order.
		if lib := e.G.Zone(state.ZLibrary, 0); len(lib) != len(libBefore)-1 || lib[0] != libBefore[1] {
			t.Fatalf("library after dig = %v, want %v minus the taken card", lib, libBefore)
		}
		replayCheck(t, e, cfg)
	})

	t.Run("chaos warp: non-permanent card stays revealed on the library top", func(t *testing.T) {
		e, cfg, id := newFixtureDeck(t, 4115, digSplitLibrarySrc, digBoon)
		libBefore := digReorder(t, e, "Digr Boon", "Mountain")
		boon := libBefore[0]

		if d := digCast(t, e, id, "R", false); d != nil {
			t.Fatalf("one-card window with ChangeNum$ All must not ask, got %+v", d)
		}
		if note := digPublicRevealNote(e, boon); note == nil {
			t.Fatal("no public Note revealed the window card")
		}
		if o := e.G.Obj(boon); o == nil || o.Zone != state.ZLibrary {
			t.Fatalf("unmatched card zone = %v, want Library (LibraryPosition2$ 0 = top)", o)
		}
		if lib := e.G.Zone(state.ZLibrary, 0); len(lib) != len(libBefore) || lib[0] != boon {
			t.Fatalf("library after dig = %v, want the unmatched card still on top", lib)
		}
		replayCheck(t, e, cfg)
	})

	t.Run("matter reshaper: unmatched card goes to the hand", func(t *testing.T) {
		e, cfg, id := newFixtureDeck(t, 4116, digSplitHandSrc, digBoon)
		libBefore := digReorder(t, e, "Digr Boon", "Mountain")
		boon := libBefore[0]

		if d := digCast(t, e, id, "R", false); d != nil {
			t.Fatalf("one-card window with no eligible card must not ask, got %+v", d)
		}
		if o := e.G.Obj(boon); o == nil || o.Zone != state.ZHand {
			t.Fatalf("unmatched card zone = %v, want Hand (DestinationZone2$)", o)
		}
		if note := digPublicRevealNote(e, boon); note == nil {
			t.Fatal("no public Note revealed the window card")
		}
		if lib := e.G.Zone(state.ZLibrary, 0); len(lib) != len(libBefore)-1 || lib[0] != libBefore[1] {
			t.Fatalf("library after dig = %v, want the taken card gone", lib)
		}
		replayCheck(t, e, cfg)
	})

	t.Run("matter reshaper: matched card goes to the battlefield", func(t *testing.T) {
		e, cfg, id := newFixtureDeck(t, 4117, digSplitHandSrc, digBear)
		libBefore := digReorder(t, e, "Digr Bear", "Mountain")
		bear := libBefore[0]

		d := digCast(t, e, id, "R", true)
		if d == nil || d.Kind != decision.KChoose || d.Min != 0 || d.Max != 1 {
			t.Fatalf("one-card optional window = %+v, want the 0..1 may ask", d)
		}
		submitChoices(t, e, d.Options[0].Index)
		passUntilStackEmpty(t, e, 20)
		if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("matched card zone = %v, want Battlefield", o)
		}
		replayCheck(t, e, cfg)
	})
}

// TestDigTappedAndSkipReorder is Through the Forest Gate's shape: the
// ChangeNum$ Any take is a real any-number ask (Min 0), every taken land
// enters the battlefield TAPPED (a Tap event right after each MoveZone),
// and the untaken cards stay on top in their existing order (SkipReorder$
// True -- the engine's remainder contract). The answer takes mountains 1
// and 3, proving the answered SUBSET -- not take-all, not take-nothing --
// is what moves.
func TestDigTappedAndSkipReorder(t *testing.T) {
	t.Parallel()
	e, cfg, id := newFixtureDeck(t, 4118, digTappedSrc)
	libBefore := digReorder(t, e, "Mountain")

	d := digCast(t, e, id, "G", true)
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 3 || d.Options[0].Kind != "dig" {
		t.Fatalf("expected the any-number dig KChoose over the three lands, got %+v", d)
	}
	if d.Min != 0 || d.Max != 3 {
		t.Fatalf("Min/Max = %d/%d, want 0/3 (ChangeNum$ Any takes any number)", d.Min, d.Max)
	}
	taken := []state.ObjID{d.Options[0].Obj, d.Options[2].Obj}
	spared := d.Options[1].Obj
	submitChoices(t, e, d.Options[0].Index, d.Options[2].Index)
	passUntilStackEmpty(t, e, 20)

	for _, oid := range taken {
		o := e.G.Obj(oid)
		if o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("taken card %v zone = %v, want Battlefield", oid, o)
			continue
		}
		if !o.Tapped {
			t.Fatalf("card %v entered untapped, want tapped (Tapped$ True)", oid)
		}
	}
	taps := 0
	for i := range e.L.Events {
		ev := &e.L.Events[i]
		if ev.Kind == events.Tap && ev.Text == "entered tapped" {
			taps++
		}
	}
	if taps != 2 {
		t.Fatalf("entered-tapped Tap events = %d, want 2", taps)
	}
	if o := e.G.Obj(spared); o == nil || o.Zone != state.ZLibrary {
		t.Fatalf("untaken card zone = %v, want Library (SkipReorder$ keeps the rest in place)", o)
	}
	libAfter := e.G.Zone(state.ZLibrary, 0)
	if len(libAfter) != len(libBefore)-2 {
		t.Fatalf("library size %d, want %d", len(libAfter), len(libBefore)-2)
	}
	if libAfter[0] != spared {
		t.Fatalf("library[0] = %v, want the spared card %v on top", libAfter[0], spared)
	}
	replayCheck(t, e, cfg)
}
