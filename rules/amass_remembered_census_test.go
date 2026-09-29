package rules

// Amass-remembered census (task agent-20260928T215010Z-bea05a4b, filed from
// the Goblin Plate Mail finding).
//
// `DB$ Amass | ... | RememberAmass$ True` records the amassed Army in the
// resolution's Remembered list (effects/amass.go, amassRiders). The Goblin
// Plate Mail finding this ticket came from was that the FOLLOWING
// `DB$ Attach | Defined$ Remembered` sub did not see that entry, so the
// Equipment stayed unattached (AttachedTo == 0). The mechanism is fixed in
// the base this branched from: the shared resolution Ctx carries the amass
// write into the chained sub, and
// TestSetAudit_hob_GoblinPlateMail_AmassesThenAttaches (unguarded) passes.
// This census does not change engine behaviour; it names the affected class so
// every consumer of the remembered Army is on record, not just the Attach one.
//
// This census pins the class so a corpus-pin bump that adds a new
// RememberAmass$ consumer is named rather than silently joining an untested
// set. It is a data census (the same shape as rules/paramcensus_test.go), not
// a rules test: it reads the compiled corpus directly.
//
// Measured on the pin in the Makefile (95f04e8a04c8925fa97cb226fc3341cabcc90a53):
// 5 distinct cards carry RememberAmass$ and consume the remembered Army:
//
//   - Goblin Plate Mail                -> Attach   | Defined$ Remembered
//   - Foray of Orcs                    -> ImmediateTrigger | ConditionDefined$/RememberObjects$ Remembered
//   - Grishnákh, Brash Instigator      -> ImmediateTrigger | ConditionDefined$/RememberObjects$ Remembered
//   - Surrounded by Orcs               -> Mill     | X:Remembered$CardPower
//   - Widespread Brutality             -> DamageAll | ConditionDefined$/DamageSource$ Remembered + X:Remembered$CardPower
//
// The walk lists Goblin Plate Mail and Grishnákh twice (the trigger's Execute$
// body and the SVar walk reach the same chain); the census dedupes by card, so
// the table is per-card, not per-chain.

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// amassRememberConsumer describes, for one card, how its chain reads the Army
// `RememberAmass$ True` remembered.
type amassRememberConsumer struct {
	// api is the consuming sub-ability's API (Attach, ImmediateTrigger, ...).
	api string
	// params are the consuming SA's params that name Remembered.
	params []string
	// powerViaRemembered is true when the face's SVar table defines X as a
	// `Remembered$...` value head (the "the amassed Army's power" wording).
	powerViaRemembered bool
}

// amassRememberedConsumers walks every card's resolved ability chains and
// returns, per card name, every sub-ability chained after a
// `RememberAmass$ True` Amass. Chains are deduped by (card, api, params), so
// a card reached through both a trigger's Execute$ and the blanket SVar walk
// appears once.
func amassRememberedConsumers(reg *cards.Registry) map[string][]amassRememberConsumer {
	out := map[string][]amassRememberConsumer{}
	seen := map[string]bool{}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			var chains []*cards.SA
			chains = append(chains, f.Abilities...)
			for _, tr := range f.Triggers {
				chains = append(chains, tr.Effect)
			}
			for _, r := range f.Repls {
				chains = append(chains, r.With)
			}
			f.EachSVarAbility(func(sa *cards.SA) { chains = append(chains, sa) })

			// The "the amassed Army's power" half: a face whose SVar table
			// defines the bare `Remembered$CardPower` value head. The
			// `TriggerRemembered$CardPower` spelling (Foray of Orcs,
			// Grishnákh) reads the triggering ability's own remembered list,
			// a different binding, and must not match here.
			powerViaRemembered := false
			for _, body := range f.SVars {
				if strings.HasPrefix(strings.TrimSpace(body), "Remembered$") {
					powerViaRemembered = true
					break
				}
			}

			var got []amassRememberConsumer
			for _, a := range chains {
				for sa := a; sa != nil; sa = sa.Sub {
					if sa.API != "Amass" || sa.Params["RememberAmass"] == "" {
						continue
					}
					for sub := sa.Sub; sub != nil; sub = sub.Sub {
						cons := amassRememberConsumerFor(sub, powerViaRemembered)
						key := fmt.Sprintf("%s|%s|%s|%s", f.Name, cons.api,
							strings.Join(cons.params, ","), strconvBool(cons.powerViaRemembered))
						if seen[key] {
							continue
						}
						seen[key] = true
						got = append(got, cons)
					}
				}
			}
			if len(got) > 0 {
				out[f.Name] = append(out[f.Name], got...)
			}
		}
	}
	for k := range out {
		sort.Slice(out[k], func(i, j int) bool { return out[k][i].api < out[k][j].api })
	}
	return out
}

// amassRememberConsumerFor reports one chained sub-ability as a consumer of
// the remembered Army. api is always the sub's API (so a card whose consumer
// names the Army only through the X value head, like Surrounded by Orcs' Mill,
// is still named); params lists whichever of the shared `...$ Remembered`
// keys the sub itself names; powerViaRemembered marks the face-level
// `X:Remembered$CardPower` read.
func amassRememberConsumerFor(sa *cards.SA, powerViaRemembered bool) amassRememberConsumer {
	cons := amassRememberConsumer{api: sa.API, powerViaRemembered: powerViaRemembered}
	for _, k := range []string{"Defined", "RememberObjects", "DamageSource", "ConditionDefined"} {
		if sa.Params[k] == "Remembered" {
			cons.params = append(cons.params, k+"$ Remembered")
		}
	}
	return cons
}

func strconvBool(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// TestAmassRememberedCensusNamesConsumers pins the corpus's RememberAmass$
// consumers by name. A new RememberAmass$ card (a corpus-pin bump) fails this
// test, naming the card and its consumer, so the new mechanism gets its own
// rules coverage rather than silently joining an untested set.
func TestAmassRememberedCensusNamesConsumers(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	got := amassRememberedConsumers(reg)

	type want struct {
		api    string
		params []string
		power  bool
	}
	wantByCard := map[string]want{
		"Goblin Plate Mail":           {api: "Attach", params: []string{"Defined$ Remembered"}},
		"Foray of Orcs":               {api: "ImmediateTrigger", params: []string{"ConditionDefined$ Remembered", "RememberObjects$ Remembered"}},
		"Grishnákh, Brash Instigator": {api: "ImmediateTrigger", params: []string{"ConditionDefined$ Remembered", "RememberObjects$ Remembered"}},
		"Surrounded by Orcs":          {api: "Mill", power: true},
		"Widespread Brutality":        {api: "DamageAll", params: []string{"ConditionDefined$ Remembered", "DamageSource$ Remembered"}, power: true},
	}

	// Precondition: the walk actually reached the class. A registry that
	// failed to load (or a walk that stopped linking chains) must fail
	// loudly, not pass vacuously.
	if len(got) == 0 {
		t.Fatal("census found no RememberAmass$ consumers at all; the corpus walk is broken")
	}
	goblin, ok := got["Goblin Plate Mail"]
	if !ok {
		t.Fatal("census precondition: Goblin Plate Mail's RememberAmass$->Attach chain was not found")
	}
	foundAttach := false
	for _, c := range goblin {
		if c.api == "Attach" {
			foundAttach = true
		}
	}
	if !foundAttach {
		t.Fatalf("census precondition: Goblin Plate Mail has no Attach consumer, got %+v", goblin)
	}

	// The card set must match exactly: a new consumer card is named, and a
	// stale entry (the card no longer carries the shape) is named too.
	var gotNames, wantNames []string
	for n := range got {
		gotNames = append(gotNames, n)
	}
	for n := range wantByCard {
		wantNames = append(wantNames, n)
	}
	sort.Strings(gotNames)
	sort.Strings(wantNames)
	if strings.Join(gotNames, "; ") != strings.Join(wantNames, "; ") {
		t.Fatalf("RememberAmass$ consumer cards changed:\n got: %v\nwant: %v", gotNames, wantNames)
	}

	// Each card's consumer API and named Remembered params must match. The
	// power-via-Remembered wording is folded in for the two cards whose read
	// is the X value head (its params are empty, so only the flag is checked).
	for name, wantc := range wantByCard {
		var gotAPIs, gotParams []string
		powerSeen := false
		for _, c := range got[name] {
			gotAPIs = append(gotAPIs, c.api)
			gotParams = append(gotParams, c.params...)
			if c.powerViaRemembered {
				powerSeen = true
			}
		}
		if !strings.Contains(strings.Join(gotAPIs, ","), wantc.api) {
			t.Errorf("%s: consumer API %q missing from %v", name, wantc.api, gotAPIs)
		}
		sort.Strings(gotParams)
		wantParams := append([]string(nil), wantc.params...)
		sort.Strings(wantParams)
		if strings.Join(gotParams, ",") != strings.Join(wantParams, ",") {
			t.Errorf("%s: Remembered params = %v, want %v", name, gotParams, wantParams)
		}
		if wantc.power != powerSeen {
			t.Errorf("%s: X:Remembered$CardPower read = %v, want %v", name, powerSeen, wantc.power)
		}
	}
}
