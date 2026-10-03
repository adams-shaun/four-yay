package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// timeLordSrc is a freely-authored Time Lord creature standing in for the
// intervening-if condition TARDIS's own script gates on
// (IsPresent$ Card.Time Lord+YouCtrl). The brief's carrier is TARDIS; the
// creature that satisfies the gate is a fixture.
const timeLordSrc = "Name:Time Lord Fixture\nManaCost:1 U\nTypes:Legendary Creature Time Lord\nPT:1/1\nOracle:x\n"

// planeswalkElectionIDX returns the index of the yes/no option with the given
// Kind in a pending planeswalk election, or -1.
func planeswalkElectionIDX(d *decision.Decision, kind string) int {
	for _, o := range d.Options {
		if o.Kind == kind {
			return o.Index
		}
	}
	return -1
}

// driveTardisToPlaneswalkElection resolves the REAL corpus TARDIS chain body
// through the live Engine and leaves the pending planeswalk election for the
// caller to answer. TARDIS' own attack trigger cannot fire end to end in this
// build: kw:Crew is unsupported (a Vehicle never attacks), and the trigger's
// intervening-if spec `Card.Time Lord+YouCtrl` names a two-word subtype this
// engine's predicate classifier does not recognise (see the Issue filed with
// this ticket). So the test drives the body a live TriggerPush would mint --
// the real `TrigEffect` SVar, the one the trigger's `Execute$` names -- onto
// the stack through a real LOGGED event, `KeywordTriggerPush`, whose Apply
// resolves its Counter as an SVar on the source face. That keeps log-only
// replay exact, and because the SVar body is a distinct *SA object from the
// trigger's compiled Effect pointer, resolveTop's intervening-if recheck
// (which matches a trigger by that exact pointer) does not fizzle the chain.
// The assertions below pin the compiled trigger's own Effect -> Sub linkage,
// so a `cards.Link` regression that dropped the Execute$ chain still fails.
func driveTardisToPlaneswalkElection(t *testing.T, e *Engine, tardis *cards.Card) state.ObjID {
	t.Helper()
	if len(tardis.Faces[0].Triggers) == 0 || tardis.Faces[0].Triggers[0].Effect == nil {
		t.Fatal("TARDIS has no compiled attack trigger")
	}
	realEffect := tardis.Faces[0].Triggers[0].Effect
	realSub := realEffect.Sub
	// The chain linkage this test exists to pin: the compiled trigger's
	// Execute body is a DB$ Effect whose SubAbility$ is the real DBPlaneswalk.
	if realEffect.API != "Effect" || realEffect.Params["SubAbility"] != "DBPlaneswalk" ||
		realSub == nil || realSub.API != "Planeswalk" || !strings.EqualFold(realSub.Params["Optional"], "True") {
		t.Fatalf("TARDIS TrigEffect chain = %+v / sub %+v, want Effect -> Optional$ Planeswalk", realEffect, realSub)
	}
	// A separate SVar lookup must agree with the compiled Sub pointer, so a
	// Link that stopped resolving Execute$ to the SVar body cannot hide.
	if sv := cards.ResolveSVar(tardis.Faces[0].SVars, "DBPlaneswalk"); sv == nil || sv.API != "Planeswalk" {
		t.Fatalf("TARDIS DBPlaneswalk SVar = %+v, want Planeswalk", sv)
	}

	// TARDIS on seat 0's battlefield as the ability's source (the election
	// Note names it), and a Time Lord beside it so the board reflects the
	// card's own condition even though the seeded body does not re-evaluate it.
	tardisID := moveByName(t, e, 0, "TARDIS", state.ZBattlefield)
	if e.G.Obj(tardisID).Zone != state.ZBattlefield {
		t.Fatalf("TARDIS is in %s, want battlefield", e.G.Obj(tardisID).Zone)
	}
	if id := moveByName(t, e, 0, "Time Lord Fixture", state.ZBattlefield); e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatalf("Time Lord fixture is in %s, want battlefield", e.G.Obj(id).Zone)
	}

	ability := cards.ResolveSVar(tardis.Faces[0].SVars, "TrigEffect")
	if ability == nil || ability.API != "Effect" {
		t.Fatalf("TARDIS TrigEffect SVar = %+v, want DB$ Effect", ability)
	}
	// A real, LOGGED event mints the body, so a log-only replay rebuilds the
	// same object: KeywordTriggerPush's Apply resolves its Counter as an SVar
	// on the source face (events/apply.go). findTriggerForAbility matches a
	// trigger by its exact compiled Effect pointer, and this SVar body is a
	// distinct object from that pointer, so resolving the chain this way does
	// not re-run the attack trigger's (unsatisfiable) intervening-if recheck.
	before := len(e.G.Stack)
	e.emit(events.Event{Kind: events.KeywordTriggerPush, Player: 0, Obj: tardisID,
		Counter: "TrigEffect", Text: "planeswalk chain"})
	if len(e.G.Stack) != before+1 {
		t.Fatalf("KeywordTriggerPush left the stack depth at %d, want %d", len(e.G.Stack), before+1)
	}
	e.resolveTop()
	return tardisID
}

// TestPlaneswalkProbeAskResolvesChain is the synthetic complement to the
// carrier pin: the same Optional$ Planeswalk shape on an authored probe,
// cast through the ordinary cast path so the mid-resolution election and the
// chained SubAbility after it are exercised independently of TARDIS's
// unsupported Crew/attack mechanics.
func TestPlaneswalkProbeAskResolvesChain(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	probe := card(t, "Name:Planeswalk Probe\nManaCost:G\nTypes:Sorcery\n"+
		"A:SP$ Planeswalk | Optional$ True | SubAbility$ Next\n"+
		"SVar:Next:DB$ GainLife | LifeAmount$ 1\nOracle:x\n")

	for _, want := range []string{"yes", "no"} {
		t.Run(want, func(t *testing.T) {
			e, cfg := corpusEngineCfg(t, reg, []*cards.Card{probe}, nil)
			probeID := moveByName(t, e, 0, "Planeswalk Probe", state.ZHand)
			if e.G.Obj(probeID).Zone != state.ZHand {
				t.Fatalf("probe is in %s, want hand", e.G.Obj(probeID).Zone)
			}
			life := e.G.Players[0].Life
			addMana(t, e, 0, "G")
			d := castFixture(t, e, probeID, -1)
			if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "planeswalk_optional" {
				t.Fatalf("planeswalk decision = %+v", d)
			}
			idx := planeswalkElectionIDX(d, want)
			if idx < 0 {
				t.Fatalf("no %q option in %+v", want, d.Options)
			}
			submitChoices(t, e, idx)
			passUntilStackEmpty(t, e, 40)
			if e.G.Obj(probeID).Zone != state.ZGraveyard {
				t.Fatalf("probe is in %s, want graveyard", e.G.Obj(probeID).Zone)
			}
			if e.G.Players[0].Life != life+1 {
				t.Fatalf("chained GainLife did not resolve: life %d, want %d", e.G.Players[0].Life, life+1)
			}
			replayCheck(t, e, cfg)
		})
	}
}
