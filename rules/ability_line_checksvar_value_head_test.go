package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestAbilityLineCheckSVarValueHead covers the printed-ability gate carrier
// the SVar-only walk missed: an `A:AB$ …` / `A:SP$ …` line whose gate is an
// inline Count$ expression --
// `CheckSVar$ Count$<head>…` (with an `SVarCompare$`) -- read at activation
// time through effects.CheckSVarHolds. Face.ValueHeads must attribute the
// head, because rules/legal_activation.go's sVarGateOK fails OPEN when the
// body is unreadable: an unlisted head would let the card join the playable
// pool while silently ignoring its own activation restriction.
//
// Three real-corpus carriers, one per verdict shape:
//
//   - Master's Manufactory (`A:AB$ Token`, head ThisTurnEntered) exercises
//     the face[1] half: the gate is on the back face, not face[0].
//   - Izzet Generatorium (`A:AB$ Draw`, head CountersRemovedThisTurn)
//     exercises a head that resolves but was missing from
//     effects.modelledValueHeads -- the attribution fix moves it, and this
//     asserts the head is now BOTH attributed and registered.
//   - Reset (`A:SP$ UntapAll`, head FinishedUpkeepsThisTurn) exercises a
//     head that does NOT resolve: attribution is correct and
//     Registry.Unsupported must now name `count:FinishedUpkeepsThisTurn`,
//     so the card becomes correctly Unsupported (its gate currently fails
//     open and it is castable any turn).
func TestAbilityLineCheckSVarValueHead(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)

	cases := []struct {
		card     string
		face     int
		kind     string
		api      string
		check    string
		wantHead string
		// resolves is whether effects.EvalCountOK resolves the exact gate
		// expression. A resolving head must be registered in
		// effects.Supported (and hence modelled); a non-resolving head must
		// not be, and Registry.Unsupported must report it.
		resolves bool
	}{
		{
			card:     "Master's Guide-Mural",
			face:     1,
			kind:     "AB",
			api:      "Token",
			check:    "Count$ThisTurnEntered_Battlefield_Card.Self+YouCtrl,Artifact.YouCtrl+Other",
			wantHead: "count:ThisTurnEntered",
			resolves: true,
		},
		{
			card:     "Izzet Generatorium",
			face:     0,
			kind:     "AB",
			api:      "Draw",
			check:    "Count$CountersRemovedThisTurn ENERGY You",
			wantHead: "count:CountersRemovedThisTurn",
			resolves: true,
		},
		{
			card:     "Reset",
			face:     0,
			kind:     "SP",
			api:      "UntapAll",
			check:    "Count$FinishedUpkeepsThisTurn",
			wantHead: "count:FinishedUpkeepsThisTurn",
			resolves: false,
		},
	}
	for _, tc := range cases {
		card, ok := reg.Lookup(tc.card)
		if !ok || len(card.Faces) <= tc.face {
			t.Fatalf("%s missing from corpus or has no face %d", tc.card, tc.face)
		}
		face := card.Faces[tc.face]

		// Precondition: the ability line the rule reads is present on the
		// named face with the exact Kind/API/CheckSVar the attribution
		// depends on. If the parser stops feeding Params, or the line
		// changes shape, this fails loudly rather than passing on a walk
		// that never saw a gate.
		var carrier *cards.SA
		for _, a := range face.Abilities {
			if a.Kind == tc.kind && a.API == tc.api && a.Params["CheckSVar"] == tc.check {
				carrier = a
			}
		}
		if carrier == nil {
			t.Fatalf("%s face[%d]: no A:%s$ %s line with CheckSVar$ %q", tc.card, tc.face, tc.kind, tc.api, tc.check)
		}

		// The shared gate grammar must classify the exact expression, so
		// attribution and this assertion cannot disagree.
		gates := cards.ValueHeadGateExpression(carrier.Params)
		if got := gates[strings.TrimPrefix(tc.wantHead, cards.ValueHeadPrefix)]; got != tc.check {
			t.Fatalf("%s: shared gate grammar did not classify %q: %v", tc.card, tc.check, gates)
		}

		found := false
		for _, head := range face.ValueHeads() {
			if head == tc.wantHead {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s face[%d]: ValueHeads() = %v, want %s", tc.card, tc.face, face.ValueHeads(), tc.wantHead)
		}

		// The attributed head must be exactly the expression the gate is
		// read through at run time. Add the source off-zone and evaluate it
		// at the same effects.EvalCountOK boundary CheckSVarHolds uses.
		source := e.G.AddObject(card, 0).ID
		if source == 0 {
			t.Fatalf("%s: precondition: source object not created", tc.card)
		}
		ctx := &effects.Ctx{Source: source, Controller: 0, SVars: face.SVars}
		_, resolved := effects.EvalCountOK(e, ctx, tc.check)
		if resolved != tc.resolves {
			t.Fatalf("%s: EvalCountOK(%q) resolved=%v, want %v", tc.card, tc.check, resolved, tc.resolves)
		}

		// Attribution and the honesty gate must agree with the evaluator:
		// a resolving head is a registered primitive; a non-resolving head
		// is not, and Registry.Unsupported reports the card.
		if tc.resolves && !effects.Supported()[tc.wantHead] {
			t.Fatalf("%s: %s resolves but is not a registered coverage primitive", tc.card, tc.wantHead)
		}
		if !tc.resolves && effects.Supported()[tc.wantHead] {
			t.Fatalf("%s: %s does not resolve but is a registered coverage primitive", tc.card, tc.wantHead)
		}
		unsupported := reg.Unsupported(card, effects.Supported())
		has := false
		for _, u := range unsupported {
			if u == tc.wantHead {
				has = true
			}
		}
		if has != !tc.resolves {
			t.Fatalf("%s: Unsupported() = %v, want contains=%v for %s", tc.card, unsupported, !tc.resolves, tc.wantHead)
		}
	}
}
