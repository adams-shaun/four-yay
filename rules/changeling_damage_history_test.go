package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// Damage history binds the complete effective type set at the hit, including
// Changeling's semantic CDA when no layer-4 sidecar entry was needed.
func TestChangelingDamageHistoryKeepsDamageTimeTypes(t *testing.T) {
	e, ids := pcdrEngine(t, "Amoeboid Changeling", "Changeling Outcast", "Grizzly Bears")
	changeling := ids["Changeling Outcast"]
	bear := ids["Grizzly Bears"]
	for name, id := range ids {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: %s is not on the battlefield", name)
		}
	}
	if !e.Derived(changeling).AllCreatureTypes || !effects.MatchesSpecCtx(e.G, "Creature.Surrakar", changeling, e.specCtx(0, 0)) {
		t.Fatal("precondition: unstripped Changeling must have all creature types")
	}

	// Both directions must retain the unstripped CDA in their history.
	damageCountEvent(e, changeling, bear, 0, 1, false)
	damageCountEvent(e, bear, changeling, 0, 1, false)
	sourceRecord := e.G.Obj(changeling).DamageDealtThisTurn[0]
	recipientRecord := e.G.Obj(bear).DamageDealtThisTurn[0]
	if !slices.Contains(sourceRecord.SourceTypes, "Surrakar") || !slices.Contains(recipientRecord.RecipientTypes, "Surrakar") {
		t.Fatalf("precondition: damage-time records omit the semantic type: source=%v recipient=%v", sourceRecord.SourceTypes, recipientRecord.RecipientTypes)
	}
	ctx := &effects.Ctx{Controller: 0, Source: bear}
	if got := effects.EvalCount(e, ctx, "Count$NumDamageThisTurn Creature.Surrakar Any"); got != 1 {
		t.Errorf("unstripped changeling source history = %d, want 1", got)
	}
	if got := effects.EvalCount(e, ctx, "Count$NumDamageThisTurn Card Creature.Surrakar"); got != 1 {
		t.Errorf("unstripped changeling recipient history = %d, want 1", got)
	}

	// A later layer-4 strip cannot rewrite either historical snapshot.
	e.AddContinuous(state.ContinuousEffect{Source: changeling, Affects: "Card.Self", Layer: state.LType, RemoveCreatureTypes: true})
	if e.Derived(changeling).AllCreatureTypes {
		t.Fatal("precondition: the layer-4 type-strip effect must have applied")
	}
	if got := effects.EvalCount(e, ctx, "Count$NumDamageThisTurn Creature.Surrakar Any"); got != 1 {
		t.Errorf("post-strip historical source count = %d, want 1", got)
	}
	if got := effects.EvalCount(e, ctx, "Count$NumDamageThisTurn Card Creature.Surrakar"); got != 1 {
		t.Errorf("post-strip historical recipient count = %d, want 1", got)
	}
}

// Stripped damage snapshots stay stripped even after the CDA is restored at
// cleanup; live printed-face fallback must not resurrect their old types.
func TestGrantedCreatureTypesRemainInDamageHistoryAfterExpiry(t *testing.T) {
	e, ids := pcdrEngine(t, "Changeling Outcast", "Grizzly Bears")
	changeling, bear := ids["Changeling Outcast"], ids["Grizzly Bears"]
	e.AddContinuous(state.ContinuousEffect{Source: bear, Affects: "Card.Self", Layer: state.LType, AddAllCreatureTypes: true, UntilEOT: true})
	if !e.Derived(bear).AllCreatureTypes {
		t.Fatal("precondition: the layer-4 all-types grant must apply to the battlefield Bear")
	}
	damageCountEvent(e, bear, changeling, 0, 1, false)
	damageCountEvent(e, changeling, bear, 0, 1, false)
	if !slices.Contains(e.G.Obj(bear).DamageDealtThisTurn[0].SourceTypes, "Surrakar") || !slices.Contains(e.G.Obj(changeling).DamageDealtThisTurn[0].RecipientTypes, "Surrakar") {
		t.Fatal("precondition: damage snapshots must capture the granted all-types set")
	}
	e.EndOfTurnCleanup()
	if e.Derived(bear).AllCreatureTypes {
		t.Fatal("precondition: cleanup must expire the temporary all-types grant")
	}
	ctx := &effects.Ctx{Controller: 0, Source: changeling}
	if got := effects.EvalCount(e, ctx, "Count$NumDamageThisTurn Creature.Surrakar Any"); got != 2 {
		t.Errorf("source history after grant expiry = %d, want 2", got)
	}
	if got := effects.EvalCount(e, ctx, "Count$NumDamageThisTurn Card Creature.Surrakar"); got != 2 {
		t.Errorf("recipient history after grant expiry = %d, want 2", got)
	}
}

func TestChangelingDamageHistoryKeepsStripAndExpirySnapshots(t *testing.T) {
	e, ids := pcdrEngine(t, "Changeling Outcast", "Grizzly Bears")
	changeling, bear := ids["Changeling Outcast"], ids["Grizzly Bears"]
	if e.G.Obj(changeling).Zone != state.ZBattlefield || e.G.Obj(bear).Zone != state.ZBattlefield {
		t.Fatal("precondition: both damage participants must be on the battlefield")
	}
	e.AddContinuous(state.ContinuousEffect{Source: changeling, Affects: "Card.Self", Layer: state.LType, RemoveCreatureTypes: true, UntilEOT: true})
	if e.Derived(changeling).AllCreatureTypes || effects.MatchesSpecCtx(e.G, "Creature.Surrakar", changeling, e.specCtx(0, 0)) {
		t.Fatal("precondition: strip must clear Changeling's effective creature types")
	}
	damageCountEvent(e, changeling, bear, 0, 1, false)
	damageCountEvent(e, bear, changeling, 0, 1, false)
	ctx := &effects.Ctx{Controller: 0, Source: bear}
	if got := effects.EvalCount(e, ctx, "Count$NumDamageThisTurn Creature.Surrakar Any"); got != 0 {
		t.Errorf("stripped source damage history = %d, want 0", got)
	}
	if got := effects.EvalCount(e, ctx, "Count$NumDamageThisTurn Card Creature.Surrakar"); got != 0 {
		t.Errorf("stripped recipient damage history = %d, want 0", got)
	}
	e.EndOfTurnCleanup()
	if !e.Derived(changeling).AllCreatureTypes {
		t.Fatal("precondition: cleanup must restore the intrinsic Changeling CDA")
	}
	damageCountEvent(e, changeling, bear, 0, 1, false)
	damageCountEvent(e, bear, changeling, 0, 1, false)
	if got := effects.EvalCount(e, ctx, "Count$NumDamageThisTurn Creature.Surrakar Any"); got != 1 {
		t.Errorf("post-expiry Changeling source history = %d, want 1", got)
	}
	if got := effects.EvalCount(e, ctx, "Count$NumDamageThisTurn Card Creature.Surrakar"); got != 1 {
		t.Errorf("post-expiry Changeling recipient history = %d, want 1", got)
	}
}
