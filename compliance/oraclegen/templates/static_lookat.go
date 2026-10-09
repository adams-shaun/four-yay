// Level-B observation of a Continuous MayLookAt$ static that lookAtLibraryTop
// (static_legality_host.go) cannot serve: the permission is gated on the
// source's own Case being solved, or the permission is over a face-down
// battlefield PERMANENT rather than the library top. Both are hidden
// information no snapshot field carries, so the item asserts gorge's own
// permission read (the runner's look_at_library_top / look_at expectation,
// frozen into `fails` like can_block), beside a control without the source
// where the permission is absent.
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// lookAtSolvedItem serves a MayLookAt$ static gated on its own Case being
// solved (IsPresent$ Card.Self+IsSolved, the MKM Case cycle's "Solved --"
// rows): the gate is a state no setup reaches, but the Case solves ITSELF at
// the end step, so the scenario places exactly what the Case's own
// Mode$ Phase | End of Turn solve trigger counts (the shared static count
// fixture: seven lands for Case of the Locked Hothouse's GE7), passes through
// the end step where the solve trigger fires and resolves, and reads the
// look-at permission at p1's next-turn begin-combat -- the first checkpoint
// after the solve. The control keeps the counted permanents but drops the
// Case, so nothing solves and the permission stays absent. ok is false when
// the static is not IsSolved-gated or the solve is not reachable, leaving the
// row to its named look-at skip.
func lookAtSolvedItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	if !strings.Contains(strings.ToLower(st.ParamStr(cards.PKIsPresent)), "issolved") {
		return oraclegen.Item{}, false
	}
	lands, ok := caseSolveCount(reg, f)
	if !ok {
		return oraclegen.Item{}, false
	}
	steps := func(want bool) []oraclegen.Step {
		return []oraclegen.Step{
			{Op: "pass_to", Seat: 0, Step: "end"},
			{Op: "resolve", Seat: 0},
			{Op: "pass_to", Seat: 1, Step: "begin-combat", Active: "p1",
				Expect: []oraclegen.Expect{{LookAtLibraryTop: map[string]bool{"p0": want}}}},
		}
	}
	scenario := func(withCard, want bool) oraclegen.Scenario {
		bf := append([]string(nil), lands...)
		if withCard {
			bf = append([]string{name}, bf...)
		}
		// No SetupAnswers, like the Case's own trigger row: the scenario
		// casts nothing, and a setup-only scenario replays in XMage without
		// opening-hand answers (the trigger#0.0 verdict's shape).
		sc := oraclegen.Scenario{
			Setup: map[string]oraclegen.Seat{
				"p0": {Battlefield: bf},
				"p1": {},
			},
			Steps: steps(want),
		}
		oraclegen.Baseline(sc.Setup, f)
		return sc
	}
	if res, ok := runStatic(reg, scenario(false, false)); !ok || len(res.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	sc := withBackFace(scenario(true, true), name, req)
	it, skip := finishLegalityItem(reg, f, name, "MayLookAt", req, sc, []string{"401.2", "719.3b"})
	return it, skip == nil
}

// caseSolveCount is the permanents the face's own Case-solve trigger counts,
// by the shared static count fixture: the Mode$ Phase | End of Turn trigger
// whose !IsSolved gate is unsolved-only and whose Execute body is an
// AlterAttribute Solved solve. ok is false for a Case whose solve counts
// something the fixture cannot place (a token, an event history), leaving
// that row in its named look-at skip.
func caseSolveCount(reg *cards.Registry, f *cards.Face) ([]string, bool) {
	for i := range f.Triggers {
		t := &f.Triggers[i]
		if !strings.EqualFold(strings.TrimSpace(t.ParamStr(cards.PKPhase)), "End of Turn") {
			continue
		}
		if !strings.Contains(strings.ToLower(t.ParamStr(cards.PKIsPresent)), "!issolved") {
			continue
		}
		exec := f.SVars[strings.TrimSpace(t.ParamStr(cards.PKExecute))]
		lower := strings.ToLower(exec)
		if !strings.Contains(lower, "alterattribute") || !strings.Contains(lower, "solved") {
			continue
		}
		check := strings.TrimSpace(t.ParamStr(cards.PKCheckSVar))
		if check == "" {
			continue
		}
		fx, ok := staticBodyFixture(reg, svarBody(f, check), staticCountFrom(t.ParamStr(cards.PKSVarCompare)))
		if !ok || len(fx.steps) > 0 || len(fx.hand) > 0 || len(fx.graveyard) > 0 {
			return nil, false
		}
		return fx.battlefield, true
	}
	return nil, false
}

// lookAtFaceDownItem serves a MayLookAt$ static whose Affected$ spec names
// face-down creatures (Found Footage, Keeper of the Lens): the permission is
// over a face-down battlefield PERMANENT, not the library top, so the
// observation is the runner's look_at expectation over a face-down probe. p1
// casts a plain Disguise card face down for the family-independent {3} and
// never turns it up; p0 -- the static's controller, the source setup-placed
// on its battlefield -- may look at the probe at p1's next begin-combat, and
// the identical checkpoint without the source must not. ok is false when the
// static's Affected$ does not name a face-down probe or no probe cast
// replays, leaving the row to its named look-at skip.
func lookAtFaceDownItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	if !strings.Contains(strings.ToLower(st.ParamStr(cards.PKAffected)), "facedown") {
		return oraclegen.Item{}, false
	}
	probe := faceDownProbe(reg, name)
	if probe == "" {
		return oraclegen.Item{}, false
	}
	steps := func(want bool) []oraclegen.Step {
		return []oraclegen.Step{
			{Op: "pass_to", Seat: 1, Step: "main1", Active: "p1"},
			{Op: "cast", Seat: 1, Card: cardAt(1, probe), Mana: faceDownCastPool, CastMode: "disguised"},
			{Op: "resolve", Seat: 1},
			// No Active: p1's begin-combat is this turn's, one pass away, and
			// the XMage driver's active-bearing pass_to would advance the turn
			// a second time (its same-seat pass_to skips a full turn).
			{Op: "pass_to", Seat: 1, Step: "begin-combat",
				Expect: []oraclegen.Expect{{LookAt: map[string]string{"p0": cardAt(1, probe)}, Want: boolPtr(want)}}},
		}
	}
	scenario := func(withCard, want bool) oraclegen.Scenario {
		var bf []string
		if withCard {
			bf = []string{name}
		}
		sc := staticScenario(f, name, bf, nil, steps(want))
		p1 := sc.Setup["p1"]
		p1.Hand = appendFixtureUnique(p1.Hand, probe)
		sc.Setup["p1"] = p1
		return sc
	}
	if res, ok := runStatic(reg, scenario(false, false)); !ok || len(res.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	sc := withBackFace(scenario(true, true), name, req)
	it, skip := finishLegalityItem(reg, f, name, "MayLookAt", req, sc, []string{"708.6"})
	return it, skip == nil
}

// faceDownProbe is the plain Disguise card the scenario casts face down, the
// first probe that is neither the card under test nor absent from the corpus.
func faceDownProbe(reg *cards.Registry, name string) string {
	for _, cand := range turnedFaceUpDisguiseProbes {
		if cand == name {
			continue
		}
		c, ok := reg.Lookup(cand)
		if !ok || len(c.Faces) == 0 {
			continue
		}
		if _, ok := c.Faces[0].KeywordParam("Disguise"); !ok {
			continue
		}
		return cand
	}
	return ""
}
