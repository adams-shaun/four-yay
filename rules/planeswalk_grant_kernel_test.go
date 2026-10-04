package rules

// Kernel-era restorations of the tests W3 removed from planeswalk_grant_test.go: the
// same behaviour driven through the resolution kernel (the only ask path).

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestTardisPlaneswalkAskResolvesChain is the real-corpus carrier pin: the
// actual TARDIS compiled attack trigger's Execute$ SA (a `DB$ Effect` whose
// SubAbility$ is DBPlaneswalk) is resolved through the live Engine, and its
// own DBPlaneswalk election is answered yes and no. It fails if the
// TrigEffect -> SubAbility$ DBPlaneswalk linkage, the election resume, or the
// chain continuation regresses.
func TestTardisPlaneswalkAskResolvesChainKernel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	tardis := lookup(t, reg, "TARDIS")
	timeLord := card(t, timeLordSrc)
	realPlaneswalk := cards.ResolveSVar(tardis.Faces[0].SVars, "DBPlaneswalk")
	if realPlaneswalk == nil || realPlaneswalk.API != "Planeswalk" || realPlaneswalk.Params["Optional"] != "True" {
		t.Fatalf("TARDIS DBPlaneswalk = %+v, want Optional$ True Planeswalk", realPlaneswalk)
	}

	for _, want := range []string{"yes", "no"} {
		t.Run(want, func(t *testing.T) {
			e, cfg := corpusEngineCfg(t, reg, []*cards.Card{tardis, timeLord}, nil)
			tardisID := kr7DriveTardisToPlaneswalkElection(t, e, tardis)
			d := e.Pending()
			if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "planeswalk_optional" {
				t.Fatalf("TARDIS chain did not reach the planeswalk election: %+v", d)
			}
			life := e.G.Players[0].Life
			idx := planeswalkElectionIDX(d, want)
			if idx < 0 {
				t.Fatalf("no %q option in %+v", want, d.Options)
			}
			submitChoices(t, e, idx)
			kr7Settle(e)
			passUntilStackEmpty(t, e, 40)
			// The chain completes: the seeded body and its Sub leave the stack.
			if len(e.G.Stack) != 0 {
				t.Fatalf("stack not empty after the chain: %v", e.G.Stack)
			}
			// TARDIS itself is untouched (the Effect only grants cascade).
			if e.G.Obj(tardisID).Zone != state.ZBattlefield {
				t.Fatalf("TARDIS left the battlefield: %s", e.G.Obj(tardisID).Zone)
			}
			if e.G.Players[0].Life != life {
				t.Fatalf("planeswalk moved life: %d, want %d", e.G.Players[0].Life, life)
			}
			var elected, noDeck bool
			for _, event := range e.L.Events {
				if event.Kind != events.Note {
					continue
				}
				switch event.Text {
				case "planeswalk election: " + want:
					elected = true
				case "planeswalk (no planar deck)":
					noDeck = true
				}
				if strings.HasPrefix(event.Text, "unimplemented API") {
					t.Fatalf("Planeswalk reached generic fallback: %q", event.Text)
				}
			}
			if !elected {
				t.Fatalf("no %q election Note in the log", want)
			}
			if want == "yes" && !noDeck {
				t.Fatal("a yes election must still record the no-planar-deck no-op")
			}
			// The decline contract: a declined "you may planeswalk" records
			// the election and nothing else. Emitting the no-op Note here
			// would claim a planeswalk resolved and found no planar deck.
			if want == "no" && noDeck {
				t.Fatal("a declined election must not record the no-planar-deck no-op")
			}
			replayCheck(t, e, cfg)
		})
	}
}

// kr7DriveTardisToPlaneswalkElection is driveTardisToPlaneswalkElection
// resolving the chain with no priority decision pending.
func kr7DriveTardisToPlaneswalkElection(t *testing.T, e *Engine, tardis *cards.Card) state.ObjID {
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
	e.pending = nil // resolve directly, outside the pending priority window
	e.resolveTop()
	return tardisID
}
