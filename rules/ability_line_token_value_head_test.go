package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestAbilityLineTokenAmountValueHead covers the printed-ability carrier that
// the SVar-only walk missed: an `A:AB$ Token` / `A:SP$ Token` line whose
// TokenAmount$ reads a Count$ head the evaluator actually resolves. Both
// carriers are latent at the measurement commit (the heads are modelled), so
// this is a coverage-attribution regression, not a verdict fix.
//
// Cloudspire Coordinator reaches the walk through an AB$ Token ability and
// also has SVars (TrigScry), so it guards the "SVars exist but the amount is
// on the ability line" half. Rise of the Varmints has NO SVars at all, so it
// guards the relaxed empty-SVar early return.
func TestAbilityLineTokenAmountValueHead(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)

	cases := []struct {
		card     string
		kind     string
		api      string
		amount   string
		wantHead string
	}{
		{
			card:     "Cloudspire Coordinator",
			kind:     "AB",
			api:      "Token",
			amount:   "Count$ThisTurnEntered_Battlefield_Mount.YouCtrl,Vehicle.YouCtrl",
			wantHead: "count:ThisTurnEntered",
		},
		{
			card:     "Rise of the Varmints",
			kind:     "SP",
			api:      "Token",
			amount:   "Count$ValidGraveyard Creature.YouCtrl",
			wantHead: "count:ValidGraveyard",
		},
	}
	for _, tc := range cases {
		card, ok := reg.Lookup(tc.card)
		if !ok || len(card.Faces) == 0 {
			t.Fatalf("%s missing from corpus", tc.card)
		}
		face := card.Faces[0]

		// Precondition: the ability line the rule reads is present with the
		// exact Kind/API/TokenAmount the attribution depends on. If the
		// parser stops feeding Params, or the line changes shape, this fails
		// loudly rather than passing on an empty walk.
		var carrier *cards.SA
		for _, a := range face.Abilities {
			if a.Kind == tc.kind && a.API == tc.api {
				if a.Params["TokenAmount"] == tc.amount {
					carrier = a
				}
			}
		}
		if carrier == nil {
			t.Fatalf("%s: no A:%s$ %s line with TokenAmount$ %q", tc.card, tc.kind, tc.api, tc.amount)
		}
		params := cards.ValueHeadTokenRecipeExpressions(carrier.Kind, carrier.API, carrier.Params)
		if got := params[strings.TrimPrefix(tc.wantHead, cards.ValueHeadPrefix)]; got != tc.amount {
			t.Fatalf("%s: shared recipe grammar did not classify %q: %v", tc.card, tc.amount, params)
		}

		found := false
		for _, head := range face.ValueHeads() {
			if head == tc.wantHead {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: ValueHeads() = %v, want %s", tc.card, face.ValueHeads(), tc.wantHead)
		}

		// The attributed head must be exactly the expression TokenAmount is
		// read through at run time. Add the source off-zone and evaluate it
		// at the same effects.EvalCountOK boundary Num uses.
		source := e.G.AddObject(card, 0).ID
		if source == 0 {
			t.Fatalf("%s: precondition: source object not created", tc.card)
		}
		ctx := &effects.Ctx{Source: source, Controller: 0, SVars: face.SVars}
		if _, resolved := effects.EvalCountOK(e, ctx, tc.amount); !resolved {
			t.Fatalf("%s: TokenAmount expression %q does not resolve; the attribution would be dishonest", tc.card, tc.amount)
		}
		if !effects.Supported()[tc.wantHead] {
			t.Fatalf("%s: %s is not a registered coverage primitive", tc.card, tc.wantHead)
		}
	}
}
