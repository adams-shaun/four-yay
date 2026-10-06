package rules

// cast_limit_each_turn_test.go pins CantBeCast's NumLimitEachTurn$ rider
// (High Noon: "Each player can't cast more than one spell each turn"): the
// restriction refuses the SECOND spell a caster casts in a turn, counted per
// caster, and never the first.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

const highNoonSecondSpell = `{"name":"high-noon-limit","cr":["601.2"],"why":"inline","setup":{"p0":{"battlefield":["High Noon"],"hand":["Ornithopter","Memnite"]},"p1":{"hand":["Gut Shot"]}},"steps":[` +
	`{"op":"cast","seat":0,"card":"p0:Ornithopter"},{"op":"resolve"},` +
	`{"op":"pass_to","seat":0,"decision":"priority","expect":[{"offered":{"seat":0,"kind":"cast","card":"p0:Memnite"},"want":false}]},` +
	`{"op":"pass","seat":0,"expect":[{"offered":{"seat":1,"kind":"cast","card":"p1:Gut Shot"},"want":true}]}]}`

func TestCastLimitEachTurnRefusesOnlyTheSecondSpell(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(highNoonSecondSpell))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	// Precondition: the first spell really resolved onto the battlefield, so
	// the withheld Memnite is the second-spell limit and not a blanket ban.
	last := res.Snapshots[len(res.Snapshots)-1]
	if _, ok := snapPerm(last, "p0:Ornithopter"); !ok {
		t.Fatalf("Ornithopter is not on p0's battlefield after the first cast: %+v", last.Permanents)
	}
}

// Deafening Silence limits NONCREATURE spells only: a creature spell cast first
// must not use up the allowance, so the first noncreature spell stays offered.
const deafeningSilenceCreatureFirst = `{"name":"deafening-silence-limit","cr":["601.2"],"why":"inline","setup":{"p0":{"battlefield":["Deafening Silence"],"hand":["Ornithopter","Gut Shot"]}},"steps":[` +
	`{"op":"cast","seat":0,"card":"p0:Ornithopter"},{"op":"resolve"},` +
	`{"op":"pass_to","seat":0,"decision":"priority","expect":[{"offered":{"seat":0,"kind":"cast","card":"p0:Gut Shot"},"want":true}]}]}`

func TestCastLimitEachTurnCountsOnlyTheValidCardSpells(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(deafeningSilenceCreatureFirst))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	last := res.Snapshots[len(res.Snapshots)-1]
	if _, ok := snapPerm(last, "p0:Deafening Silence"); !ok {
		t.Fatalf("Deafening Silence is not on the battlefield: %+v", last.Permanents)
	}
	if _, ok := snapPerm(last, "p0:Ornithopter"); !ok {
		t.Fatalf("Ornithopter did not resolve: %+v", last.Permanents)
	}
}
