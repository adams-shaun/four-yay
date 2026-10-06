package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// namedBoard extends the board fixture with the objects the name-predicate
// family needs: two same-named creatures, a comma-named card, an
// underscore-workaround name, and a two-face (split) card in the library.
func namedBoard(t *testing.T) (*state.Game, map[string]state.ObjID) {
	t.Helper()
	g := state.NewGame([]string{"you", "them"})
	mkIn := func(owner state.PlayerID, zone state.Zone, src string) state.ObjID {
		c, d := cards.ParseBytes("t.txt", []byte(src))
		if len(d) != 0 {
			t.Fatalf("diags: %v", d)
		}
		c.Link()
		for _, f := range c.Faces {
			f.ApplyIntrinsics()
		}
		o := g.AddObject(c, owner)
		o.Zone = zone
		g.SetZone(zone, owner, append(g.Zone(zone, owner), o.ID))
		return o.ID
	}
	split := "Name:Split Left\nManaCost:1 R\nTypes:Instant\nOracle:x\nAlternateMode:Split\n\nALTERNATE\n\nName:Split Right\nManaCost:1 U\nTypes:Instant\nOracle:x\n"
	ids := map[string]state.ObjID{
		"hawk1":   mkIn(0, state.ZLibrary, "Name:Squadron Hawk\nManaCost:1 W\nTypes:Creature Bird\nPT:1/1\nOracle:x\n"),
		"hawk2":   mkIn(0, state.ZBattlefield, "Name:Squadron Hawk\nManaCost:1 W\nTypes:Creature Bird\nPT:1/1\nOracle:x\n"),
		"comma":   mkIn(0, state.ZLibrary, "Name:Calim, Djinn Emperor\nManaCost:3 U U\nTypes:Creature Djinn\nPT:5/5\nOracle:x\n"),
		"under":   mkIn(0, state.ZLibrary, "Name:Aether Burst\nManaCost:3 U\nTypes:Instant\nOracle:x\n"),
		"libleft": mkIn(0, state.ZLibrary, split),
		"bfleft":  mkIn(0, state.ZBattlefield, split),
		"bfhawk":  mkIn(0, state.ZBattlefield, "Name:Squadron Hawk\nManaCost:1 W\nTypes:Creature Bird\nPT:1/1\nOracle:x\n"),
		"bear":    mkIn(0, state.ZBattlefield, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"),
		"giant":   mkIn(1, state.ZBattlefield, "Name:Giant\nManaCost:4 R\nTypes:Creature Giant\nPT:5/5\nOracle:x\n"),
	}
	return g, ids
}

// TestNamedPredicateMatchesPrintedName covers `named<Name>` end to end: the
// exact printed name (spaces included), the ';' comma workaround Forge
// CardProperty applies, and the '_' space workaround. An empty argument
// never matches, and a different name is a miss.
func TestNamedPredicateMatchesPrintedName(t *testing.T) {
	g, id := namedBoard(t)
	cases := []struct {
		spec string
		obj  string
		want bool
	}{
		{"Card.namedSquadron Hawk", "hawk1", true},
		{"Card.namedSquadron Hawk", "hawk2", true},
		{"Card.namedSquadron Hawk", "bear", false},
		{"Creature.namedSquadron Hawk", "hawk1", true},
		{"Card.namedCalim; Djinn Emperor", "comma", true},
		{"Card.namedAether_Burst", "under", true},
		{"Card.named", "bear", false}, // empty argument: never matches
		{"Card.namedGiant", "giant", true},
		{"Card.namedGiant", "bear", false},
	}
	for _, c := range cases {
		if got := MatchesSpec(g, c.spec, id[c.obj], 0); got != c.want {
			t.Errorf("MatchesSpec(%q, %s) = %v, want %v", c.spec, c.obj, got, c.want)
		}
	}
	if got := UnknownPredicates("Card.namedSquadron Hawk"); len(got) != 0 {
		t.Errorf("UnknownPredicates(named spec) = %v, want none", got)
	}
}

// TestNamedPredicateKeepsRawCommaInGrandeurCost is the regression for a real
// corpus spelling that cannot use Forge's usual ';' comma workaround. The
// Korlash ability's Discard spec is passed unchanged to MatchesSpec by the
// cost path, so losing its comma as an OR delimiter made its Grandeur ability
// unpayable even with another Korlash in hand.
func TestNamedPredicateKeepsRawCommaInGrandeurCost(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	korlash, ok := reg.Lookup("Korlash, Heir to Blackblade")
	if !ok {
		t.Fatal("Korlash, Heir to Blackblade missing from corpus")
	}
	const cost = "Discard<1/Card.namedKorlash, Heir to Blackblade>"
	found := false
	for _, sa := range korlash.Faces[0].Abilities {
		if sa.Params["Cost"] == cost {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Korlash's raw Grandeur Cost missing %q", cost)
	}

	g := state.NewGame([]string{"you", "them"})
	o := g.AddObject(korlash, 0)
	o.Zone = state.ZHand
	g.SetZone(state.ZHand, 0, []state.ObjID{o.ID})
	const spec = "Card.namedKorlash, Heir to Blackblade"
	if !MatchesSpec(g, spec, o.ID, 0) {
		t.Errorf("MatchesSpec(%q, Korlash) = false, want true", spec)
	}
	if got := UnknownPredicates(spec); len(got) != 0 {
		t.Errorf("UnknownPredicates(%q) = %v, want none", spec, got)
	}

	// A true alternative delimiter remains an OR: the raw-name fix must not
	// collapse the routine Card.namedX,Card.namedY form into one long name.
	if !MatchesSpec(g, "Card.namedNope,Card.namedKorlash, Heir to Blackblade", o.ID, 0) {
		t.Error("a dotted alternative after a raw-comma name was not split")
	}
}

// TestNotnamedPredicate is the negated sibling. Forge implements no notnamed
// predicate and the corpus carries none (measured); the engine gives the
// token the negation of named rather than the fail-closed unknown, so any
// script that does write it behaves as its shape implies.
func TestNotnamedPredicate(t *testing.T) {
	g, id := namedBoard(t)
	if !MatchesSpec(g, "Card.notnamedSquadron Hawk", id["bear"], 0) {
		t.Error("a Bear is notnamed Squadron Hawk")
	}
	if MatchesSpec(g, "Card.notnamedSquadron Hawk", id["hawk2"], 0) {
		t.Error("a Squadron Hawk matched notnamedSquadron Hawk")
	}
	if !MatchesSpec(g, "Creature.notnamedGiant", id["bear"], 0) {
		t.Error("notnamed under a base failed")
	}
	if MatchesSpec(g, "Card.notnamedGiant", id["giant"], 0) {
		t.Error("the Giant matched notnamedGiant")
	}
	// A bare `notnamed` would be the negation of "has no name" -- always
	// true -- so it stays an unknown predicate and fails closed.
	if MatchesSpec(g, "Card.notnamed", id["giant"], 0) {
		t.Error("a bare notnamed must match nothing")
	}
	if got := UnknownPredicates("Card.notnamed"); len(got) != 1 {
		t.Errorf("UnknownPredicates(bare notnamed) = %v, want the token", got)
	}
	if got := UnknownPredicates("Card.notnamedGiant"); len(got) != 0 {
		t.Errorf("UnknownPredicates = %v, want none", got)
	}
}

// TestNamedSplitCardBothNamesOffBattlefield pins CR 709.4: away from the
// stack and battlefield a split card carries both halves' names, so a
// `named` search in a library finds either half; on the battlefield the
// permanent's current face is the only name.
func TestNamedSplitCardBothNamesOffBattlefield(t *testing.T) {
	g, id := namedBoard(t)
	if !MatchesSpec(g, "Card.namedSplit Left", id["libleft"], 0) {
		t.Error("library split card missed its front name")
	}
	if !MatchesSpec(g, "Card.namedSplit Right", id["libleft"], 0) {
		t.Error("library split card missed its back name (CR 709.4)")
	}
	if !MatchesSpec(g, "Card.namedSplit Left", id["bfleft"], 0) {
		t.Error("battlefield split card missed its current face name")
	}
	if MatchesSpec(g, "Card.namedSplit Right", id["bfleft"], 0) {
		t.Error("untransformed battlefield split card matched its back name")
	}
}

// TestNamedDFCKeepsFrontFaceAwayFromBattlefield uses the real compiled Delver
// card: unlike a split card, a transforming DFC in a library has only its
// front-face characteristics (CR 712).
func TestNamedDFCKeepsFrontFaceAwayFromBattlefield(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	delver, ok := reg.Lookup("Delver of Secrets")
	if !ok || delver.AlternateMode != "DoubleFaced" {
		t.Fatalf("Delver layout = %q, want DoubleFaced", delver.AlternateMode)
	}
	g := state.NewGame([]string{"you", "them"})
	o := g.AddObject(delver, 0)
	g.SetZone(state.ZLibrary, 0, []state.ObjID{o.ID})
	l := events.NewLog(1)

	// Transform the real DFC on the battlefield, then move it out through the
	// event path. Move resets FaceIdx (CR 712.8a), and the predicates must
	// answer from the front face either way: below, a stale transformed
	// FaceIdx is forced back on so the characteristic path off the battlefield
	// is still pinned independently of the reset (CR 712).
	events.Emit(g, l, events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	events.Emit(g, l, events.Event{Kind: events.FlipFace, Obj: o.ID, Amount: 1})
	if !MatchesSpec(g, "Card.namedInsectile Aberration", o.ID, 0) {
		t.Error("transformed battlefield Delver missed its selected face")
	}
	events.Emit(g, l, events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZBattlefield, To: state.ZLibrary})
	if o.FaceIdx != 0 {
		t.Fatalf("FaceIdx after leaving battlefield = %d, want front face (CR 712.8a)", o.FaceIdx)
	}
	if !MatchesSpec(g, "Card.namedDelver of Secrets", o.ID, 0) {
		t.Error("library Delver missed its front face after leaving the battlefield")
	}
	o.SetFaceIdx(1)
	if o.FaceIdx != 1 {
		t.Fatal("precondition: could not force a stale transformed FaceIdx")
	}
	if !MatchesSpec(g, "Card.namedDelver of Secrets", o.ID, 0) {
		t.Error("library Delver missed its front face after transforming")
	}
	if MatchesSpec(g, "Card.namedInsectile Aberration", o.ID, 0) {
		t.Error("library Delver matched its transformed face after transforming")
	}

	// sameName takes a name from its source too. A candidate front-face Delver
	// must still share its source's name after that source transformed and left
	// the battlefield; a stale transformed FaceIdx must not make it source the back name.
	candidate := g.AddObject(delver, 0)
	g.SetZone(state.ZLibrary, 0, append(g.Zone(state.ZLibrary, 0), candidate.ID))
	if !MatchesObjectCtx(g, "Card.sameName", candidate, SpecContext{Source: o.ID}) {
		t.Error("sameName used a moved transformed DFC source's back face")
	}
}

// TestNamedCardUsesNameCharacteristics covers Forge's uppercase NamedCard
// sibling: it compares with the source's chosen name using the same split/DFC
// rules as named<Name>, rather than just comparing the current face string.
func TestNamedCardUsesNameCharacteristics(t *testing.T) {
	g, id := namedBoard(t)
	g.Obj(id["bear"]).ChosenName = "Split Right"
	if !MatchesSpecFrom(g, "Card.NamedCard", id["libleft"], 0, id["bear"]) {
		t.Error("NamedCard missed the second name of a library split card")
	}
	g.Obj(id["bear"]).ChosenName = "Insectile Aberration"
	reg := testutil.CorpusRegistry(t)
	delver, ok := reg.Lookup("Delver of Secrets")
	if !ok {
		t.Fatal("Delver missing from corpus")
	}
	o := g.AddObject(delver, 0)
	o.Zone = state.ZLibrary
	g.SetZone(state.ZLibrary, 0, append(g.Zone(state.ZLibrary, 0), o.ID))
	if MatchesSpecFrom(g, "Card.NamedCard", o.ID, 0, id["bear"]) {
		t.Error("NamedCard matched a DFC's transformed face in the library")
	}
}

// TestSameNamePredicateSourceRelative covers the plain source-relative shape
// (Evil Twin's ValidTgts$ Creature.sameName): candidates share the SOURCE
// card's name, and the conjunction partners still apply.
func TestSameNamePredicateSourceRelative(t *testing.T) {
	g, id := namedBoard(t)
	sc := SpecContext{You: 0, Source: id["hawk2"]}
	if !MatchesObjectCtx(g, "Creature.sameName", g.Obj(id["hawk1"]), sc) {
		t.Error("hawk1 missed sameName against the hawk source")
	}
	if MatchesObjectCtx(g, "Creature.sameName", g.Obj(id["bear"]), sc) {
		t.Error("a Bear matched sameName against a hawk source")
	}
	// Evil Twin's full spec: base + sameName conjunction.
	if !MatchesObjectCtx(g, "Creature.sameName+YouCtrl", g.Obj(id["hawk1"]), SpecContext{You: 0, Source: id["hawk2"]}) {
		t.Error("sameName+YouCtrl missed")
	}
	if MatchesObjectCtx(g, "Creature.sameName+YouCtrl", g.Obj(id["hawk1"]), SpecContext{You: 1, Source: id["hawk2"]}) {
		t.Error("sameName+YouCtrl matched under the wrong You")
	}
	if got := UnknownPredicates("Creature.sameName"); len(got) != 0 {
		t.Errorf("UnknownPredicates = %v, want none", got)
	}
}

// TestSameNameBasePrefixReferents pins the sameName-specific context forms:
// Remembered./Targeted./Triggered. name the card whose name sameName compares
// against; an unbound referent leaves the alternative nothing to match.
func TestSameNameBasePrefixReferents(t *testing.T) {
	g, id := namedBoard(t)
	hawkInLib := id["hawk1"]

	// Remembered.sameName: the referent is the first remembered card, not
	// the ability source. hawk2 (battlefield) is the remembered Hawk; the
	// library Hawk shares ITS name even though Source points at the Bear.
	sc := SpecContext{You: 0, Source: id["bear"], Remembered: []state.Target{{Obj: id["hawk2"]}}}
	if !MatchesObjectCtx(g, "Remembered.sameName", g.Obj(hawkInLib), sc) {
		t.Error("Remembered.sameName missed against the remembered Hawk")
	}
	if MatchesObjectCtx(g, "Remembered.sameName", g.Obj(hawkInLib), SpecContext{You: 0, Source: id["hawk2"], Remembered: []state.Target{{Obj: id["bear"]}}}) {
		t.Error("Remembered fell back to the source card's name")
	}
	if MatchesObjectCtx(g, "Remembered.sameName", g.Obj(hawkInLib), SpecContext{You: 0, Source: id["hawk2"]}) {
		t.Error("unbound Remembered fell back to the source card")
	}

	// Targeted.Permanent+sameName (Bifurcate): the context base degrades to
	// Card and its Permanent auxiliary is checked only for this sameName form.
	sc = SpecContext{You: 0, Source: id["bear"], ResolutionTargets: []state.Target{{Obj: id["hawk2"]}}, Resolving: true}
	if !MatchesObjectCtx(g, "Targeted.Permanent+sameName", g.Obj(id["bfhawk"]), sc) {
		t.Error("the battlefield Hawk missed Targeted.Permanent+sameName")
	}
	if MatchesObjectCtx(g, "Targeted.Permanent+sameName", g.Obj(id["bear"]), sc) {
		t.Error("a non-same-name permanent matched")
	}
	if MatchesObjectCtx(g, "Targeted.Permanent+sameName", g.Obj(id["under"]), sc) {
		t.Error("a library instant matched a Permanent predicate")
	}
	if !MatchesObjectCtx(g, "Targeted.Permanent+sameName", g.Obj(hawkInLib), sc) {
		t.Error("a library Hawk card missed Targeted.Permanent+sameName")
	}

	// Triggered.sameName (Bloodbond March): the triggering card is the
	// referent.
	sc = SpecContext{You: 0, Source: id["bear"],
		TriggerContext: TriggerContext{TriggerCard: id["giant"]}}
	if !MatchesObjectCtx(g, "Triggered.sameName", g.Obj(id["giant"]), sc) {
		t.Error("Triggered.sameName missed against the triggering card")
	}
	if MatchesObjectCtx(g, "Triggered.sameName", g.Obj(hawkInLib), sc) {
		t.Error("Triggered.sameName matched a differently named card")
	}
	if MatchesObjectCtx(g, "Triggered.sameName", g.Obj(id["giant"]), SpecContext{You: 0}) {
		t.Error("an absent TriggerCard bound the referent")
	}

	// The context rewrite is intentionally limited to sameName. An unrelated
	// Remembered.Self filter remains fail-closed rather than gaining new
	// behaviour as a side effect of this task.
	sc = SpecContext{You: 0, Source: id["bear"], Remembered: []state.Target{{Obj: id["giant"]}}}
	if MatchesObjectCtx(g, "Remembered.Self", g.Obj(id["giant"]), sc) {
		t.Error("unrelated Remembered.Self gained name-predicate behaviour")
	}
}

// TestSameNameSplitSourceSharesEitherHalf is the regression for sameName
// reducing its source to one name. Forge Card.sharesNameWith(Card) compares
// both cards' full name sets, and a split card away from the stack has both
// halves' names (CR 709.4), so a library or graveyard split source shares a
// name with a card named only for its right half -- directly and through the
// Remembered./Targeted./Triggered. referent forms.
func TestSameNameSplitSourceSharesEitherHalf(t *testing.T) {
	g, id := namedBoard(t)
	add := func(zone state.Zone, name string) *state.Object {
		c, d := cards.ParseBytes("t.txt", []byte("Name:"+name+"\nManaCost:1 U\nTypes:Instant\nOracle:x\n"))
		if len(d) != 0 {
			t.Fatalf("diags: %v", d)
		}
		c.Link()
		o := g.AddObject(c, 0)
		o.Zone = zone
		g.SetZone(zone, 0, append(g.Zone(zone, 0), o.ID))
		return o
	}
	right := add(state.ZLibrary, "Split Right")
	left := add(state.ZGraveyard, "Split Left")

	// Plain source-relative sameName with a library split source.
	sc := SpecContext{You: 0, Source: id["libleft"]}
	if !MatchesObjectCtx(g, "Card.sameName", right, sc) {
		t.Error("a Split Right card must share a name with a library split source")
	}
	if !MatchesObjectCtx(g, "Card.sameName", left, sc) {
		t.Error("a Split Left card must share a name with a library split source")
	}
	if MatchesObjectCtx(g, "Card.sameName", g.Obj(id["bear"]), sc) {
		t.Error("a Bear shared a name with a split source")
	}

	// The referent forms reach the same comparison.
	sc = SpecContext{You: 0, Source: id["bear"], Remembered: []state.Target{{Obj: id["libleft"]}}}
	if !MatchesObjectCtx(g, "Remembered.sameName", right, sc) {
		t.Error("Remembered.sameName missed the remembered split card's right half")
	}
	sc = SpecContext{You: 0, Source: id["bear"],
		TriggerContext: TriggerContext{TriggerCard: id["libleft"]}}
	if !MatchesObjectCtx(g, "Triggered.sameName", right, sc) {
		t.Error("Triggered.sameName missed the triggering split card's right half")
	}

	// A split source on the battlefield keeps only its selected face, the
	// zone scope sharesName already applies to a candidate.
	sc = SpecContext{You: 0, Source: id["bfleft"]}
	if MatchesObjectCtx(g, "Card.sameName", right, sc) {
		t.Error("a battlefield split source contributed its unselected half's name")
	}
	if !MatchesObjectCtx(g, "Card.sameName", left, sc) {
		t.Error("a battlefield split source lost its selected face's name")
	}
}

// TestNamedPredicateRawCommaInZoneCount pins the raw-comma name grammar on
// both object-filter paths: the ordinary MatchesSpec split, and the zone
// count path (Count$Valid<Zone>) that splits alternatives separately. A
// printed comma in the argument is part of the name, not an OR delimiter.
func TestNamedPredicateRawCommaInZoneCount(t *testing.T) {
	g, id := namedBoard(t)
	const spec = "Card.namedCalim, Djinn Emperor"
	if !MatchesSpec(g, spec, id["comma"], 0) {
		t.Errorf("MatchesSpec(%q) missed the comma-named card", spec)
	}
	if !matchesZoneSpecCtx(g, spec, id["comma"], SpecContext{}, state.ZLibrary) {
		t.Errorf("matchesZoneSpecCtx(%q) missed the comma-named card", spec)
	}
	if matchesZoneSpecCtx(g, spec, id["hawk1"], SpecContext{}, state.ZLibrary) {
		t.Errorf("matchesZoneSpecCtx(%q) matched a Squadron Hawk", spec)
	}
	if got := UnknownPredicates(spec); len(got) != 0 {
		t.Errorf("UnknownPredicates(%q) = %v, want none", spec, got)
	}
}

// TestNamedPredicateSearchStatesQuality guards the CR 701.23 split: a
// `Card.namedSquadron Hawk` search states a quality (fail-to-find allowed),
// while `Card` stays quantity-only.
func TestNamedPredicateSearchStatesQuality(t *testing.T) {
	if !SearchStatesQuality("Card.namedSquadron Hawk") {
		t.Error("a named search must state a quality")
	}
	if SearchStatesQuality("Card") {
		t.Error("a bare Card search is quantity-only")
	}
}
