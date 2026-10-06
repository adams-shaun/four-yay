package templates

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

func optionalCostReq(t *testing.T, reg *cards.Registry, name string) levelb.Requirement {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("%s not in the corpus", name)
	}
	for _, r := range levelb.Requirements(c) {
		if r.Sub == "static.optional-cost" {
			return r
		}
	}
	t.Fatalf("precondition: %s has no static.optional-cost requirement", name)
	return levelb.Requirement{}
}

// optionalCostRun replays it with its cast step's cast_mode as given and
// returns the final snapshot.
func optionalCostRun(t *testing.T, reg *cards.Registry, it oraclegen.Item, castIdx int, mode string) rules.OracleSnapshot {
	t.Helper()
	sc := it.Scenario
	sc.Steps = append([]oraclegen.Step(nil), sc.Steps...)
	sc.Steps[castIdx].CastMode = mode
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Snapshots) == 0 {
		t.Fatalf("scenario (cast_mode %q) does not replay", mode)
	}
	return res.Snapshots[len(res.Snapshots)-1]
}

func hasName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// TestStaticOptionalCostServed: each cost kind is served with the cast step's
// cast_mode set, a leading "yes" for XMage's "pay it?" ask (never the "no" the
// level-A cast scripts), and the cost's own trace visible in gorge's final
// snapshot while the same cast declined shows none of it.
func TestStaticOptionalCostServed(t *testing.T) {
	reg := loadGenRegistry(t)
	cases := []struct {
		card string
		// picks are the answers that follow the yes.
		picks []string
		// trace asserts the cost's own trace in the paid snapshot and its
		// absence in the declined one.
		trace func(t *testing.T, paid, declined rules.OracleSnapshot)
	}{
		{"Analyze the Pollen", nil, func(t *testing.T, paid, declined rules.OracleSnapshot) {
			if !hasName(declined.Players[0].Graveyard, "Colossal Dreadmaw") || hasName(declined.Players[0].Exile, "Colossal Dreadmaw") {
				t.Fatalf("precondition: declined cast must leave the evidence in the graveyard: gy=%v exile=%v", declined.Players[0].Graveyard, declined.Players[0].Exile)
			}
			if !hasName(paid.Players[0].Exile, "Colossal Dreadmaw") || !hasName(paid.Players[0].Exile, "Craw Wurm") {
				t.Errorf("paid cast must exile the evidence: exile=%v", paid.Players[0].Exile)
			}
		}},
		{"Burning Curiosity", []string{"Hill Giant"}, func(t *testing.T, paid, declined rules.OracleSnapshot) {
			blight := func(s rules.OracleSnapshot) int32 {
				for _, p := range s.Permanents {
					if p.Name == "Hill Giant" {
						n := int32(0)
						for _, c := range p.Counters {
							n += c
						}
						return n
					}
				}
				t.Fatalf("precondition: Hill Giant is not on the battlefield")
				return 0
			}
			if got := blight(declined); got != 0 {
				t.Fatalf("precondition: declined cast blighted %d", got)
			}
			if got := blight(paid); got != 1 {
				t.Errorf("paid cast put %d counters on Hill Giant, want 1", got)
			}
		}},
		{"Dispelling Exhale", []string{"Shivan Dragon"}, func(t *testing.T, paid, declined rules.OracleSnapshot) {
			// The Dragon is revealed, not moved; the observable trace is the
			// spell's paid branch, which leaves a different board.
			if !hasName(paid.Players[0].Hand, "Shivan Dragon") {
				t.Errorf("precondition: the beheld Dragon left the hand: %v", paid.Players[0].Hand)
			}
		}},
		{"Ruinous Waterbending", nil, func(t *testing.T, paid, declined rules.OracleSnapshot) {
			if len(declined.Players[0].Pool) <= len(paid.Players[0].Pool) {
				t.Errorf("paid cast must spend the extra {4} from the pool: paid %q declined %q", paid.Players[0].Pool, declined.Players[0].Pool)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.card, func(t *testing.T) {
			it, skip := GenerateB(reg, tc.card, optionalCostReq(t, reg, tc.card))
			if skip != nil {
				t.Fatalf("skipped: %s", skip.Reason)
			}
			castIdx := -1
			for i, st := range it.Scenario.Steps {
				if st.Op == "cast" && st.Card == "p0:"+tc.card {
					castIdx = i
				}
			}
			if castIdx < 0 {
				t.Fatalf("no cast step for the card: %+v", it.Scenario.Steps)
			}
			if got := it.Scenario.Steps[castIdx].CastMode; got != "optionalcost" {
				t.Fatalf("cast_mode = %q, want optionalcost", got)
			}
			if len(it.XAnswers) != len(it.Scenario.Steps) {
				t.Fatalf("xmage_answers has %d steps, scenario %d", len(it.XAnswers), len(it.Scenario.Steps))
			}
			ans := it.XAnswers[castIdx]
			if len(ans) == 0 || ans[0].Kind != "choice" || ans[0].Value != "yes" {
				t.Fatalf("cast step answers must lead with the yes: %+v", ans)
			}
			for _, a := range ans {
				if a.Value == "no" {
					t.Errorf("the paid cast also queues a decline: %+v", ans)
				}
			}
			for i, p := range tc.picks {
				if i+1 >= len(ans) || ans[i+1].Value != p {
					t.Errorf("answer %d = %+v, want pick %q (answers %+v)", i+1, ans, p, ans)
				}
			}
			paid := optionalCostRun(t, reg, it, castIdx, "optionalcost")
			declined := optionalCostRun(t, reg, it, castIdx, "")
			pj, _ := json.Marshal(paid)
			dj, _ := json.Marshal(declined)
			if string(pj) == string(dj) {
				t.Fatalf("precondition: the paid and declined casts end in the same snapshot")
			}
			tc.trace(t, paid, declined)
		})
	}
}

// TestStaticOptionalCostNamedSkips: the compound cost gets a named skip, never
// the generic gap, and the level-A cast of a served card keeps its bytes.
func TestStaticOptionalCostNamedSkips(t *testing.T) {
	reg := loadGenRegistry(t)
	_, skip := GenerateB(reg, "Celestial Reunion", optionalCostReq(t, reg, "Celestial Reunion"))
	if skip == nil {
		t.Fatalf("Celestial Reunion: served, want a named skip")
	}
	if !strings.HasPrefix(skip.Reason, "static OptionalCost ") || !strings.Contains(skip.Reason, "compound cost") {
		t.Errorf("skip %q, want static OptionalCost ... compound cost", skip.Reason)
	}
}

// TestStaticOptionalCostLevelABytesUnchanged: a level-A cast item carries no
// cast_mode key, so every existing item stays byte-identical.
func TestStaticOptionalCostLevelABytesUnchanged(t *testing.T) {
	reg := loadGenRegistry(t)
	it, skip := Generate(reg, "Analyze the Pollen")
	if skip != nil {
		t.Fatalf("level-A cast skipped: %s", skip.Reason)
	}
	b, _ := json.Marshal(it)
	if strings.Contains(string(b), "cast_mode") {
		t.Errorf("level-A item carries cast_mode: %s", b)
	}
	paidB, skipB := ItemFor(reg, "Analyze the Pollen", "static#0.0")
	if skipB != nil {
		t.Fatalf("precondition: level-B item skipped: %s", skipB.Reason)
	}
	pb, _ := json.Marshal(paidB)
	if !strings.Contains(string(pb), `"cast_mode":"optionalcost"`) {
		t.Errorf("precondition: the level-B item does not carry cast_mode: %s", pb)
	}
}
