// End-to-end tests for the layer-6 AddKeyword$ Cycling/TypeCycling grant's
// activation route (CR 613.1f): the granted cycling ability is OFFERED,
// activatable through the ordinary cast flow, and its cost discard carries
// the cycling provenance events.DiscardCostCycling -- so a Mode$ Cycled
// trigger fires. The review round 2 finding this closes: tagging the discard
// (round 1) is not enough when legalActionsPriced never offered a granted
// Cycling ability, so no granted activation could ever produce one.
package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// grantedCyclingOption finds the granted-keyword cycling option for id in
// p's legal actions. ok=false means none was offered.
func grantedCyclingOption(e *Engine, p state.PlayerID, id state.ObjID) (decision.Option, bool) {
	for _, o := range e.legalActions(p) {
		if o.Kind == "ability" && o.Obj == id && o.Keyword != "" {
			return o, true
		}
	}
	return decision.Option{}, false
}

// TestGrantedCyclingActivationFiresCycledTrigger is the end-to-end regression:
// Rhet-Tomb Mystic's layer-6 `AddKeyword$ Cycling:1 U` grant gives a hand
// creature card (which prints NO cycling) a cycling ability; activating it
// discards the card with the cycling provenance and Valiant Rescuer's real
// corpus Cycled trigger fires and draws.
func TestGrantedCyclingActivationFiresCycledTrigger(t *testing.T) {
	t.Parallel()
	mystic := mshCorpusCard(t, "Rhet-Tomb Mystic")
	rescuer := mshCorpusCard(t, "Valiant Rescuer")
	const beast = "Name:Vanilla Beast\nManaCost:1 G\nTypes:Creature Beast\nPT:2/2\nOracle:x\n"
	e := handEngine(t, card(t, beast))
	mysticID := onBoardCard(t, e, 0, mystic)
	rescuerID := onBoardCard(t, e, 0, rescuer)

	beastID := e.G.Zone(state.ZHand, 0)[0]
	// Preconditions, each its own failure:
	//  - the hand card is where the grant (AffectedZone$ Hand) and the
	//    cycling activation (ActivationZone$ Hand) read it;
	//  - it prints no Cycling, so the offered ability can only be the
	//    granted one;
	//  - the grant is live in the derived keyword list.
	if o := e.G.Obj(beastID); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: beast = %+v, want a hand card", o)
	}
	if o := e.G.Obj(beastID); o != nil && o.Face().HasKeyword("Cycling") {
		t.Fatal("precondition: the beast must print no Cycling, else the offered ability is not the granted one")
	}
	if o := e.G.Obj(mysticID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: mystic = %+v, want on the battlefield (the grant's EffectZone)", o)
	}
	if o := e.G.Obj(rescuerID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: rescuer = %+v, want on the battlefield (the Cycled trigger's source)", o)
	}
	if param, ok := e.derivedKeywordParam(beastID, "Cycling"); !ok || param != "1 U" {
		t.Fatalf("precondition: derived cycling grant = (%q, %v), want (\"1 U\", true)", param, ok)
	}

	addMana(t, e, 0, "CCU")
	opt, ok := grantedCyclingOption(e, 0, beastID)
	if !ok {
		t.Fatal("the granted cycling activation was never offered: legalActionsPriced walks printed abilities only, so a granted Cycling can never produce the tagged discard its Cycled trigger reads")
	}
	if opt.Keyword != "Cycling:1 U" {
		t.Fatalf("granted option keyword = %q, want \"Cycling:1 U\"", opt.Keyword)
	}

	e.beginActivation(0, opt)
	submitChoices(t, e, 0) // the cost's Discard<1/CARDNAME>: the beast itself
	if got := e.G.Obj(beastID).Zone; got != state.ZGraveyard {
		t.Fatalf("the granted cycle's discard left the beast in %s, want graveyard", got)
	}
	// The paid cost discard is TAGGED with the cycling cause -- the same
	// provenance a printed K:Cycling activation records, derived from the
	// activation's own body (pcAbility resolves the synthesized SA), never
	// from the moved card's printed face.
	tagged := 0
	for _, ev := range e.L.Events {
		if ev.Obj != beastID || !events.IsDiscardCost(ev) {
			continue
		}
		kw, ok := events.IsCyclingDiscard(ev)
		if !ok || kw != "Cycling" {
			t.Fatalf("the granted activation's cost discard was not tagged as a cycling cost (kw=%q ok=%v)", kw, ok)
		}
		tagged++
	}
	if tagged != 1 {
		t.Fatalf("tagged cycling cost discards = %d, want 1", tagged)
	}
	// The mint: events.KeywordAbilityPush created the ability object from the
	// keyword line, so a replay re-derives the identical body.
	if n := countKind(e.L.Events, events.KeywordAbilityPush, beastID); n != 1 {
		t.Fatalf("KeywordAbilityPush count = %d, want 1", n)
	}
	// Valiant Rescuer ("Whenever you cycle another card for the first time
	// each turn") fired on the granted cycle: the trigger resolves ABOVE the
	// resolving cycling ability (CR 117.5).
	if len(e.G.Stack) != 2 {
		t.Fatalf("stack depth after the granted cycle = %d, want 2 (ability + rescuer trigger); the Cycled trigger did not fire on the granted activation", len(e.G.Stack))
	}
	libTop := e.G.Zone(state.ZLibrary, 0)[0]
	e.resolveTop() // the rescuer trigger
	e.resolveTop() // the granted cycling ability's draw
	if e.G.Obj(libTop).Zone != state.ZHand {
		t.Fatal("the granted cycle did not draw (the rescuer trigger or the cycling body failed to resolve)")
	}
}

// TestGrantedCyclingIsOfferedOnceWhenTheFacePrintsTheSameLine pins the dedup:
// a card that PRINTS K:Cycling:1 U while Rhet-Tomb Mystic grants the same
// line is offered exactly ONE cycling activation -- the printed expansion the
// pile walk offers -- never a synthesized duplicate beside it.
func TestGrantedCyclingIsOfferedOnceWhenTheFacePrintsTheSameLine(t *testing.T) {
	t.Parallel()
	mystic := mshCorpusCard(t, "Rhet-Tomb Mystic")
	const printed = "Name:Printed Cycler\nManaCost:2 G\nTypes:Creature Beast\nPT:2/2\nK:Cycling:1 U\nOracle:x\n"
	e := handEngine(t, card(t, printed))
	onBoardCard(t, e, 0, mystic)
	id := e.G.Zone(state.ZHand, 0)[0]
	if o := e.G.Obj(id); o == nil || !o.Face().HasKeyword("Cycling") {
		t.Fatal("precondition: the fixture must print Cycling, else the dedup is vacuous")
	}
	addMana(t, e, 0, "CCU")
	var opts []decision.Option
	for _, o := range e.legalActions(0) {
		if o.Kind == "ability" && o.Obj == id {
			opts = append(opts, o)
		}
	}
	if len(opts) != 1 {
		t.Fatalf("ability options for the printed cycler = %d, want exactly 1 -- the printed expansion; a synthesized duplicate must not appear beside it", len(opts))
	}
	if opts[0].Keyword != "" || opts[0].Ability < 0 {
		t.Fatalf("the surviving option anchors keyword %q ability %d, want the printed expansion (empty keyword, a pile index)", opts[0].Keyword, opts[0].Ability)
	}
}

// TestGrantedTypeCyclingCrossSeatActivationFiresCycledTrigger is the
// TypeCycling + cross-seat half: Homing Sliver's
// `AddKeyword$ TypeCycling:Sliver:3` grant (Affected$ Sliver, no controller
// qualifier) gives a SLIVER card in EACH player's hand slivercycling -- here
// seat 1's, while seat 0 controls the grantor -- and activating it discards
// with the TypeCycling provenance, firing the sliver's own Cycled trigger.
func TestGrantedTypeCyclingCrossSeatActivationFiresCycledTrigger(t *testing.T) {
	t.Parallel()
	homing := mshCorpusCard(t, "Homing Sliver")
	const sliver = "Name:Test Sliver\nManaCost:1 G\nTypes:Creature Sliver\nPT:1/1\n" +
		"T:Mode$ Cycled | ValidCard$ Card.Self | Execute$ TrigDraw | TriggerDescription$ When you cycle CARDNAME, draw a card.\n" +
		"SVar:TrigDraw:DB$ Draw | NumCards$ 1\nOracle:x\n"
	e := handEngine(t)
	onBoardCard(t, e, 0, homing)
	sliverCard := card(t, sliver)
	o := e.G.AddObject(sliverCard, 1)
	o.Zone = state.ZHand
	sliverID := o.ID
	e.G.SetZone(state.ZHand, 1, []state.ObjID{sliverID})
	// A second Sliver sits BELOW the library top: the slivercycling search
	// needs a findable Sliver in seat 1's library (the fixture deck is
	// Mountains), and the Cycled trigger's draw (which resolves first, CR
	// 117.5) must not take it -- hence not the top card.
	const deepSliver = "Name:Deep Sliver\nManaCost:1 G\nTypes:Creature Sliver\nPT:1/1\nOracle:x\n"
	deep := e.G.AddObject(card(t, deepSliver), 1)
	deep.Zone = state.ZLibrary
	lib := e.G.Zone(state.ZLibrary, 1)
	e.G.SetZone(state.ZLibrary, 1, append([]state.ObjID{lib[0], deep.ID}, lib[1:]...))
	if o := e.G.Obj(sliverID); o == nil || o.Zone != state.ZHand || o.Controller != 1 {
		t.Fatalf("precondition: seat 1's sliver = %+v, want in seat 1's hand", o)
	}
	if sf := e.G.Obj(sliverID).Face(); sf.HasKeyword("TypeCycling") || sf.HasKeyword("Cycling") {
		t.Fatal("precondition: the sliver must print no cycling of its own, else the offered ability is not the granted one")
	}
	if param, ok := e.derivedKeywordParam(sliverID, "TypeCycling"); !ok || param != "Sliver:3" {
		t.Fatalf("precondition: derived slivercycling grant = (%q, %v), want (\"Sliver:3\", true)", param, ok)
	}

	addMana(t, e, 1, "CCC")
	opt, ok := grantedCyclingOption(e, 1, sliverID)
	if !ok {
		t.Fatal("seat 1's granted slivercycling was never offered: the grant (Affected$ Sliver, no controller qualifier) must reach every player's hand")
	}
	if opt.Keyword != "TypeCycling:Sliver:3" {
		t.Fatalf("granted option keyword = %q, want \"TypeCycling:Sliver:3\"", opt.Keyword)
	}

	e.beginActivation(1, opt)
	submitChoices(t, e, 0) // the cost's Discard<1/CARDNAME>: the sliver itself
	if got := e.G.Obj(sliverID).Zone; got != state.ZGraveyard {
		t.Fatalf("the granted slivercycling's discard left the sliver in %s, want graveyard", got)
	}
	for _, ev := range e.L.Events {
		if ev.Obj != sliverID || !events.IsDiscardCost(ev) {
			continue
		}
		kw, ok := events.IsCyclingDiscard(ev)
		if !ok || kw != "TypeCycling" {
			t.Fatalf("the granted slivercycling's cost discard was not tagged TypeCycling (kw=%q ok=%v)", kw, ok)
		}
	}
	if n := countKind(e.L.Events, events.KeywordAbilityPush, sliverID); n != 1 {
		t.Fatalf("KeywordAbilityPush count = %d, want 1", n)
	}
	if len(e.G.Stack) != 2 {
		t.Fatalf("stack depth after the granted slivercycling = %d, want 2 (ability + the sliver's own Cycled trigger)", len(e.G.Stack))
	}
	libTop := e.G.Zone(state.ZLibrary, 1)[0]
	e.resolveTop() // the sliver's Cycled trigger (its draw takes the mountain on top)
	e.resolveTop() // the slivercycling search poses its found-card ask
	if e.G.Obj(libTop).Zone != state.ZHand {
		t.Fatal("the sliver's Cycled trigger did not draw")
	}
	d := e.Pending()
	if d == nil {
		t.Fatal("the granted slivercycling search posed no found-card ask")
	}
	choice := -1
	for i, o := range d.Options {
		if o.Obj == deep.ID {
			choice = i
		}
	}
	if choice < 0 {
		t.Fatalf("the granted slivercycling search did not offer the library Sliver: %+v", d)
	}
	submitChoices(t, e, choice)
	if got := e.G.Obj(deep.ID).Zone; got != state.ZHand {
		t.Fatalf("the granted slivercycling search left the library Sliver in %s, want hand (revealed and put into hand, CR 702.28d)", got)
	}
}

// compile-time shape guards on the synthesizer the route stands on.
func TestGrantedCyclingAbilityShapes(t *testing.T) {
	t.Parallel()
	plain := cards.GrantedCyclingAbility("Cycling:1 U")
	if plain == nil || plain.Kind != "AB" || plain.API != "Draw" || plain.Params["Keyword"] != "Cycling" {
		t.Fatalf("Cycling:1 U synthesized = %+v, want the AB$ Draw cycling body", plain)
	}
	if plain.Params["Cost"] != "1 U Discard<1/CARDNAME>" {
		t.Fatalf("synthesized Cost$ = %q, want \"1 U Discard<1/CARDNAME>\"", plain.Params["Cost"])
	}
	typed := cards.GrantedCyclingAbility("TypeCycling:Sliver:3")
	if typed == nil || typed.API != "ChangeZone" || typed.Params["ChangeType"] != "Sliver" || typed.Params["Keyword"] != "TypeCycling" {
		t.Fatalf("TypeCycling:Sliver:3 synthesized = %+v, want the slivercycling search body", typed)
	}
	if cards.GrantedCyclingAbility("Flying") != nil {
		t.Fatal("a non-cycling keyword line must synthesize nothing (fail closed)")
	}
}

// TestGrantedCyclingTectonicReformationLandGrant is the LAND-card half of the
// layer-6 cycling grant: Tectonic Reformation's
// `Affected$ Land.YouOwn | AffectedZone$ Hand | AddKeyword$ Cycling:R` (no
// EffectZone$, so the source-zone default applies) gives a land card in its
// controller's hand cycling {R} -- a different Affected$ filter and a
// different card type than the creature-grant pin -- and activating it
// discards with the cycling provenance and draws.
func TestGrantedCyclingTectonicReformationLandGrant(t *testing.T) {
	t.Parallel()
	reformation := mshCorpusCard(t, "Tectonic Reformation")
	const land = "Name:Vanilla Land\nTypes:Land\nOracle:x\n"
	e := handEngine(t, card(t, land))
	reformationID := onBoardCard(t, e, 0, reformation)

	landID := e.G.Zone(state.ZHand, 0)[0]
	// Preconditions, each its own failure:
	//  - the hand card is a LAND where the grant (Affected$ Land) and the
	//    cycling activation (ActivationZone$ Hand) read it;
	//  - it prints no Cycling, so the offered ability can only be the
	//    granted one;
	//  - the granter is on the battlefield, where the source-zone default
	//    (no EffectZone$) makes the static live.
	if o := e.G.Obj(landID); o == nil || o.Zone != state.ZHand || !slices.Contains(o.Face().Types, "Land") {
		t.Fatalf("precondition: hand card = %+v, want a land in hand", o)
	}
	if o := e.G.Obj(landID); o != nil && o.Face().HasKeyword("Cycling") {
		t.Fatal("precondition: the land must print no Cycling, else the offered ability is not the granted one")
	}
	if o := e.G.Obj(reformationID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: reformation = %+v, want on the battlefield (the grant's source-zone default)", o)
	}
	if param, ok := e.derivedKeywordParam(landID, "Cycling"); !ok || param != "R" {
		t.Fatalf("precondition: derived cycling grant = (%q, %v), want (\"R\", true)", param, ok)
	}

	addMana(t, e, 0, "CR")
	opt, ok := grantedCyclingOption(e, 0, landID)
	if !ok {
		t.Fatal("the granted land cycling activation was never offered: the Land Affected$ filter or the source-zone default did not reach the hand card")
	}
	if opt.Keyword != "Cycling:R" {
		t.Fatalf("granted option keyword = %q, want \"Cycling:R\"", opt.Keyword)
	}

	e.beginActivation(0, opt)
	submitChoices(t, e, 0) // the cost's Discard<1/CARDNAME>: the land itself
	if got := e.G.Obj(landID).Zone; got != state.ZGraveyard {
		t.Fatalf("the granted land cycle's discard left the land in %s, want graveyard", got)
	}
	tagged := 0
	for _, ev := range e.L.Events {
		if ev.Obj != landID || !events.IsDiscardCost(ev) {
			continue
		}
		kw, ok := events.IsCyclingDiscard(ev)
		if !ok || kw != "Cycling" {
			t.Fatalf("the granted land activation's cost discard was not tagged as a cycling cost (kw=%q ok=%v)", kw, ok)
		}
		tagged++
	}
	if tagged != 1 {
		t.Fatalf("tagged cycling cost discards = %d, want 1", tagged)
	}
	if n := countKind(e.L.Events, events.KeywordAbilityPush, landID); n != 1 {
		t.Fatalf("KeywordAbilityPush count = %d, want 1", n)
	}
	libTop := e.G.Zone(state.ZLibrary, 0)[0]
	e.resolveTop() // the granted cycling ability's draw
	if e.G.Obj(libTop).Zone != state.ZHand {
		t.Fatal("the granted land cycle did not draw")
	}
}

// TestGrantedCyclingJoGrantHistoricGrant is the HISTORIC-card half of the
// layer-6 cycling grant: Jo Grant's
// `Affected$ Card.Historic+YouOwn | EffectZone$ Battlefield | AffectedZone$
// Hand | AddKeyword$ Cycling:2 W` gives a historic card (artifact, legendary
// or Saga -- CR 700.6 / the Historic reminder) in its controller's hand
// cycling {2}{W}, and Jo Grant's own `Mode$ Cycled` trigger puts a +1/+1
// counter on it when the cycle resolves.
func TestGrantedCyclingJoGrantHistoricGrant(t *testing.T) {
	t.Parallel()
	jo := mshCorpusCard(t, "Jo Grant")
	// A legendary artifact is historic on BOTH the artifact and legendary
	// limbs of the filter.
	const relic = "Name:Vanilla Relic\nManaCost:1\nTypes:Legendary Artifact\nOracle:x\n"
	e := handEngine(t, card(t, relic))
	joID := onBoardCard(t, e, 0, jo)

	relicID := e.G.Zone(state.ZHand, 0)[0]
	// Preconditions: a historic hand card that prints no Cycling, and the
	// granter on the battlefield (EffectZone$ Battlefield).
	if o := e.G.Obj(relicID); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: relic = %+v, want a hand card", o)
	}
	if o := e.G.Obj(relicID); o != nil && !slices.Contains(o.Face().Types, "Artifact") {
		t.Fatal("precondition: the fixture must be historic (an artifact), else the grant cannot reach it")
	}
	if o := e.G.Obj(relicID); o != nil && o.Face().HasKeyword("Cycling") {
		t.Fatal("precondition: the relic must print no Cycling, else the offered ability is not the granted one")
	}
	if o := e.G.Obj(joID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: jo = %+v, want on the battlefield (EffectZone$)", o)
	}
	if param, ok := e.derivedKeywordParam(relicID, "Cycling"); !ok || param != "2 W" {
		t.Fatalf("precondition: derived cycling grant = (%q, %v), want (\"2 W\", true)", param, ok)
	}

	addMana(t, e, 0, "CCCW")
	opt, ok := grantedCyclingOption(e, 0, relicID)
	if !ok {
		t.Fatal("the granted historic cycling activation was never offered: the Card.Historic+YouOwn Affected$ filter did not reach the hand card")
	}
	if opt.Keyword != "Cycling:2 W" {
		t.Fatalf("granted option keyword = %q, want \"Cycling:2 W\"", opt.Keyword)
	}

	e.beginActivation(0, opt)
	submitChoices(t, e, 0) // the cost's Discard<1/CARDNAME>: the relic itself
	if got := e.G.Obj(relicID).Zone; got != state.ZGraveyard {
		t.Fatalf("the granted historic cycle's discard left the relic in %s, want graveyard", got)
	}
	for _, ev := range e.L.Events {
		if ev.Obj != relicID || !events.IsDiscardCost(ev) {
			continue
		}
		kw, ok := events.IsCyclingDiscard(ev)
		if !ok || kw != "Cycling" {
			t.Fatalf("the granted historic activation's cost discard was not tagged as a cycling cost (kw=%q ok=%v)", kw, ok)
		}
	}
	if n := countKind(e.L.Events, events.KeywordAbilityPush, relicID); n != 1 {
		t.Fatalf("KeywordAbilityPush count = %d, want 1", n)
	}
	// Jo Grant's own Cycled trigger ("Whenever you cycle a card, put a
	// +1/+1 counter on Jo Grant") fires on the granted cycle and resolves
	// above the cycling ability (CR 117.5).
	if len(e.G.Stack) != 2 {
		t.Fatalf("stack depth after the granted historic cycle = %d, want 2 (ability + Jo Grant's Cycled trigger)", len(e.G.Stack))
	}
	libTop := e.G.Zone(state.ZLibrary, 0)[0]
	e.resolveTop() // Jo Grant's Cycled trigger
	e.resolveTop() // the granted cycling ability's draw
	if e.G.Obj(libTop).Zone != state.ZHand {
		t.Fatal("the granted historic cycle did not draw")
	}
	if c := e.G.Obj(joID).Counter("P1P1"); c != 1 {
		t.Fatalf("Jo Grant's Cycled trigger put %d +1/+1 counters, want 1", c)
	}
}
