package manabrew

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
)

// TestBoardTargetsPromptAndResponse covers the §6.3 target build (the
// TargetEffect.API → TargetingIntent table) and the §6.4 response mapping.
func TestBoardTargetsPromptAndResponse(t *testing.T) {
	tr := New("table", 2, nil)
	v := battleView()

	// A removal ask: TargetEffect classifies the ask, the adapter derives
	// intent + hostile from the same vocabulary.
	three := 3
	d := &decision.Decision{Seq: 12, Player: 1, Kind: decision.KTarget, Min: 1, Max: 2, Prompt: "Choose a target to destroy",
		TargetEffect: &decision.TargetEffect{API: "DealDamage", Damage: &decision.DamageEffect{Amount: &three}},
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Label: "Bear", Obj: 2},
			{Index: 1, Kind: "player", Label: "Alice", Player: 0},
		}}
	msg := pendingMustBuild(t, d, v)
	in, ok := msg.Input.Value.(mb.ChooseBoardTargetsInput)
	if !ok {
		t.Fatalf("input type = %s, want chooseBoardTargets", msg.Input.Value.PromptType())
	}
	// Precondition: the fixture resolves both target shapes.
	if findCard(&v, 2) == nil || playerLabel(&v, 0) != "Alice" {
		t.Fatal("fixture must resolve the permanent and the player label")
	}
	if in.Intent != mb.IntentDamage || !in.Hostile {
		t.Fatalf("DealDamage must map to hostile damage, got intent %q hostile %v", in.Intent, in.Hostile)
	}
	if in.MinTargets != 1 || in.MaxTargets != 2 {
		t.Fatalf("min/max targets = %d/%d, want 1/2", in.MinTargets, in.MaxTargets)
	}
	if len(in.Candidates) != 2 {
		t.Fatalf("candidates = %v, want one per option", in.Candidates)
	}
	if in.Candidates[0] != (mb.TargetRef{Kind: mb.RefCard, ID: "o2", Intent: mb.IntentDamage, Oracle: "Bear"}) {
		t.Fatalf("permanent candidate = %#v", in.Candidates[0])
	}
	if in.Candidates[1] != (mb.TargetRef{Kind: mb.RefPlayer, ID: "player-0", Intent: mb.IntentDamage, Oracle: "Alice"}) {
		t.Fatalf("player candidate = %#v", in.Candidates[1])
	}
	if in.Presentation.Title != "Choose a target to destroy" {
		t.Fatalf("presentation title = %q, want the engine prompt text", in.Presentation.Title)
	}
	if in.Cancellable {
		t.Fatal("Cancellable must stay false while G-3 gates it on AutoMana")
	}

	p := pendingFor(d, v)
	// Both targets in response order: the response lists player-0 first, so
	// the intent's choice order is [option of player-0, option of o2].
	o := tr.TranslateResponse(respFor(p, mb.BoardTargetsDecision{Chosen: []mb.TargetRef{
		{Kind: mb.RefPlayer, ID: "player-0"}, {Kind: mb.RefCard, ID: "o2"}}}), p, 1)
	got := mustIntent(t, o)
	if !reflect.DeepEqual(got.Choices, []int{1, 0}) {
		t.Fatalf("chosen = %v, want the response order [1 0]", got.Choices)
	}
	// An unknown target is invalidShape ("not offered").
	wantErrCode(t, tr.TranslateResponse(respFor(p, mb.BoardTargetsDecision{Chosen: []mb.TargetRef{
		{Kind: mb.RefCard, ID: "o9"}}}), p, 1), mb.CodeInvalidShape)
	// Too many targets: Decision.Validate's own text rides invalidShape.
	wantErrCode(t, tr.TranslateResponse(respFor(p, mb.BoardTargetsDecision{Chosen: []mb.TargetRef{
		{Kind: mb.RefCard, ID: "o2"}, {Kind: mb.RefPlayer, ID: "player-0"}, {Kind: mb.RefCard, ID: "o5"}}}), p, 1), mb.CodeInvalidShape)
	// The two above already fail; assert the fence is Validate's, not the
	// matcher's: the same three ref set against a Max=3 decision passes.
	d3 := *d
	d3.Max = 3
	d3.Options = append(d3.Options, decision.Option{Index: 2, Kind: "permanent", Label: "Siege", Obj: 5})
	p3 := pendingFor(&d3, v)
	o = tr.TranslateResponse(respFor(p3, mb.BoardTargetsDecision{Chosen: []mb.TargetRef{
		{Kind: mb.RefCard, ID: "o2"}, {Kind: mb.RefPlayer, ID: "player-0"}, {Kind: mb.RefCard, ID: "o5"}}}), p3, 1)
	if !reflect.DeepEqual(mustIntent(t, o).Choices, []int{0, 1, 2}) {
		t.Fatal("three legal targets must pass when Max allows")
	}
}

// TestTargetingIntentTable pins the §6.3 intent table rows the corpus
// reaches today, row by row, so a code change that reclassifies an API is
// caught here.
func TestBoardTargetsIntentTable(t *testing.T) {
	rows := []struct {
		eff     *decision.TargetEffect
		intent  mb.TargetingIntent
		hostile bool
	}{
		{&decision.TargetEffect{API: "DealDamage", Damage: &decision.DamageEffect{}}, mb.IntentDamage, true},
		{&decision.TargetEffect{API: "Counter"}, mb.IntentCounter, true},
		{&decision.TargetEffect{API: "Draw"}, mb.IntentDraw, false},
		{&decision.TargetEffect{API: "Mill"}, mb.IntentMill, true},
		{&decision.TargetEffect{API: "Discard"}, mb.IntentDiscard, true},
		{&decision.TargetEffect{API: "Pump"}, mb.IntentBuff, false},
		{&decision.TargetEffect{API: "GainLife"}, mb.IntentHeal, false},
		{&decision.TargetEffect{API: "LoseLife"}, mb.IntentLoseLife, true},
		{&decision.TargetEffect{API: "TapAll"}, mb.IntentTap, true},
		{&decision.TargetEffect{API: "Untap"}, mb.IntentUntap, false},
		{&decision.TargetEffect{API: "CopyPermanent"}, mb.IntentCopy, false},
		{&decision.TargetEffect{API: "GainControl"}, mb.IntentGainControl, true},
		{&decision.TargetEffect{API: "Fight"}, mb.IntentFight, true},
		{&decision.TargetEffect{API: "Attach"}, mb.IntentAttach, false},
		{&decision.TargetEffect{API: "Destroy"}, mb.IntentDestroy, true},
		{&decision.TargetEffect{API: "Sacrifice"}, mb.IntentSacrifice, true},
		{&decision.TargetEffect{API: "ChangeZone", Removal: &decision.RemovalEffect{Kind: "exile", Destination: "Exile"}}, mb.IntentExile, true},
		{&decision.TargetEffect{API: "ChangeZone", Removal: &decision.RemovalEffect{Kind: "bounce", Destination: "Hand"}}, mb.IntentBounce, false},
		{&decision.TargetEffect{Removal: &decision.RemovalEffect{Kind: "graveyard"}}, mb.IntentMill, true},
		// No effect context at all: intent empty, hostile false (§6.3).
		{nil, "", false},
		// An unknown API with no context: no intent is claimed either way.
		{&decision.TargetEffect{API: "Venture"}, "", false},
		// An unrecognised removal kind with no API classification at all is
		// still hostile by shape.
		{&decision.TargetEffect{API: "Venture", Removal: &decision.RemovalEffect{Kind: "somewhere"}}, mb.IntentDestroy, true},
	}
	for _, r := range rows {
		intent, hostile := targetingIntent(r.eff)
		if intent != r.intent || hostile != r.hostile {
			t.Errorf("effect %+v: got intent %q hostile %v, want %q hostile %v", r.eff, intent, hostile, r.intent, r.hostile)
		}
	}
}

// TestTargetPromptWithoutEffectContext pins the no-context shape: intent
// empty (omitted on the refs), hostile false, candidates still one per
// option.
func TestBoardTargetsWithoutEffectContext(t *testing.T) {
	d := &decision.Decision{Seq: 4, Player: 1, Kind: decision.KTarget, Min: 1, Max: 1, Prompt: "Choose a card",
		Options: []decision.Option{{Index: 0, Kind: "permanent", Label: "Bear", Obj: 2}}}
	v := smallView()
	msg := pendingMustBuild(t, d, v)
	in := msg.Input.Value.(mb.ChooseBoardTargetsInput)
	if in.Intent != "" || in.Hostile {
		t.Fatalf("no-context ask must be intent-empty and non-hostile, got %q/%v", in.Intent, in.Hostile)
	}
	if len(in.Candidates) != 1 || in.Candidates[0].Intent != "" || in.Candidates[0].Oracle != "Bear" {
		t.Fatalf("candidates = %#v", in.Candidates)
	}
}
