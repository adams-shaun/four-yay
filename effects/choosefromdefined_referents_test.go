package effects

// The ChangeZone ChooseFromDefined$ referent spellings beyond the original
// dotted AttachedTo form: each measured non-AttachedTo selector either
// resolves the intended object pool (offered as the pick's options and
// enforced on the answered picks) or stays loud-fail-closed (one Note, an
// empty pool, nothing offered). Every card here is the REAL compiled corpus
// card, so a corpus pin that moves the scripts fails the lookup.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// chooseFromDefinedSA locates the real corpus card's ChangeZone sub-ability
// carrying the named ChooseFromDefined$ spelling: printed face abilities and
// their sub-ability chains first, then every face's SVar bodies compiled
// through cards.ResolveSVar (the same compile the resolving chain uses).
func chooseFromDefinedSA(t *testing.T, cardName, spelling string) *cards.SA {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup(cardName)
	if !ok {
		t.Fatalf("corpus missing %q", cardName)
	}
	for _, face := range card.Faces {
		for _, ab := range face.Abilities {
			if ab.Params["ChooseFromDefined"] == spelling {
				return ab
			}
			for sub := ab.Sub; sub != nil; sub = sub.Sub {
				if sub.Params["ChooseFromDefined"] == spelling {
					return sub
				}
			}
		}
		for name := range face.SVars {
			if sa := cards.ResolveSVar(face.SVars, name); sa != nil &&
				sa.Params["ChooseFromDefined"] == spelling {
				return sa
			}
		}
	}
	t.Fatalf("corpus pin moved: %q no longer carries ChooseFromDefined$ %s", cardName, spelling)
	return nil
}

// isolate detaches a copy's SubAbility chain so a selector test exercises
// the one ChangeZone leg it asserts (the SA body is shared card data; only
// the copy is touched).
func isolate(sa *cards.SA) *cards.SA {
	cp := *sa
	cp.Sub = nil
	return &cp
}

// cfdFixture is one placed object: its inline card text, owner and zone.
type cfdFixture struct {
	text  string
	owner state.PlayerID
	zone  state.Zone
}

// cfdBoard builds a 2-seat ask host and places every fixture in its zone.
// Zones are rebuilt through SetZone so the game's zone slices agree with the
// object fields, and the returned ids follow the fixtures' order.
func cfdBoard(t *testing.T, specs ...cfdFixture) (*askHost, []state.ObjID) {
	t.Helper()
	h := &askHost{}
	h.g = state.NewGame(names(2))
	ids := make([]state.ObjID, 0, len(specs))
	byOwner := map[state.Zone]map[state.PlayerID][]state.ObjID{}
	for _, s := range specs {
		o := h.g.AddObject(mkCard(t, s.text), s.owner)
		o.Zone = s.zone
		ids = append(ids, o.ID)
		if byOwner[s.zone] == nil {
			byOwner[s.zone] = map[state.PlayerID][]state.ObjID{}
		}
		byOwner[s.zone][s.owner] = append(byOwner[s.zone][s.owner], o.ID)
	}
	for z, owners := range byOwner {
		for p, zs := range owners {
			h.g.SetZone(z, p, zs)
		}
	}
	return h, ids
}

// assertOfferSet pins the decision's offered object set exactly (order
// preserved on the first mismatch for the failure message).
func assertOfferSet(t *testing.T, d *decision.Decision, want ...state.ObjID) {
	t.Helper()
	got := make([]state.ObjID, 0, len(d.Options))
	for _, o := range d.Options {
		got = append(got, o.Obj)
	}
	if len(got) != len(want) {
		t.Fatalf("options = %v, want exactly %v", got, want)
	}
	for i, id := range want {
		if got[i] != id {
			t.Fatalf("options = %v, want %v (slot %d differs)", got, want, i)
		}
	}
}

// assertNote pins that one Note naming the selector was emitted.
func assertNote(t *testing.T, h *askHost, raw string) {
	t.Helper()
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "ChooseFromDefined$ "+raw) &&
			strings.Contains(ev.Text, "not resolvable") {
			return
		}
	}
	t.Fatalf("no fail-closed Note for ChooseFromDefined$ %s; log: %+v", raw, h.log)
}

const (
	whiteCreature = "Name:White Runt\nManaCost:W\nTypes:Creature\nPT:1/1\nOracle:x\n"
	blueCreature  = "Name:Blue Drake\nManaCost:1 U\nTypes:Creature\nPT:2/2\nOracle:x\n"
	plainLand     = "Name:Test Plains\nTypes:Land\nOracle:x\n"
)

// TestChangeZoneChooseFromDefinedRememberedColor is Sanar, Innovative
// First-Year's real `ChooseFromDefined$ Remembered.White` leg: of the cards
// the DigUntil revealed (Ctx.Remembered, which stayed in the library), only
// the WHITE one is offered, and the answered pick moves to exile while every
// non-member stays put.
func TestChangeZoneChooseFromDefinedRememberedColor(t *testing.T) {
	sa := isolate(chooseFromDefinedSA(t, "Sanar, Innovative First-Year", "Remembered.White"))
	if sa.API != "ChangeZone" || sa.Params["Origin"] != "Library" || sa.Params["Destination"] != "Exile" {
		t.Fatalf("corpus pin moved: leg is %+v", sa)
	}
	h, ids := cfdBoard(t,
		cfdFixture{"Name:Sanar\nTypes:Creature Goblin\nPT:2/4\nOracle:x\n", 0, state.ZBattlefield},
		cfdFixture{whiteCreature, 0, state.ZLibrary},
		cfdFixture{blueCreature, 0, state.ZLibrary},
		cfdFixture{plainLand, 0, state.ZLibrary},
	)
	sanar, white, blue, land := ids[0], ids[1], ids[2], ids[3]
	// Preconditions: the pool's members really are library cards, the
	// eligible set is nonempty, and the two colors actually differ.
	for _, id := range []state.ObjID{white, blue, land} {
		if o := h.g.Obj(id); o == nil || o.Zone != state.ZLibrary {
			t.Fatalf("precondition failed: %d zone %v, want library", id, h.g.Obj(id))
		}
	}
	if white == blue || h.g.Obj(white).Face().Cmc() == 0 {
		t.Fatal("precondition failed: pool members not distinct real cards")
	}
	c := &Ctx{Source: sanar, Controller: 0,
		Remembered: []state.Target{{Obj: white}, {Obj: blue}}}
	effChangeZone(h, c, sa)
	if h.asked == nil || h.asked.ResumeKind != "search" || h.asked.Player != 0 {
		t.Fatalf("no search ask for the Remembered.White leg: %+v", h.asked)
	}
	assertOfferSet(t, h.asked, white)
	assertOptionsExclude(t, h.asked, blue, land)
	// The answered pick moves only the offered white card to exile.
	c.Search, c.SearchDone, c.LibraryTarget = []state.ObjID{white}, true, 0
	effChangeZone(h, c, sa)
	if got := h.g.Obj(white).Zone; got != state.ZExile {
		t.Fatalf("picked white card zone = %v, want exile", got)
	}
	if got := h.g.Obj(blue).Zone; got != state.ZLibrary {
		t.Fatalf("blue non-member moved to %v, want library", got)
	}
	if got := h.g.Obj(land).Zone; got != state.ZLibrary {
		t.Fatalf("land non-member moved to %v, want library", got)
	}
}

// assertOptionsExclude reports whether the decision's options exclude every
// named id (the boolean is a formality for the caller above).
func assertOptionsExclude(t *testing.T, d *decision.Decision, exclude ...state.ObjID) bool {
	t.Helper()
	seen := map[state.ObjID]bool{}
	for _, o := range d.Options {
		seen[o.Obj] = true
	}
	for _, id := range exclude {
		if seen[id] {
			t.Fatalf("option list must not offer %d; options: %+v", id, d.Options)
			return false
		}
	}
	return true
}

// TestChangeZoneChooseFromDefinedRememberedBareSearchPool pins the bare
// `ChooseFromDefined$ Remembered` spelling (The Celestial Toymaker's
// ExileDown leg): the whole remembered set is the offered pool, ChangeNum$ 3
// of it, and the deeper library distractors are never offered.
func TestChangeZoneChooseFromDefinedRememberedBareSearchPool(t *testing.T) {
	sa := isolate(chooseFromDefinedSA(t, "The Celestial Toymaker", "Remembered"))
	if sa.API != "ChangeZone" || sa.Params["Origin"] != "Library" || sa.Params["Destination"] != "Exile" ||
		sa.Params["ChangeNum"] != "3" {
		t.Fatalf("corpus pin moved: leg is %+v", sa)
	}
	h, ids := cfdBoard(t,
		cfdFixture{"Name:Toymaker\nTypes:Creature Rogue\nPT:2/4\nOracle:x\n", 0, state.ZBattlefield},
		cfdFixture{"Name:Peeked One\nManaCost:1 W\nTypes:Creature\nPT:1/1\nOracle:x\n", 0, state.ZLibrary},
		cfdFixture{"Name:Peeked Two\nManaCost:1 U\nTypes:Creature\nPT:1/1\nOracle:x\n", 0, state.ZLibrary},
		cfdFixture{"Name:Peeked Three\nManaCost:1 B\nTypes:Creature\nPT:1/1\nOracle:x\n", 0, state.ZLibrary},
		cfdFixture{"Name:Deep Distraction\nManaCost:2\nTypes:Creature\nPT:2/2\nOracle:x\n", 0, state.ZLibrary},
	)
	toymaker, p1, p2, p3, deep := ids[0], ids[1], ids[2], ids[3], ids[4]
	for _, id := range []state.ObjID{p1, p2, p3, deep} {
		if o := h.g.Obj(id); o == nil || o.Zone != state.ZLibrary {
			t.Fatalf("precondition failed: %d zone %v, want library", id, h.g.Obj(id))
		}
	}
	if deep == p1 || p1 == p2 {
		t.Fatal("precondition failed: pool and distractor are not distinct cards")
	}
	c := &Ctx{Source: toymaker, Controller: 0,
		Remembered: []state.Target{{Obj: p1}, {Obj: p2}, {Obj: p3}}}
	effChangeZone(h, c, sa)
	if h.asked == nil {
		t.Fatal("no search ask for the bare Remembered leg")
	}
	assertOfferSet(t, h.asked, p1, p2, p3)
	assertOptionsExclude(t, h.asked, deep)
	c.Search, c.SearchDone, c.LibraryTarget = []state.ObjID{p1, p2, p3}, true, 0
	effChangeZone(h, c, sa)
	for _, id := range []state.ObjID{p1, p2, p3} {
		if got := h.g.Obj(id).Zone; got != state.ZExile {
			t.Fatalf("picked %d zone = %v, want exile", id, got)
		}
	}
	if got := h.g.Obj(deep).Zone; got != state.ZLibrary {
		t.Fatalf("distractor moved to %v, want library", got)
	}
}

// TestChangeZoneChooseFromDefinedTopThirdOfLibrary pins Assemble the Team's
// real `ChooseFromDefined$ TopThirdOfLibrary`: of a 7-card library the top
// third rounded up (3 cards) is the offered pool, and a deeper card is never
// offered even though the filter (`ChangeType$ Card`) would admit it.
func TestChangeZoneChooseFromDefinedTopThirdOfLibrary(t *testing.T) {
	sa := chooseFromDefinedSA(t, "Assemble the Team", "TopThirdOfLibrary")
	if sa.API != "ChangeZone" || sa.Params["Origin"] != "Library" || sa.Params["Destination"] != "Hand" ||
		sa.Params["Hidden"] == "True" {
		t.Fatalf("corpus pin moved: leg is %+v", sa)
	}
	h, ids := cfdBoard(t,
		cfdFixture{"Name:Top One\nManaCost:1\nTypes:Creature\nPT:1/1\nOracle:x\n", 0, state.ZLibrary},
		cfdFixture{"Name:Top Two\nManaCost:2\nTypes:Creature\nPT:2/2\nOracle:x\n", 0, state.ZLibrary},
		cfdFixture{"Name:Top Three\nManaCost:3\nTypes:Creature\nPT:3/3\nOracle:x\n", 0, state.ZLibrary},
		cfdFixture{"Name:Deep One\nManaCost:4\nTypes:Creature\nPT:4/4\nOracle:x\n", 0, state.ZLibrary},
		cfdFixture{"Name:Deep Two\nManaCost:5\nTypes:Creature\nPT:5/5\nOracle:x\n", 0, state.ZLibrary},
		cfdFixture{"Name:Deep Three\nManaCost:6\nTypes:Creature\nPT:6/6\nOracle:x\n", 0, state.ZLibrary},
		cfdFixture{"Name:Deep Four\nManaCost:7\nTypes:Creature\nPT:7/7\nOracle:x\n", 0, state.ZLibrary},
	)
	top := ids[:3]
	deep := ids[3:]
	for _, id := range ids {
		if o := h.g.Obj(id); o == nil || o.Zone != state.ZLibrary {
			t.Fatalf("precondition failed: %d zone %v, want library", id, h.g.Obj(id))
		}
	}
	// The zone slice really is the 7-card order the rounding is measured on.
	lib := h.g.Zone(state.ZLibrary, 0)
	if len(lib) != 7 {
		t.Fatalf("precondition failed: library holds %d cards, want 7", len(lib))
	}
	c := &Ctx{Source: top[0], Controller: 0}
	effChangeZone(h, c, sa)
	if h.asked == nil || h.asked.ResumeKind != "search" {
		t.Fatalf("no search ask for the TopThirdOfLibrary leg: %+v", h.asked)
	}
	assertOfferSet(t, h.asked, top...)
	assertOptionsExclude(t, h.asked, deep...)
	// Answer one pick; it moves to hand and nothing else leaves the library.
	c.Search, c.SearchDone, c.LibraryTarget = []state.ObjID{top[1]}, true, 0
	effChangeZone(h, c, sa)
	if got := h.g.Obj(top[1]).Zone; got != state.ZHand {
		t.Fatalf("picked card zone = %v, want hand", got)
	}
	for _, id := range append(append([]state.ObjID(nil), top...), deep...) {
		if id == top[1] {
			continue
		}
		if got := h.g.Obj(id).Zone; got != state.ZLibrary {
			t.Fatalf("unpicked %d zone = %v, want library", id, got)
		}
	}
}

// TestDefinedTopThirdOfLibraryRounding pins the rounding rule directly: top
// third rounded up over 1, 3, 4 and 7-card libraries.
func TestDefinedTopThirdOfLibraryRounding(t *testing.T) {
	for n, want := range map[int]int{1: 1, 3: 1, 4: 2, 7: 3} {
		h, ids := cfdBoard(t, nCards(n)...)
		c := &Ctx{Source: ids[0], Controller: 0}
		ts, ok := knownDefinedTargets(h, c, "TopThirdOfLibrary")
		if !ok {
			t.Fatalf("TopThirdOfLibrary over %d cards: not resolvable", n)
		}
		if len(ts) != want {
			t.Fatalf("TopThirdOfLibrary over %d cards = %d targets, want %d", n, len(ts), want)
		}
		for i, tgt := range ts {
			if tgt.Obj != ids[i] {
				t.Fatalf("TopThirdOfLibrary slot %d = %d, want the zone-order card %d", i, tgt.Obj, ids[i])
			}
		}
	}
}

// nCards builds n distinct library cards.
func nCards(n int) []cfdFixture {
	out := make([]cfdFixture, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, cfdFixture{cfdCreatureText(i+1, i+1), 0, state.ZLibrary})
	}
	return out
}

func cfdCreatureText(cost, power int) string {
	return "Name:Round Card " + string(rune('A'+cost)) + "\nManaCost:" + itoa(cost) +
		"\nTypes:Creature\nPT:" + itoa(power) + "/" + itoa(power) + "\nOracle:x\n"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

// TestChangeZoneChooseFromDefinedTriggeredSources pins Zurgo and Ojutai's
// real `ChooseFromDefined$ TriggeredSources` (the Dragon that dealt the
// combat damage the trigger fired for): of a battlefield holding that Dragon
// and another creature, only the trigger source is offered, and the answered
// pick moves to its owner's hand.
func TestChangeZoneChooseFromDefinedTriggeredSources(t *testing.T) {
	sa := chooseFromDefinedSA(t, "Zurgo and Ojutai", "TriggeredSources")
	if sa.API != "ChangeZone" || sa.Params["Origin"] != "Battlefield" || sa.Params["Destination"] != "Hand" {
		t.Fatalf("corpus pin moved: leg is %+v", sa)
	}
	h, ids := cfdBoard(t,
		cfdFixture{"Name:Zurgo and Ojutai\nTypes:Creature Orc Dragon\nPT:4/4\nOracle:x\n", 0, state.ZBattlefield},
		cfdFixture{"Name:Dmg Dragon\nTypes:Creature Dragon\nPT:4/4\nOracle:x\n", 0, state.ZBattlefield},
		cfdFixture{"Name:Other Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n", 0, state.ZBattlefield},
	)
	zurgo, dragon, bear := ids[0], ids[1], ids[2]
	if o := h.g.Obj(dragon); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition failed: trigger source %v", h.g.Obj(dragon))
	}
	if dragon == bear || bear == zurgo {
		t.Fatal("precondition failed: battlefield members not distinct objects")
	}
	c := &Ctx{Source: zurgo, Controller: 0, TriggerContext: TriggerContext{TriggerSource: dragon}}
	effChangeZone(h, c, sa)
	if h.asked == nil || h.asked.ResumeKind != "hidden_pick" {
		t.Fatalf("no hidden-pick ask for the TriggeredSources leg: %+v", h.asked)
	}
	assertOfferSet(t, h.asked, dragon)
	assertOptionsExclude(t, h.asked, bear)
	c.HiddenPick, c.HiddenPickDone, c.HiddenPickTarget = []state.ObjID{dragon}, true, 0
	effChangeZone(h, c, sa)
	if got := h.g.Obj(dragon).Zone; got != state.ZHand {
		t.Fatalf("picked trigger source zone = %v, want hand", got)
	}
	if got := h.g.Obj(bear).Zone; got != state.ZBattlefield {
		t.Fatalf("unpicked bear zone = %v, want battlefield", got)
	}
}

// TestChangeZoneChooseFromDefinedTargetedCmcLE4 pins Back for Seconds' real
// `ChooseFromDefined$ Targeted.cmcLE4`: of the spell's two targeted
// graveyard creature cards, only the one with mana value 4 or less is
// offered; the expensive target and an untargeted graveyard creature are
// never offered.
func TestChangeZoneChooseFromDefinedTargetedCmcLE4(t *testing.T) {
	sa := isolate(chooseFromDefinedSA(t, "Back for Seconds", "Targeted.cmcLE4"))
	if sa.API != "ChangeZone" || sa.Params["Origin"] != "Graveyard" || sa.Params["Destination"] != "Battlefield" {
		t.Fatalf("corpus pin moved: leg is %+v", sa)
	}
	h, ids := cfdBoard(t,
		cfdFixture{"Name:Back for Seconds\nManaCost:2 B\nTypes:Sorcery\nOracle:x\n", 0, state.ZStack},
		cfdFixture{"Name:Cheep Target\nManaCost:2 B\nTypes:Creature\nPT:3/2\nOracle:x\n", 0, state.ZGraveyard},
		cfdFixture{"Name:Pricey Target\nManaCost:5 B B\nTypes:Creature\nPT:7/7\nOracle:x\n", 0, state.ZGraveyard},
		cfdFixture{"Name:Untargeted Bear\nManaCost:1\nTypes:Creature\nPT:2/2\nOracle:x\n", 0, state.ZGraveyard},
	)
	spell, cheap, pricey, bear := ids[0], ids[1], ids[2], ids[3]
	if cmc := int(h.g.Obj(cheap).Face().Cmc()); cmc > 4 {
		t.Fatalf("precondition failed: cheap target cmc %d, want <= 4", cmc)
	}
	if cmc := int(h.g.Obj(pricey).Face().Cmc()); cmc <= 4 {
		t.Fatalf("precondition failed: pricey target cmc %d, want > 4", cmc)
	}
	for _, id := range []state.ObjID{cheap, pricey, bear} {
		if o := h.g.Obj(id); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("precondition failed: %d zone %v, want graveyard", id, h.g.Obj(id))
		}
	}
	c := &Ctx{Source: spell, Controller: 0, Targets: []state.Target{{Obj: cheap}, {Obj: pricey}}}
	effChangeZone(h, c, sa)
	// The leg's explicit Optional$ marker poses Forge's confirm-before-pick
	// gate first: accept it, then the narrowed hidden-pick ask follows.
	if h.asked == nil || h.asked.ResumeKind != "hidden_pick_confirm" {
		t.Fatalf("first ask = %+v, want the hidden_pick_confirm gate (Optional$ asks before the pick)", h.asked)
	}
	c.HiddenPickConfirm, c.HiddenPickConfirmDone, c.HiddenPickConfirmTarget = "yes", true, 0
	effChangeZone(h, c, sa)
	if h.asked == nil || h.asked.ResumeKind != "hidden_pick" {
		t.Fatalf("no hidden-pick ask for the Targeted.cmcLE4 leg: %+v", h.asked)
	}
	assertOfferSet(t, h.asked, cheap)
	assertOptionsExclude(t, h.asked, pricey, bear)
	c.HiddenPick, c.HiddenPickDone, c.HiddenPickTarget = []state.ObjID{cheap}, true, 0
	effChangeZone(h, c, sa)
	if got := h.g.Obj(cheap).Zone; got != state.ZBattlefield {
		t.Fatalf("picked cheap target zone = %v, want battlefield", got)
	}
	if got := h.g.Obj(pricey).Zone; got != state.ZGraveyard {
		t.Fatalf("pricey target zone = %v, want graveyard", got)
	}
}

// TestChangeZoneChooseFromDefinedExiledWithCreature pins The Grim Captain's
// real `ChooseFromDefined$ ExiledWith.Creature`: of the exile zone, only the
// CREATURE exiled with the resolving source is offered; a noncreature exiled
// with the same source and a creature exiled with another source are not.
func TestChangeZoneChooseFromDefinedExiledWithCreature(t *testing.T) {
	sa := chooseFromDefinedSA(t, "The Grim Captain", "ExiledWith.Creature")
	if sa.API != "ChangeZone" || sa.Params["Origin"] != "Exile" || sa.Params["Destination"] != "Battlefield" {
		t.Fatalf("corpus pin moved: leg is %+v", sa)
	}
	other := cfdFixture{"Name:Other Exiler\nTypes:Creature\nPT:2/2\nOracle:x\n", 0, state.ZBattlefield}
	h, ids := cfdBoard(t,
		cfdFixture{"Name:The Grim Captain\nTypes:Creature Nightmare Pirate\nPT:7/7\nOracle:x\n", 0, state.ZBattlefield},
		cfdFixture{"Name:Exiled Creature\nManaCost:3 G\nTypes:Creature Beast\nPT:4/4\nOracle:x\n", 0, state.ZExile},
		cfdFixture{"Name:Exiled Land\nTypes:Land\nOracle:x\n", 0, state.ZExile},
		other,
		cfdFixture{"Name:Exiled Snake\nManaCost:1 G\nTypes:Creature Snake\nPT:1/1\nOracle:x\n", 0, state.ZExile},
	)
	captain, creature, land, otherExiler, snake := ids[0], ids[1], ids[2], ids[3], ids[4]
	h.g.Obj(creature).ExiledWith = captain
	h.g.Obj(land).ExiledWith = captain
	h.g.Obj(snake).ExiledWith = otherExiler
	for _, id := range []state.ObjID{creature, land, snake} {
		if o := h.g.Obj(id); o == nil || o.Zone != state.ZExile {
			t.Fatalf("precondition failed: %d zone %v, want exile", id, h.g.Obj(id))
		}
	}
	if h.g.Obj(creature).ExiledWith != captain || h.g.Obj(land).ExiledWith != captain ||
		h.g.Obj(snake).ExiledWith != otherExiler {
		t.Fatal("precondition failed: ExiledWith associations not as asserted")
	}
	c := &Ctx{Source: captain, Controller: 0}
	effChangeZone(h, c, sa)
	if h.asked == nil || h.asked.ResumeKind != "hidden_pick" {
		t.Fatalf("no hidden-pick ask for the ExiledWith.Creature leg: %+v", h.asked)
	}
	assertOfferSet(t, h.asked, creature)
	assertOptionsExclude(t, h.asked, land, snake)
	c.HiddenPick, c.HiddenPickDone, c.HiddenPickTarget = []state.ObjID{creature}, true, 0
	effChangeZone(h, c, sa)
	if got := h.g.Obj(creature).Zone; got != state.ZBattlefield {
		t.Fatalf("picked creature zone = %v, want battlefield", got)
	}
	if got := h.g.Obj(land).Zone; got != state.ZExile {
		t.Fatalf("noncreature zone = %v, want exile", got)
	}
}

// TestChangeZoneChooseFromDefinedReplacedCards pins Averna, the Chaos
// Bloom's real `ChooseFromDefined$ ReplacedCards.Land`. With a plural
// replaced-cards batch bound (Ctx.ReplacedCards) the selector resolves to the
// batch's LANDS and only those -- the pick offers exactly them, a nonmember
// land also in exile is never offered, and the answered land moves. With NO
// binding the pool is a known-empty set that fails CLOSED: no ask, nothing
// moved, never the whole origin zone.
func TestChangeZoneChooseFromDefinedReplacedCards(t *testing.T) {
	sa := isolate(chooseFromDefinedSA(t, "Averna, the Chaos Bloom", "ReplacedCards.Land"))
	if sa.API != "ChangeZone" || sa.Params["Origin"] != "Exile" || sa.Params["Destination"] != "Battlefield" {
		t.Fatalf("corpus pin moved: leg is %+v", sa)
	}
	// A nonmember land also sits in exile: the pool, not the zone, must
	// bound the offer.
	h, ids := cfdBoard(t,
		cfdFixture{"Name:Averna\nTypes:Creature Elemental\nPT:4/2\nOracle:x\n", 0, state.ZBattlefield},
		cfdFixture{"Name:Batch Land\nTypes:Land\nOracle:x\n", 0, state.ZExile},
		cfdFixture{"Name:Exiled Beast\nManaCost:2 G\nTypes:Creature Beast\nPT:3/3\nOracle:x\n", 0, state.ZExile},
		cfdFixture{"Name:Nonmember Land\nTypes:Land\nOracle:x\n", 0, state.ZExile},
	)
	aver, land, beast, other := ids[0], ids[1], ids[2], ids[3]
	for _, id := range []state.ObjID{land, beast, other} {
		if o := h.g.Obj(id); o == nil || o.Zone != state.ZExile {
			t.Fatalf("precondition failed: %d zone %v, want exile", id, h.g.Obj(id))
		}
	}
	// The batch is exactly {batch land, exiled beast}; the nonmember land is
	// NOT in it even though it is a land in the same zone.
	c := &Ctx{Source: aver, Controller: 0, ReplacedCards: []state.ObjID{land, beast}}
	effChangeZone(h, c, sa)
	if h.asked == nil || h.asked.ResumeKind != "hidden_pick" {
		t.Fatalf("no hidden-pick ask for the ReplacedCards.Land leg: %+v", h.asked)
	}
	assertOfferSet(t, h.asked, land)
	assertOptionsExclude(t, h.asked, beast, other)
	// Answer the pick: the batch land moves to the battlefield.
	c.HiddenPick, c.HiddenPickDone, c.HiddenPickTarget = []state.ObjID{land}, true, 0
	effChangeZone(h, c, sa)
	if got := h.g.Obj(land).Zone; got != state.ZBattlefield {
		t.Fatalf("picked land zone = %v, want battlefield", got)
	}
	if got := h.g.Obj(other).Zone; got != state.ZExile {
		t.Fatalf("nonmember land zone = %v, want exile (never offered)", got)
	}
	if got := h.g.Obj(beast).Zone; got != state.ZExile {
		t.Fatalf("nonland batch card zone = %v, want exile (not a land)", got)
	}

	// Absent binding: a known-empty pool, no ask, nothing moved -- never a
	// fallback to the whole exile zone.
	h2, ids2 := cfdBoard(t,
		cfdFixture{"Name:Averna\nTypes:Creature Elemental\nPT:4/2\nOracle:x\n", 0, state.ZBattlefield},
		cfdFixture{"Name:Exiled Land\nTypes:Land\nOracle:x\n", 0, state.ZExile},
	)
	aver2, land2 := ids2[0], ids2[1]
	if o := h2.g.Obj(land2); o == nil || o.Zone != state.ZExile {
		t.Fatalf("precondition failed: %d zone %v, want exile", land2, h2.g.Obj(land2))
	}
	c2 := &Ctx{Source: aver2, Controller: 0}
	effChangeZone(h2, c2, sa)
	if h2.asked != nil {
		t.Fatalf("a pick was offered over an unbound ReplacedCards pool: %+v", h2.asked)
	}
	if got := h2.g.Obj(land2).Zone; got != state.ZExile {
		t.Fatalf("unbound land moved to %v, want exile", got)
	}
}
