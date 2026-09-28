package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// addArtifacts puts n synthetic artifacts (each built from src) under
// seat 0 on the battlefield and returns their ids in battlefield order.
func addArtifacts(t *testing.T, e *Engine, n int, src string) []state.ObjID {
	t.Helper()
	var ids []state.ObjID
	for i := 0; i < n; i++ {
		o := e.G.AddObject(card(t, src), 0)
		o.Zone = state.ZBattlefield
		ids = append(ids, o.ID)
	}
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), ids...))
	return ids
}

const plainArtifactSrc = "Name:Gear\nTypes:Artifact\nOracle:x\n"
const manaArtifactSrc = "Name:Mana Gear\nTypes:Artifact\nA:AB$ Mana | Cost$ T | Produced$ R\nOracle:x\n"

// TestImproviseOrganicExtinctionAnnouncesArtifactsAndPays is the corpus pin:
// {8}{W}{W} DestroyAll with Improvise. With eight untapped artifacts and a
// pool holding only the two white pips, the cast is offered, the CR 601.2b
// announcement offers one improvise_generic option per untapped artifact
// (a tapped artifact none), the chosen artifacts are committed and TAPPED
// as the payment, and the pool paid exactly its {W}{W} -- the artifacts
// contributed no mana.
func TestImproviseOrganicExtinctionAnnouncesArtifactsAndPays(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Organic Extinction"))
	ids := addArtifacts(t, e, 8, plainArtifactSrc)
	// A tapped artifact is never an Improvise candidate (CR 702.66a).
	tapped := addArtifacts(t, e, 1, plainArtifactSrc)[0]
	e.G.Obj(tapped).Tapped = true
	spell := e.G.Zone(state.ZHand, 0)[0]
	if !e.HasKeyword(spell, "Improvise") {
		t.Fatal("corpus Organic Extinction lost Improvise before cast")
	}
	// The registration is what unlocks the carriers in the coverage report;
	// the cast path reads the printed keyword directly, so pin it here.
	if !effects.Supported()["kw:Improvise"] {
		t.Fatal("kw:Improvise not registered in effects.Supported()")
	}
	e.G.Players[0].Pool[state.MW] = 2
	castMode(t, e, spell, "")
	d := e.Pending()
	if d == nil || len(d.Options) == 0 {
		t.Fatalf("Improvise did not announce its artifact payment: %+v", d)
	}
	var byID map[state.ObjID]int
	for i, o := range d.Options {
		if o.Kind != "improvise_generic" {
			t.Fatalf("announcement offered a non-improvise option: %+v", o)
		}
		if o.Obj == tapped {
			t.Fatalf("tapped artifact %v was offered as an Improvise payment", tapped)
		}
		if byID == nil {
			byID = map[state.ObjID]int{}
		}
		byID[o.Obj] = i
	}
	if len(byID) != len(ids) {
		t.Fatalf("announcement offered %d artifacts, want %d: %+v", len(byID), len(ids), d.Options)
	}
	// Pay all eight generic pips with the artifacts.
	var picks []int
	for _, id := range ids {
		picks = append(picks, byID[id])
	}
	submitChoices(t, e, picks...)
	if e.cast != nil {
		t.Fatalf("Improvise payment did not complete the cast: %+v", e.cast)
	}
	if e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("spell went to %s, want stack", e.G.Obj(spell).Zone)
	}
	for _, id := range ids {
		if !e.G.Obj(id).Tapped {
			t.Fatalf("committed Improvise artifact %d was not tapped", id)
		}
	}
	if !e.G.Obj(tapped).Tapped {
		t.Fatal("the pre-tapped artifact was untapped")
	}
	if pool := e.G.Players[0].Pool; pool.Total() != 0 {
		t.Fatalf("pool left %v after Improvise payment, want exactly the two white pips spent", pool)
	}
}

// TestImproviseCommittedArtifactExcludedFromManaWindow: CR 702.66b taps the
// artifacts after mana abilities are done, so an artifact announced for
// Improvise is reserved (convokeCommitted) and is NOT offered again in the
// CR 601.2g mana window; an unannounced mana source still is.
func TestImproviseCommittedArtifactExcludedFromManaWindow(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Organic Extinction"))
	ids := addArtifacts(t, e, 8, plainArtifactSrc)
	manaGear := addArtifacts(t, e, 1, manaArtifactSrc)[0]
	land := e.G.AddObject(card(t, "Name:Land\nTypes:Basic Land Mountain\nA:AB$ Mana | Cost$ T | Produced$ R\nOracle:x\n"), 0)
	land.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), land.ID))
	spell := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MW] = 2
	castMode(t, e, spell, "")
	d := e.Pending()
	if d == nil {
		t.Fatalf("Improvise did not announce: %+v", d)
	}
	var byID map[state.ObjID]int
	for i, o := range d.Options {
		if o.Kind != "improvise_generic" {
			t.Fatalf("announcement offered a non-improvise option: %+v", o)
		}
		if byID == nil {
			byID = map[state.ObjID]int{}
		}
		byID[o.Obj] = i
	}
	if _, ok := byID[manaGear]; !ok {
		t.Fatalf("mana artifact %v was not offered as an Improvise payment", manaGear)
	}
	// Announce seven of the eight plain artifacts, INCLUDING the mana
	// gear, leaving one generic for the mana window.
	var picks []int
	for _, id := range append([]state.ObjID{manaGear}, ids[:6]...) {
		picks = append(picks, byID[id])
	}
	submitChoices(t, e, picks...)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("remaining generic did not open the mana window: %+v", d)
	}
	var windowIDs []state.ObjID
	for _, o := range d.Options {
		if o.Obj != 0 {
			windowIDs = append(windowIDs, o.Obj)
		}
	}
	for _, wid := range windowIDs {
		if e.convokeCommitted(e.cast, wid) {
			t.Fatalf("committed Improvise artifact %v was offered again in the mana window", wid)
		}
		if wid != land.ID {
			t.Fatalf("unexpected mana-window source %v", wid)
		}
	}
	if len(windowIDs) != 1 {
		t.Fatalf("mana window offered %v, want only the land", windowIDs)
	}
	submitChoices(t, e, 0)
	if e.cast != nil {
		t.Fatalf("cast did not complete after the mana window: %+v", e.cast)
	}
	if e.G.Obj(spell).Zone != state.ZStack || !e.G.Obj(manaGear).Tapped || !e.G.Obj(land.ID).Tapped {
		t.Fatalf("payment did not tap and cast: spell=%s gear=%v land=%v",
			e.G.Obj(spell).Zone, e.G.Obj(manaGear).Tapped, e.G.Obj(land.ID).Tapped)
	}
	if pool := e.G.Players[0].Pool; pool.Total() != 0 {
		t.Fatalf("pool left %v after payment", pool)
	}
}

// TestImproviseBottleCapBlastTapsTwoArtifactsPaysFullPrice is the
// brief's named corpus pin (agent-20260919T185907Z): Bottle-Cap Blast
// ({4}{R}, Improvise) cast for its full price from a pool holding only
// the {R} -- the CR 601.2c target ask opens the flow, the CR 601.2b
// announcement offers exactly exactly the four untapped artifacts, both are
// committed and TAPPED, and the pool pays exactly the {R}. Resolution
// then runs the card's own excess rider: against a 2/2 the CR 120.10
// excess is 5 - 2 = 3, so exactly three TAPPED Treasure tokens are
// created (ExcessSVar$ Excess -> DBToken's TokenAmount$ Excess,
// TokenTapped$ True).
func TestImproviseBottleCapBlastTapsTwoArtifactsPaysFullPrice(t *testing.T) {
	t.Parallel()
	e := handEngineTokens(t, corpusAlternativeCard(t, "Bottle-Cap Blast"))
	bear := e.G.AddObject(card(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 1)
	bear.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 1, []state.ObjID{bear.ID})
	ids := addArtifacts(t, e, 4, plainArtifactSrc)
	spell := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MR] = 1
	castMode(t, e, spell, "")
	// CR 601.2b: the Improvise announcement opens the flow (it precedes
	// the CR 601.2c target ask in this engine's cast walk).
	d := e.Pending()
	if d == nil || len(d.Options) != len(ids) {
		t.Fatalf("Improvise announcement offered %v, want all four artifacts: %+v", d.Options, ids)
	}
	var byID map[state.ObjID]int
	for i, o := range d.Options {
		if o.Kind != "improvise_generic" {
			t.Fatalf("announcement offered a non-improvise option: %+v", o)
		}
		if byID == nil {
			byID = map[state.ObjID]int{}
		}
		byID[o.Obj] = i
	}
	var picks []int
	for _, id := range ids {
		picks = append(picks, byID[id])
	}
	submitChoices(t, e, picks...)
	// CR 601.2c: the target ask. Precondition: the bear is on the
	// battlefield and offered.
	d = e.Pending()
	targetIdx := -1
	for i, o := range d.Options {
		if o.Obj == bear.ID {
			targetIdx = i
		}
	}
	if targetIdx < 0 {
		t.Fatalf("target ask did not offer the bear: %+v", d)
	}
	submitChoices(t, e, targetIdx)
	if e.cast != nil {
		t.Fatalf("two-artifact Improvise payment did not complete the cast: %+v", e.cast)
	}
	if e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("spell went to %s, want stack", e.G.Obj(spell).Zone)
	}
	for _, id := range ids {
		if !e.G.Obj(id).Tapped {
			t.Fatalf("committed Improvise artifact %d was not tapped", id)
		}
	}
	if pool := e.G.Players[0].Pool; pool.Total() != 0 {
		t.Fatalf("pool left %v after Improvise payment, want exactly the {R} spent", pool)
	}
	// Resolve: 5 damage to the 2/2, excess 3 -> three tapped Treasures.
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(bear.ID); got == nil || got.Zone != state.ZGraveyard {
		t.Fatalf("bear did not die to the 5 damage: %+v", got)
	}
	var treasures int
	for _, tid := range e.G.Zone(state.ZBattlefield, 0) {
		to := e.G.Obj(tid)
		if to == nil || to.Face() == nil || !strings.HasPrefix(to.Face().Name, "Treasure") {
			continue
		}
		treasures++
		if !to.Tapped {
			t.Fatalf("Treasure token %d entered untapped, want TokenTapped$ True", tid)
		}
	}
	if treasures != 3 {
		t.Fatalf("excess 5-2 created %d Treasure tokens, want 3", treasures)
	}
}

// TestImproviseBottleCapBlastPlayerTargetCreatesNoExcess is the control
// half of the same card: a PLAYER target (dealt 5 damage, no lethal
// permanent amount) binds no excess, so the same DBToken creates nothing.
// Proves the token count is driven by the CR 120.10 excess, not a
// constant.
func TestImproviseBottleCapBlastPlayerTargetCreatesNoExcess(t *testing.T) {
	t.Parallel()
	e := handEngineTokens(t, corpusAlternativeCard(t, "Bottle-Cap Blast"))
	ids := addArtifacts(t, e, 4, plainArtifactSrc)
	spell := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MR] = 1
	life1 := e.G.Players[1].Life
	castMode(t, e, spell, "")
	d := e.Pending()
	if d == nil || len(d.Options) != len(ids) {
		t.Fatalf("Improvise announcement did not offer all four artifacts: %+v", d)
	}
	submitChoices(t, e, 0, 1, 2, 3)
	d = e.Pending()
	playerIdx := -1
	for i, o := range d.Options {
		if o.Obj == 0 && o.Player == 1 {
			playerIdx = i
		}
	}
	if playerIdx < 0 {
		t.Fatalf("target ask did not offer the opposing player: %+v", d.Options)
	}
	submitChoices(t, e, playerIdx)
	if e.cast != nil || e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("cast did not complete: cast=%+v zone=%s", e.cast, e.G.Obj(spell).Zone)
	}
	passUntilStackEmpty(t, e, 20)
	if e.G.Players[1].Life != life1-5 {
		t.Fatalf("player 1 lost %d life, want 5", life1-e.G.Players[1].Life)
	}
	for _, tid := range e.G.Zone(state.ZBattlefield, 0) {
		to := e.G.Obj(tid)
		if to != nil && to.Face() != nil && strings.HasPrefix(to.Face().Name, "Treasure") {
			t.Fatalf("player target created a Treasure token (no excess exists): %d", tid)
		}
	}
}

// TestImproviseNoArtifactsNoAnnouncement is the control: with zero
// artifacts the announcement is never posed (convokeAsk's empty-options
// early return) and a pool-paid cast completes without any decision.
func TestImproviseNoArtifactsNoAnnouncement(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Organic Extinction"))
	spell := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MW] = 8, 2
	castMode(t, e, spell, "")
	if d := e.Pending(); d != nil {
		t.Fatalf("no-artifact Improvise posed an announcement: %+v", d)
	}
	if e.cast != nil || e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("pool-paid Improvise cast did not complete: cast=%+v zone=%s", e.cast, e.G.Obj(spell).Zone)
	}
}

// TestImproviseArtifactCreatureQualifies: CR 702.66a says "artifact", and
// an artifact creature is an artifact independently of being a creature.
func TestImproviseArtifactCreatureQualifies(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Organic Extinction"))
	addArtifacts(t, e, 7, plainArtifactSrc)
	creature := e.G.AddObject(card(t, "Name:Gear Golem\nManaCost:4\nTypes:Artifact Creature Golem\nPT:4/4\nOracle:x\n"), 0)
	creature.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), creature.ID))
	if !creature.EffectiveIsArtifact() {
		t.Fatal("artifact creature did not read as an artifact")
	}
	spell := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MW] = 2
	castMode(t, e, spell, "")
	d := e.Pending()
	announced := map[state.ObjID]bool{}
	for _, o := range d.Options {
		if o.Kind != "improvise_generic" {
			t.Fatalf("announcement offered a non-improvise option: %+v", o)
		}
		announced[o.Obj] = true
	}
	if !announced[creature.ID] {
		t.Fatalf("artifact creature %v was not offered as an Improvise payment: %+v", creature.ID, d.Options)
	}
	picks := []int{}
	for i, o := range d.Options {
		if announced[o.Obj] {
			picks = append(picks, i)
		}
	}
	if len(picks) != 8 {
		t.Fatalf("announcement offered %d artifacts, want 8", len(picks))
	}
	submitChoices(t, e, picks...)
	if e.cast != nil || e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("cast did not complete: cast=%+v zone=%s", e.cast, e.G.Obj(spell).Zone)
	}
	if !e.G.Obj(creature.ID).Tapped {
		t.Fatal("artifact creature was not tapped as the payment")
	}
}
