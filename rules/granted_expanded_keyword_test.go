// End-to-end tests for a layer-6 AddKeyword$ grant of an EXPANDED keyword
// that mints an ACTIVATED ability (CR 613.1f): Saddle (Jandor, Fortuned
// Traveler) and Crew (Kotori, Pilot Prodigy). The grant lands in the derived
// keyword list, but the link-time expansion reads only a face's PRINTED
// keyword list, so before this route existed the granted ability was never
// offered. The granted body shares its builder with the printed expansion
// (cards.GrantedKeywordAbility), so these tests also pin that the granted
// offer is priced, gated and resolved exactly as a printed one.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// grantedKeywordOption finds the granted-keyword option for id (a synthesized
// body anchored on the derived keyword LINE, so Ability < 0 and Keyword !=
// "") and asserts exactly one reason it exists.
func grantedKeywordOption(t *testing.T, e *Engine, _ state.PlayerID, id state.ObjID, wantLine string) decision.Option {
	t.Helper()
	e.pending = nil
	e.priorityRound()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending while searching for the granted-keyword option")
	}
	var got []decision.Option
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == id && o.Keyword != "" {
			got = append(got, o)
		}
	}
	if len(got) != 1 {
		t.Fatalf("granted-keyword options for obj %d = %d %+v, want exactly 1 (%q)", id, len(got), got, wantLine)
	}
	if got[0].Keyword != wantLine {
		t.Fatalf("granted option keyword = %q, want %q", got[0].Keyword, wantLine)
	}
	if got[0].Ability >= 0 {
		t.Fatalf("granted option anchors a printed pile index %d; it must read Ability < 0", got[0].Ability)
	}
	return got[0]
}

// TestGrantedCrewIsOfferedAndCrews is the Crew half: Kotori, Pilot Prodigy
// grants `Crew:2` to a Vehicle that prints NO crew of its own, so the offered
// crew ability can only be the granted one. Activating it with a 2-power
// creature crews the Vehicle (the CR 702.122b animation + the Crew event) --
// proving the granted route reaches the whole printed-crew machinery.
func TestGrantedCrewIsOfferedAndCrews(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	kotori := lookup(t, reg, "Kotori, Pilot Prodigy")
	const vehicle = "Name:Granted Hauler\nManaCost:3\nTypes:Artifact Vehicle\nPT:4/4\nOracle:x\n"
	bears := lookup(t, reg, "Grizzly Bears")
	extras := []*cards.Card{card(t, vehicle), kotori, bears}
	e := corpusEngine(t, reg, extras, nil)
	vid := moveByName(t, e, 0, "Granted Hauler", state.ZBattlefield)
	kotoriID := moveByName(t, e, 0, "Kotori, Pilot Prodigy", state.ZBattlefield)
	bearID := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	for _, id := range []state.ObjID{vid, kotoriID, bearID} {
		e.G.Obj(id).SummonSick = false
	}

	// Preconditions, each its own failure: the Vehicle is on the battlefield,
	// prints no crew of its own, and the Saddle/Crew grant is live in the
	// derived keyword list.
	if o := e.G.Obj(vid); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Vehicle = %+v, want on the battlefield", e.G.Obj(vid))
	}
	if o := e.G.Obj(vid); o.Face().HasKeyword("Crew") {
		t.Fatal("precondition: the Vehicle prints Crew, so the offered ability is not the granted one")
	}
	if len(e.G.Obj(vid).Face().Abilities) != 0 {
		t.Fatal("precondition: the Vehicle's face already carries an ability; the granted offer would be ambiguous")
	}
	if param, ok := e.derivedKeywordParam(vid, "Crew"); !ok || param != "2" {
		t.Fatalf("precondition: derived Crew grant = (%q, %v), want (\"2\", true)", param, ok)
	}
	if e.IsCreature(vid) {
		t.Fatal("precondition: the Vehicle is already a creature; the animation assertion would be vacuous")
	}
	if got := e.Power(bearID); got != 2 {
		t.Fatalf("precondition: Grizzly Bears power = %d, want 2 (the Crew 2 floor)", got)
	}

	opt := grantedKeywordOption(t, e, 0, vid, "Crew:2")
	submitChoices(t, e, opt.Index)

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("pending decision %+v, want the crew tap election (KChoose)", d)
	}
	if d.MinSum != 2 {
		t.Fatalf("granted crew election MinSum = %d, want 2", d.MinSum)
	}
	var bearIdx = -1
	for _, o := range d.Options {
		if o.Obj == bearID {
			bearIdx = o.Index
		}
	}
	if bearIdx < 0 {
		t.Fatalf("granted crew election does not offer Grizzly Bears: %+v", d.Options)
	}
	submitChoices(t, e, bearIdx)
	passUntilStackEmpty(t, e, 20)

	if !e.G.Obj(bearID).Tapped {
		t.Fatal("Grizzly Bears was not tapped as the granted crew cost")
	}
	// CR 702.122b: the Vehicle becomes an artifact creature. The animation is
	// the body's AB$ Animate, so it reverts at end of turn; the assertion is
	// live now, before any end step.
	if !e.IsCreature(vid) {
		t.Fatal("the granted crew ability did not animate its Vehicle (CR 702.122b)")
	}
	// The Crew event pairing the tapped crewer with the Vehicle -- the
	// Creature.CrewedThisTurn provenance -- is emitted off the SA's Keyword$
	// Crew tag, so it proves the granted body, not the printed one, was paid.
	if n := countKind(e.L.Events, events.Crew, bearID); n != 1 {
		t.Fatalf("Crew events for the crewer = %d, want 1; the granted body did not reach the crew provenance", n)
	}
}

// TestGrantedSaddleJandorMintsSaddleAbility is the ticket's headline: Jandor,
// Fortuned Traveler grants `Saddle:2` to a Beast (making it a Mount). Before
// this route existed the grant landed but no Saddle ability was ever offered,
// so the Beast could never be saddled. This activates the GRANTED Saddle and
// proves the Beast becomes saddled.
func TestGrantedSaddleJandorMintsSaddleAbility(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	jandor := lookup(t, reg, "Jandor, Fortuned Traveler")
	const beast = "Name:Granted Beast\nManaCost:1 G\nTypes:Creature Beast\nPT:2/2\nOracle:x\n"
	extras := []*cards.Card{card(t, beast), jandor, lookup(t, reg, "Runeclaw Bear")}
	e := corpusEngine(t, reg, extras, nil)
	beastID := moveByName(t, e, 0, "Granted Beast", state.ZBattlefield)
	jandorID := moveByName(t, e, 0, "Jandor, Fortuned Traveler", state.ZBattlefield)
	payID := moveByName(t, e, 0, "Runeclaw Bear", state.ZBattlefield)
	for _, id := range []state.ObjID{beastID, jandorID, payID} {
		e.G.Obj(id).SummonSick = false
	}

	// Preconditions: the Beast is on the battlefield, prints no Saddle of its
	// own, unsaddled, and the grant is live in the derived keyword list.
	if o := e.G.Obj(beastID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Beast = %+v, want on the battlefield", e.G.Obj(beastID))
	}
	if o := e.G.Obj(beastID); o.Face().HasKeyword("Saddle") {
		t.Fatal("precondition: the Beast prints Saddle, so the offered ability is not the granted one")
	}
	if len(e.G.Obj(beastID).Face().Abilities) != 0 {
		t.Fatal("precondition: the Beast's face already carries an ability; the granted offer would be ambiguous")
	}
	if o := e.G.Obj(beastID); o.SaddledTurn != 0 {
		t.Fatalf("precondition: the Beast starts saddled (SaddledTurn %d)", o.SaddledTurn)
	}
	if param, ok := e.derivedKeywordParam(beastID, "Saddle"); !ok || param != "2" {
		t.Fatalf("precondition: derived Saddle grant = (%q, %v), want (\"2\", true)", param, ok)
	}
	if effects.MatchesObjectCtx(e.G, "Creature.IsSaddled", e.G.Obj(beastID), effects.SpecContext{}) {
		t.Fatal("precondition: the IsSaddled predicate is already true")
	}

	opt := grantedKeywordOption(t, e, 0, beastID, "Saddle:2")
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("pending decision %+v, want the saddle tap election (KChoose)", d)
	}
	if d.MinSum != 2 {
		t.Fatalf("granted saddle election MinSum = %d, want 2", d.MinSum)
	}
	var payIdx = -1
	for _, o := range d.Options {
		if o.Obj == payID {
			payIdx = o.Index
		}
	}
	if payIdx < 0 {
		t.Fatalf("granted saddle election does not offer the paying Bear: %+v", d.Options)
	}
	submitChoices(t, e, payIdx)
	passUntilStackEmpty(t, e, 20)

	if !e.G.Obj(payID).Tapped {
		t.Fatal("the paying creature was not tapped as the granted saddle cost")
	}
	if got := e.G.Obj(beastID).SaddledTurn; got != e.G.Turn {
		t.Fatalf("after the granted Saddle resolves SaddledTurn = %d, want the current turn %d", got, e.G.Turn)
	}
	if !effects.MatchesObjectCtx(e.G, "Creature.IsSaddled", e.G.Obj(beastID), effects.SpecContext{}) {
		t.Fatal("IsSaddled does not match the Beast the GRANTED Saddle ability just saddled")
	}
}

// TestGrantedSaddleIsSorcerySpeed pins CR 702.171a on the GRANTED body: the
// synthesized Saddle carries SorcerySpeed$ True, so it is not offered with a
// spell on the stack (mirroring TestSaddleSorcerySpeedGate for the printed
// body).
func TestGrantedSaddleIsSorcerySpeed(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	jandor := lookup(t, reg, "Jandor, Fortuned Traveler")
	bolt := lookup(t, reg, "Lightning Bolt")
	const beast = "Name:Granted Beast\nManaCost:1 G\nTypes:Creature Beast\nPT:2/2\nOracle:x\n"
	extras := []*cards.Card{card(t, beast), jandor, bolt, lookup(t, reg, "Runeclaw Bear")}
	e := corpusEngine(t, reg, extras, nil)
	beastID := moveByName(t, e, 0, "Granted Beast", state.ZBattlefield)
	jandorID := moveByName(t, e, 0, "Jandor, Fortuned Traveler", state.ZBattlefield)
	payID := moveByName(t, e, 0, "Runeclaw Bear", state.ZBattlefield)
	for _, id := range []state.ObjID{beastID, jandorID, payID} {
		e.G.Obj(id).SummonSick = false
	}
	// Precondition: the ability is offered on the empty-stack main phase, else
	// the withheld assertion below is vacuous.
	if param, ok := e.derivedKeywordParam(beastID, "Saddle"); !ok || param != "2" {
		t.Fatalf("precondition: derived Saddle grant = (%q, %v), want (\"2\", true)", param, ok)
	}
	e.pending = nil
	e.priorityRound()
	if _, offered := grantedKeywordOptionOK(e, beastID, "Saddle:2"); !offered {
		t.Fatal("precondition: the granted Saddle is not offered on the empty-stack main phase")
	}

	boltID := moveByName(t, e, 0, "Lightning Bolt", state.ZHand)
	addMana(t, e, 0, "R")
	idx := -1
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == boltID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Lightning Bolt: %+v", e.Pending().Options)
	}
	submitChoices(t, e, idx)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget && len(d.Options) > 0 {
		submitChoices(t, e, d.Options[0].Index)
	}
	if len(e.G.Stack) == 0 {
		t.Fatal("precondition: Lightning Bolt did not reach the stack")
	}
	if _, offered := grantedKeywordOptionOK(e, beastID, "Saddle:2"); offered {
		t.Fatal("the GRANTED Saddle was offered with a spell on the stack; CR 702.171a says only as a sorcery")
	}
}

// TestGrantedCrewIsOfferedOnceWhenTheFacePrintsTheSameLine pins the dedup: a
// Vehicle that PRINTS K:Crew:2 while Kotori grants the same Crew:2 line is
// offered exactly ONE crew activation -- the printed expansion the pile walk
// offers -- never a synthesized duplicate beside it.
func TestGrantedCrewIsOfferedOnceWhenTheFacePrintsTheSameLine(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	kotori := lookup(t, reg, "Kotori, Pilot Prodigy")
	const vehicle = "Name:Printed Hauler\nManaCost:3\nTypes:Artifact Vehicle\nPT:4/4\nK:Crew:2\nOracle:Crew 2\n"
	bears := lookup(t, reg, "Grizzly Bears")
	e := corpusEngine(t, reg, []*cards.Card{card(t, vehicle), kotori, bears}, nil)
	vid := moveByName(t, e, 0, "Printed Hauler", state.ZBattlefield)
	moveByName(t, e, 0, "Kotori, Pilot Prodigy", state.ZBattlefield)
	moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	// Precondition: the face really prints Crew (else the dedup is vacuous)
	// and the grant really is live.
	if !e.G.Obj(vid).Face().HasKeyword("Crew") {
		t.Fatal("precondition: the fixture must print Crew, else the dedup is vacuous")
	}
	if param, ok := e.derivedKeywordParam(vid, "Crew"); !ok || param != "2" {
		t.Fatalf("precondition: derived Crew grant = (%q, %v), want (\"2\", true)", param, ok)
	}
	var opts []decision.Option
	for _, o := range e.legalActions(0) {
		if o.Kind == "ability" && o.Obj == vid {
			opts = append(opts, o)
		}
	}
	if len(opts) != 1 {
		t.Fatalf("ability options for the printed crew Vehicle = %d, want exactly 1 -- the printed expansion; a synthesized duplicate must not appear beside it", len(opts))
	}
	if opts[0].Keyword != "" || opts[0].Ability < 0 {
		t.Fatalf("the surviving option anchors keyword %q ability %d, want the printed expansion (empty keyword, a pile index)", opts[0].Keyword, opts[0].Ability)
	}
}

// grantedKeywordOptionOK is grantedKeywordOption for a caller that wants the
// offered/withheld answer without a t.Fatal on zero.
func grantedKeywordOptionOK(e *Engine, id state.ObjID, wantLine string) (decision.Option, bool) {
	d := e.Pending()
	if d == nil {
		return decision.Option{}, false
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == id && o.Keyword == wantLine {
			return o, true
		}
	}
	return decision.Option{}, false
}
