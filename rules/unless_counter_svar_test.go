package rules

import (
	"fmt"
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// walkFaceSAPaths visits every distinct compiled SA reachable from a face and
// hands each one a STABLE identity path. Unlike walkAllSAs it (a) dedups by SA
// pointer, so a body reached twice is visited once, and (b) names where each
// SA sits -- `ability0.sub3`, `trigger0`, `repl1`, `svar[DBCounter].sub1` --
// so two Counter lines on the SAME card are separate census rows. The SVar
// roots are walked in sorted name order: ranging a map here would make the
// path a card printed twice reaches depend on iteration order, and a golden
// built on it would flap.
func walkFaceSAPaths(face *cards.Face, visit func(path string, sa *cards.SA)) {
	seen := map[*cards.SA]bool{}
	var walk func(prefix string, sa *cards.SA)
	walk = func(prefix string, sa *cards.SA) {
		for i := 0; sa != nil; sa, i = sa.Sub, i+1 {
			p := prefix
			if i > 0 {
				p = fmt.Sprintf("%s.sub%d", prefix, i)
			}
			if !seen[sa] {
				seen[sa] = true
				visit(p, sa)
			}
		}
	}
	for i, a := range face.Abilities {
		walk(fmt.Sprintf("ability%d", i), a)
	}
	for i, tr := range face.Triggers {
		walk(fmt.Sprintf("trigger%d", i), tr.Effect)
	}
	for i, r := range face.Repls {
		walk(fmt.Sprintf("repl%d", i), r.With)
	}
	names := make([]string, 0, len(face.SVars))
	for n := range face.SVars {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		walk("svar["+n+"]", cards.ResolveSVar(face.SVars, n))
	}
}

// counterUnlessSVarLine is one census row: a single compiled Counter SA whose
// UnlessCost$ names a face SVar, identified by card/face/ability path rather
// than by card NAME.
type counterUnlessSVarLine struct {
	id  string // "<face>#<faceIndex>|<path>|UnlessCost$<svar>"
	sa  *cards.SA
	raw string
}

// counterUnlessSVarLines returns, per face, every such Counter SA in the
// deterministic path order above.
func counterUnlessSVarLines(face *cards.Face, faceIdx int) []counterUnlessSVarLine {
	var out []counterUnlessSVarLine
	walkFaceSAPaths(face, func(path string, sa *cards.SA) {
		raw := sa.Params["UnlessCost"]
		if sa.API != "Counter" || raw == "" {
			return
		}
		if _, isSVar := face.SVars[raw]; !isSVar {
			return
		}
		out = append(out, counterUnlessSVarLine{
			id:  fmt.Sprintf("%s#%d|%s|UnlessCost$%s", face.Name, faceIdx, path, raw),
			sa:  sa,
			raw: raw,
		})
	})
	return out
}

// classifyCounterUnlessSVarLines walks the whole compiled corpus and returns
// one sorted "<identity> => <verdict>" row per Counter SVar unless-cost LINE.
// The verdict is the runtime one: `decline` when effects.UnlessCostResolved
// hands the raw SVar name straight back (the ask is posed but cannot be
// answered "pay" -- a HARD DECLINE), otherwise `resolves:<cost>` carrying the
// concrete folded amount, so a change to the evaluated VALUE is caught too and
// not only a change of kind.
//
// The representative resolution context below supplies every provenance
// channel Counter's corpus forms use: a cast X, a sacrificed-card LKI
// (Mausoleum Wanderer's Sacrificed$CardPower), target/trigger source, a chosen
// number and a remembered object. It is deliberately ONE fixed context: the
// census measures which bodies the shared evaluator can fold at all, not what
// they evaluate to in a particular board state.
func classifyCounterUnlessSVarLines(t *testing.T, reg *cards.Registry) []string {
	t.Helper()
	var out []string
	for _, card := range reg.Cards {
		for fi, face := range card.Faces {
			lines := counterUnlessSVarLines(face, fi)
			if len(lines) == 0 {
				continue
			}
			e := handEngine(t, card)
			id := handIDsByFace(e)[card.Faces[0].Name]
			if id == 0 {
				t.Fatalf("census hand has no %q", card.Faces[0].Name)
			}
			ctx := &effects.Ctx{
				Source:         id,
				Controller:     0,
				Targets:        []state.Target{{Obj: id}},
				Remembered:     []state.Target{{Obj: id}},
				Sacrificed:     []state.SacrificedInfo{{Obj: id, Power: 3}},
				SVars:          face.SVars,
				X:              2,
				Num:            effects.NumberInputs{Chosen: 2, ChosenBound: true},
				TriggerContext: effects.TriggerContext{TriggerCard: id},
			}
			for _, ln := range lines {
				verdict := "decline"
				if got := effects.UnlessCostResolved(e, ctx, ln.sa); got != ln.raw {
					verdict = "resolves:" + got
				}
				out = append(out, ln.id+" => "+verdict)
			}
		}
	}
	sort.Strings(out)
	return out
}

// counterUnlessSVarCensus is the golden: every Counter SA in the compiled
// corpus whose UnlessCost$ names a face SVar, one row per LINE, with the
// verdict the shared fold gives it. Measured 2026-09-22 at FORGE_REF
// 95f04e8a: 53 lines across 41 cards, 50 folded to a concrete amount and 3
// still hard declines because their bodies (Count$Party/Plus, Count$Domain,
// Count$Teamwork) are outside the shared evaluator.
//
// Re-measured 2026-09-23 (task api:Poison): Rune Snag's
// `SVar:Z:Number$2/Plus.Y` MOVED from `decline` to `resolves:{2}`. That is
// this diff's doing and is attributed here: `evalCountExprOK` grew the
// literal `Number$<int>` arm (the Vraska, Betrayal's Sting differential
// `SVar:Difference:Number$9/Minus.X` needs it), so Rune Snag's body is now
// evaluable and its unless-cost prices the correct {2} plus {2} per Rune
// Snag in a graveyard -- with none in any graveyard, {2}. The old `decline`
// row was a fail-closed verdict on an unevaluable body, not a rules
// judgement, so the movement is a fix rather than a regression. It is the
// only row in this census the `Number$` arm moved.
//
// A card-NAME census cannot defend this population: ten cards carry the same
// Counter body twice (once printed, once through the SVar the SubAbility
// names) and Aether Spike carries four, so a line whose classification
// changed left a name-keyed golden green. Each row here is a separate
// assertion. If the corpus pin moves or the evaluator grows a head, re-measure
// and update the row -- do not delete rows to go green.
var counterUnlessSVarCensus = []string{
	"Aether Spike#0|ability0.sub3|UnlessCost$N => resolves:{2}",
	"Aether Spike#0|svar[DBChooseNumber].sub2|UnlessCost$N => resolves:{2}",
	"Aether Spike#0|svar[DBCounter]|UnlessCost$N => resolves:{2}",
	"Aether Spike#0|svar[DBPay].sub1|UnlessCost$N => resolves:{2}",
	"Brine Seer#0|ability0.sub1|UnlessCost$Y => resolves:{1}",
	"Brine Seer#0|svar[DBCounter]|UnlessCost$Y => resolves:{1}",
	"Broken Ambitions#0|ability0|UnlessCost$X => resolves:{2}",
	"Cephalid Shrine#0|svar[TrigCounter]|UnlessCost$X => resolves:{0}",
	"Cephalid Shrine#0|trigger0|UnlessCost$X => resolves:{0}",
	"Circular Logic#0|ability0|UnlessCost$Y => resolves:{0}",
	"Clash of Wills#0|ability0|UnlessCost$X => resolves:{2}",
	"Concerted Defense#0|ability0|UnlessCost$Y => resolves:{1}",
	"Condescend#0|ability0|UnlessCost$X => resolves:{2}",
	"Countervailing Winds#0|ability0|UnlessCost$Y => resolves:{0}",
	"Dazzling Denial#0|ability0|UnlessCost$Y => resolves:{2}",
	"Dispelling Exhale#0|ability0|UnlessCost$X => resolves:{2}",
	// fuzz-cov3 modelled Count$Domain: the census board has no basic land
	// types, so Evasive Action's domain toll prices {0} (was the fail-closed
	// decline on an unevaluable body).
	"Evasive Action#0|ability0|UnlessCost$Y => resolves:{0}",
	"In the Eye of Chaos#0|svar[TrigCounter]|UnlessCost$X => resolves:{3}",
	"In the Eye of Chaos#0|trigger0|UnlessCost$X => resolves:{3}",
	"Invoke Prejudice#0|svar[TrigCounter]|UnlessCost$X => resolves:{4}",
	"Invoke Prejudice#0|trigger0|UnlessCost$X => resolves:{4}",
	"Ixidor's Will#0|ability0|UnlessCost$Y => resolves:{0}",
	"Lilting Refrain#0|ability0|UnlessCost$X => resolves:{0}",
	"Lofty Denial#0|ability0|UnlessCost$Z => resolves:{1}",
	"Logic Knot#0|ability0|UnlessCost$X => resolves:{2}",
	"Martyr of Frost#0|ability0|UnlessCost$X => resolves:{2}",
	"Mausoleum Wanderer#0|ability0|UnlessCost$X => resolves:{3}",
	"Mindswipe#0|ability0|UnlessCost$X => resolves:{2}",
	"Oppressive Will#0|ability0|UnlessCost$Y => resolves:{1}",
	"Override#0|ability0|UnlessCost$Y => resolves:{0}",
	"Overrule#0|ability0|UnlessCost$X => resolves:{2}",
	"Power Sink#0|ability0|UnlessCost$X => resolves:{2}",
	"Protect the Negotiators#0|ability0.sub1|UnlessCost$Y => resolves:{0}",
	"Protect the Negotiators#0|svar[DBCounter]|UnlessCost$Y => resolves:{0}",
	"Rakshasa's Disdain#0|ability0|UnlessCost$Y => resolves:{0}",
	"Repulsive Mutation#0|ability0.sub1|UnlessCost$Y => resolves:{0}",
	"Repulsive Mutation#0|svar[DBCounter]|UnlessCost$Y => resolves:{0}",
	"Rethink#0|ability0|UnlessCost$X => resolves:{3}",
	"Rites of Refusal#0|ability0.sub1|UnlessCost$Y => resolves:{3}",
	"Rites of Refusal#0|svar[DBCounter]|UnlessCost$Y => resolves:{3}",
	"Rune Snag#0|ability0|UnlessCost$Z => resolves:{2}",
	"Scent of Brine#0|ability0.sub1|UnlessCost$ScentOfBrineX => resolves:{1}",
	"Scent of Brine#0|svar[DBScentOfBrineCounter]|UnlessCost$ScentOfBrineX => resolves:{1}",
	"Spectral Denial#0|ability0|UnlessCost$X => resolves:{2}",
	"Spell Rupture#0|ability0|UnlessCost$X => resolves:{0}",
	"Spell Stutter#0|ability0|UnlessCost$Y => resolves:{2}",
	"Spell Syphon#0|ability0|UnlessCost$Y => resolves:{0}",
	"Swallowed by Leviathan#0|ability0.sub1|UnlessCost$X => resolves:{0}",
	"Swallowed by Leviathan#0|svar[DBCounter]|UnlessCost$X => resolves:{0}",
	"Syncopate#0|ability0|UnlessCost$X => resolves:{2}",
	"Thassa's Intervention#0|svar[DBCounter]|UnlessCost$XX => resolves:{4}",
	"Thassa's Rebuff#0|ability0|UnlessCost$X => resolves:{0}",
	"We Say Thee Nay!#0|ability0|UnlessCost$X => decline",
}

// TestCounterUnlessCostSVarResolvedPopulation ratchets the census above per
// Counter LINE, in both directions: a line that stops folding, a line that
// starts folding, a folded AMOUNT that moves, and a line added to or removed
// from the corpus each name themselves here.
func TestCounterUnlessCostSVarResolvedPopulation(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	got := classifyCounterUnlessSVarLines(t, reg)
	want := append([]string(nil), counterUnlessSVarCensus...)
	sort.Strings(want)

	gotSet := map[string]bool{}
	for _, g := range got {
		gotSet[g] = true
	}
	wantSet := map[string]bool{}
	for _, w := range want {
		wantSet[w] = true
	}
	for _, g := range got {
		if !wantSet[g] {
			t.Errorf("Counter unless-cost census: measured row not in the golden: %q", g)
		}
	}
	for _, w := range want {
		if !gotSet[w] {
			t.Errorf("Counter unless-cost census: golden row not measured: %q", w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("Counter unless-cost census = %d lines, golden %d", len(got), len(want))
	}
}

// TestPowerSinkUnlessPayChargesResolvedX is the positive-payment companion to
// the empty-pool regression: the real corpus Power Sink's Count$xPaid SVar is
// folded to {2}, presented as that label, and charges exactly two mana when
// the target's controller accepts. Without the Counter fold this assertion
// sees "Pay the cost" and fails before payment, so it protects the shared
// Count$ path rather than merely the ordinary counter outcome.
func TestPowerSinkUnlessPayChargesResolvedX(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, creatureID := xCounterFixture(t, reg, "Power Sink", "Grizzly Bears", "2")

	pay := drainUntilUnlessPay(t, e, 30)
	if pay == nil || pay.ResumeKind != "unless_pay" || len(pay.Options) < 2 {
		t.Fatalf("Power Sink unless-pay ask = %+v, want pay/decline", pay)
	}
	if got := pay.Options[0].Label; got != "Pay {2} — don't counter" {
		t.Fatalf("Power Sink X=2 pay label = %q, want %q", got, "Pay {2} — don't counter")
	}
	before := e.G.Players[0].Pool
	submitChoices(t, e, pay.Options[0].Index)
	if got := e.G.Players[0].Pool; got[state.MU] != before[state.MU]-2 {
		t.Fatalf("Power Sink accepted {2} payment left blue mana %d, want %d (before %d)", got[state.MU], before[state.MU]-2, before[state.MU])
	}
	passUntilStackEmpty(t, e, 30)
	if got := e.G.Obj(creatureID).Zone; got != state.ZBattlefield {
		t.Fatalf("Power Sink paid target zone = %s, want Battlefield", got)
	}
}
