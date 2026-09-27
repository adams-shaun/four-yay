package rules

// The Count$ head ratchet (the 2026-09-22 repo-deck audit's follow-up): the
// primitive-coverage ratchet (acceptance_test.go's knownUnsupported) only
// asks the registry whether each PRIMITIVE is registered, and the parameter
// census (paramcensus_test.go) only asks whether each PARAMETER is read. A
// Count$ head that is neither a parameter nor a primitive can sit inside a
// fully-registered card and still evaluate to nothing: Cabal Ritual's
// `SVar:X:Count$Threshold.5.3` made the ritual add NO mana and Aspect of
// Hydra's `SVar:X:Count$Devotion.Green` pumped +0/+0, both while their
// primitives (api:Mana, api:Pump) were registered and their Amount$/NumAtt$
// parameters "read". This ratchet closes that blind spot:
//
//   - it walks every repo-deck card's (all 26 deck files, both the 12 pinned
//     Legacy constructed decks and the Commander decks) face SVar bodies
//     that begin with `Count$` through effects.EvalCountOK — the same
//     verdict the SVar-condition gates consume — and fails on any body that
//     comes back UNRESOLVABLE, which is exactly "no head in the dispatch
//     matched".
//   - knownUnmodelledCountHeads is the seeded baseline over the current repo
//     decks, checked in both directions exactly like knownUnsupported: a
//     body the engine newly cannot resolve is a regression; a table entry
//     the build now resolves is stale and must be deleted. It only ever
//     shrinks, and only when a real card test proves the head resolves.
//   - the host is a live two-seat engine and the Ctx a bare
//     {Controller: 0, Source: 0}: the verdict this ratchet holds must not
//     depend on a resolution in flight. Count$ChosenNumber is the one table
//     entry whose (0, false) is a RUNTIME binding verdict, not an unmodelled
//     head (rules seeds Ctx.ChosenNumberBound for effect-created
//     replacement bodies; wildgrowth1) — it stays in the table with its own
//     comment until a bound-context read changes its unbound verdict.

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// knownUnmodelledCountHeads maps each repo-deck SVar Count$ body that still
// evaluates unresolvable to the DISTINCT card faces carrying it (sorted).
// Measured 2026-09-22 over all 26 repo decks at the devotion/threshold
// commit: the Devotion/Threshold/DevotionDual families resolved there, these
// three bodies remain (Count$ResolvedThisTurn and Count$CardNumAttacksThisTurn
// were modelled and removed).
var knownUnmodelledCountHeads = map[string][]string{
	// Forge's Count$NonCombatDamageThisTurn <spec> Any (Temple of Power's
	// "X = noncombat damage you've dealt this turn" payoff): a filtered
	// non-combat damage tally.
	"Count$NonCombatDamageThisTurn Card.Red+YouCtrl Any": {"Temple of Power"},
	// NOT an unmodelled head: Count$ChosenNumber IS implemented
	// (state.ContinuousEffect.ChosenNumber, wildgrowth1) — its (0, false)
	// verdict here is the documented UNBOUND-context read, because this
	// ratchet's Ctx binds nothing. Since fuzz-cov3 an unbound read falls back
	// to the SOURCE object's logged ChosenNumber (effects/choose.go's Choose
	// fold), so the head resolves whenever a source exists; this ratchet's
	// Ctx names none (Source 0), which keeps the verdict unresolvable here.
	// Vault 112: Sadistic Simulation (Science! / pip precon import, task
	// agent-20260922T231317Z-842a3f1e) lands in the same unbound-context class.
	"Count$ChosenNumber": {"Nahiri's Lithoforming", "Vault 112: Sadistic Simulation"},
}

// TestEveryRepoDeckCountHeadResolves is the ratchet: every repo-deck SVar
// Count$ body resolves unless knownUnmodelledCountHeads holds it, in both
// directions.
func TestEveryRepoDeckCountHeadResolves(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e := newSeats(t, 2)
	ctx := &effects.Ctx{Controller: 0, Source: 0}
	got := map[string][]string{}
	for _, dn := range testutil.RepoDeckNames() {
		deck := testutil.RepoDeck(t, reg, dn)
		for _, cd := range deck {
			for fi := range cd.Faces {
				f := cd.Faces[fi]
				for _, body := range f.SVars {
					b := strings.TrimSpace(body)
					if !strings.HasPrefix(b, "Count$") {
						continue
					}
					if _, ok := effects.EvalCountOK(e, ctx, b); !ok {
						if !slices.Contains(got[b], f.Name) {
							got[b] = append(got[b], f.Name)
						}
					}
				}
			}
		}
	}
	if len(got) == 0 && len(knownUnmodelledCountHeads) == 0 {
		return
	}
	for body, cards := range got {
		slices.Sort(cards)
		want, ok := knownUnmodelledCountHeads[body]
		if !ok {
			t.Errorf("%q evaluates unresolvable from a repo deck (carried by %v), which is not in knownUnmodelledCountHeads -- new gap, add it to the table", body, cards)
			continue
		}
		if !slices.Equal(want, cards) {
			t.Errorf("%q: knownUnmodelledCountHeads says %v, measured %v -- update the table to match", body, want, cards)
		}
	}
	for body, want := range knownUnmodelledCountHeads {
		if _, ok := got[body]; !ok {
			t.Errorf("%q now resolves from a repo deck (%v) -- delete the stale knownUnmodelledCountHeads entry", body, want)
		}
	}
}
