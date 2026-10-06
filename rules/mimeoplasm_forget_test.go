package rules

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The Mimeoplasm regression (ticket: ChooseCard.ForgetChosen +
// ChangeZone-family ForgetOtherRemembered). Its printed chain exercises both
// parameters exactly as Forge plays it:
//
//	MimeoChooseTwo  ChooseCard  RememberChosen$ True  -- remember 2 yard creatures
//	MimeoExile      ChangeZoneAll  RememberChanged$ True | ForgetOtherRemembered$ True
//	MimeoChooseCopy ChooseCard  ForgetChosen$ True    -- forget the chosen copy template
//	MimeoAddCounters PutCounter  CounterNum$ Remembered$CardPower, conditioned EQ1
//	MimeoCopyChosen  Clone  Defined$ ChosenCard, conditioned EQ1
//
// Without ForgetChosen the remembered set after the copy choice still holds
// BOTH exiled cards, so the EQ1 conditions fail and no counters or copy
// appear; without ForgetOtherRemembered the ChangeZoneAll sweep's
// Card.IsRemembered selector races the clear (or a stale entry survives the
// re-remember), so the exile leg itself breaks. The effects-level pins for
// each parameter live in effects/forget_remembered_test.go; this test is the
// real-corpus end to end.

func cardPower(t *testing.T, o *state.Object) int {
	t.Helper()
	p, _, ok := strings.Cut(o.Face().PT, "/")
	if !ok {
		t.Fatalf("card %q carries no PT", o.Face().Name)
	}
	v, err := strconv.Atoi(p)
	if err != nil {
		t.Fatalf("card %q PT %q does not parse", o.Face().Name, o.Face().PT)
	}
	return v
}

func TestMimeoplasmForgetChosenLeavesTheOtherRemembered(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	mimeo, ok := reg.Lookup("The Mimeoplasm")
	if !ok {
		t.Fatal("corpus is missing The Mimeoplasm -- the ticket's real carrier")
	}
	// Script preconditions: the chain rides exactly the two parameters this
	// ticket implements. If the carrier's SVars change shape, this pin must
	// be re-measured, not silently skipped.
	var exileSA, copySA *cards.SA
	for _, f := range mimeo.Faces {
		if sa := cards.ResolveSVar(f.SVars, "MimeoExile"); sa != nil {
			exileSA = sa
		}
		if sa := cards.ResolveSVar(f.SVars, "MimeoChooseCopy"); sa != nil {
			copySA = sa
		}
	}
	if exileSA == nil || copySA == nil {
		t.Fatal("The Mimeoplasm no longer carries the MimeoExile/MimeoChooseCopy SVars")
	}
	if exileSA.Params["ForgetOtherRemembered"] != "True" || exileSA.Params["RememberChanged"] != "True" {
		t.Fatalf("precondition: MimeoExile params moved: %+v", exileSA.Params)
	}
	if copySA.Params["ForgetChosen"] != "True" {
		t.Fatalf("precondition: MimeoChooseCopy params moved: %+v", copySA.Params)
	}

	first := corpusCard(t, "Grizzly Bears") // 2/2
	second := corpusCard(t, "Hill Giant")   // 3/3
	e := handEngine(t, mimeo)
	a := e.G.AddObject(first, 0)
	b := e.G.AddObject(second, 0)
	a.Zone, b.Zone = state.ZGraveyard, state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, 0, []state.ObjID{a.ID, b.ID})
	a, b = e.G.Obj(a.ID), e.G.Obj(b.ID)
	pa, pb := cardPower(t, a), cardPower(t, b)
	if a.Zone != state.ZGraveyard || b.Zone != state.ZGraveyard {
		t.Fatalf("precondition: yard creatures not in the graveyard (%s, %s)", a.Zone, b.Zone)
	}
	if pa == pb {
		t.Fatalf("precondition: the two yard creatures must have distinct powers (both %d) -- the counter count could not tell the chosen from the kept card", pa)
	}

	id := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MB] = 2, 1
	e.G.Players[0].Pool[state.MG], e.G.Players[0].Pool[state.MU] = 1, 1
	e.beginCast(0, decision.Option{Kind: "cast", Obj: id})
	e.resolveTop()

	// Ask 1: the ETB setup ChooseCard -- exile the two yard creatures.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("exile ask = %+v, want a KChoose", d)
	}
	idxA, idxB := -1, -1
	for _, o := range d.Options {
		if o.Obj == a.ID {
			idxA = o.Index
		}
		if o.Obj == b.ID {
			idxB = o.Index
		}
	}
	if idxA < 0 || idxB < 0 {
		t.Fatalf("precondition: the two yard creatures were not offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idxA, idxB}}); err != nil {
		t.Fatalf("submit exile choice: %v", err)
	}
	if e.G.Obj(a.ID).Zone != state.ZExile || e.G.Obj(b.ID).Zone != state.ZExile {
		t.Fatalf("the exile leg did not move both creatures: %s, %s", e.G.Obj(a.ID).Zone, e.G.Obj(b.ID).Zone)
	}
	// The ChangeZoneAll's ForgetOtherRemembered + RememberChanged leaves
	// exactly the exiled pair in the source's persistent remembered set.
	src := e.G.Obj(id)
	if got := rememberedPair(src); len(got) != 2 {
		t.Fatalf("after the exile leg the source remembered %v, want exactly the exiled pair", got)
	}

	// Ask 2: choose the copy template from the exiled pair.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("copy ask = %+v, want a KChoose", d)
	}
	idxA = -1
	for _, o := range d.Options {
		if o.Obj == a.ID {
			idxA = o.Index
		}
	}
	if idxA < 0 {
		t.Fatalf("precondition: the exiled pair was not offered for the copy: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idxA}}); err != nil {
		t.Fatalf("submit copy choice: %v", err)
	}

	// The chain's tail: Mimeoplasm entered as a copy of the CHOSEN creature,
	// with +1/+1 counters equal to the OTHER card's power -- the read the
	// ForgetChosen bookkeeping serves (the remaining remembered card).
	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("Mimeoplasm zone = %s, want battlefield", o.Zone)
	}
	if o.Face().Name != first.Faces[0].Name {
		t.Fatalf("Mimeoplasm entered as %q, want a copy of the chosen %q", o.Face().Name, first.Faces[0].Name)
	}
	if got := int(o.Counter("P1P1")); got != pb {
		t.Fatalf("P1P1 counters = %d, want %d (the OTHER card's power -- the read ForgetChosen$ leaves one remembered card for)", got, pb)
	}
	// DBCleanup's ClearRemembered/ClearChosenCard emptied both lists -- the
	// chain's own tail, reachable only when the EQ1 conditions above passed.
	if len(o.Remembered) != 0 {
		t.Errorf("after the chain the source still remembers %v -- DBCleanup did not run (a condition failed)", o.Remembered)
	}
	if len(o.Chosen) != 0 {
		t.Errorf("after the chain the source still holds chosen %v -- DBCleanup did not run", o.Chosen)
	}
}

func rememberedPair(o *state.Object) []state.ObjID {
	out := make([]state.ObjID, 0, len(o.Remembered))
	for _, tg := range o.Remembered {
		if !tg.IsPlayer {
			out = append(out, tg.Obj)
		}
	}
	return out
}

// TestMimeoplasmForgetParamsAreRead is the parameter-census pin: the
// Mimeoplasm is not a repo-deck card, so TestEveryRepoDeckParamsAreRead
// never walks it. Pin its census directly: after the ForgetChosen$ /
// ForgetOtherRemembered$ reads landed, the card must census with ZERO
// unread-param labels, and the derived read sets must still attribute both
// keys (otherwise this pin could no longer fail).
func TestChooseCardImprintChosenParamRead(t *testing.T) {
	t.Parallel()
	_, d := measureParamCensus(t, nil)
	if !d.api["ChooseCard"]["ImprintChosen"] {
		t.Fatal("api:ChooseCard no longer derives the ImprintChosen read")
	}
	for _, api := range []string{"ChoosePlayer", "ChooseSource", "GainControl"} {
		if d.api[api]["ImprintChosen"] {
			t.Fatalf("ImprintChosen read attributed to unrelated api:%s", api)
		}
	}
}

func TestMimeoplasmForgetParamsAreRead(t *testing.T) {
	t.Parallel()
	_, d := measureParamCensus(t, nil)
	if !d.api["ChangeZoneAll"]["ForgetOtherRemembered"] {
		t.Fatal("api:ChangeZoneAll no longer derives the ForgetOtherRemembered read -- the census cannot see the parameter this ticket implemented")
	}
	if !d.api["ChooseCard"]["ForgetChosen"] {
		t.Fatal("api:ChooseCard no longer derives the ForgetChosen read -- the census cannot see the parameter this ticket implemented")
	}
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("The Mimeoplasm")
	if !ok {
		t.Fatal("corpus is missing The Mimeoplasm")
	}
	if labels := cardCensusLabels(c, d, nil); len(labels) != 0 {
		t.Errorf("The Mimeoplasm censuses with unread labels %v -- a parameter regressed or a new one is unread", labels)
	}
}
