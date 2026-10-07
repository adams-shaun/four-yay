package effects

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// aiRandomAskAPIs are the APIs whose ask carries AILogic$ Random as
// decision.Decision.AIRandom (effects/charm.go): the bots draw such a pick
// from their seeded rng and the R-9 no-ask answer draws from the engine rng
// (aiRandomNoAskPick).
var aiRandomAskAPIs = map[string]bool{"GenericChoice": true, "Charm": true}

// aiRandomRepeatOtherAPI names, by "card/SVar", the AILogic$ Random
// carriers inside a Repeat body whose API is NOT one of aiRandomAskAPIs,
// with the reason each cannot loop. The census holds this table exact.
var aiRandomRepeatOtherAPI = map[string]string{
	// RepeatEach over the defending players: one bounded pass each, no gate.
	"Raging River/DBDefLeftRight": "TwoPiles under a RepeatEach (bounded per player)",
}

// gatedRandomRepeats is the class the Face to Face livelock belongs to: a
// GATE-governed Repeat (RepeatCheckSVar$/RepeatDefined$, re-run until the
// body's answers move the gate) whose body re-poses an AILogic$ Random
// choice. Every pass must draw fresh: a fixed answer moves no gate and the
// loop spins to the 1000 cap. Each member carries a behavioural test of the
// no-ask path (TestFaceToFaceNoAskHostRethrowsATie) and the bot path
// (botpolicy TestAIRandomModesAskDrawsFromTheSeatRng,
// cmd/cardfuzz TestFaceToFaceEndsUnderEveryAutoPayMode); a new member
// fails here until it has one.
var gatedRandomRepeats = []string{"Face to Face"}

// TestAIRandomRepeatCensus walks every corpus Repeat/RepeatEach body
// (through SubAbility$, Choices$, RepeatSubAbility$ and FallbackAbility$)
// and asserts every re-asked random choice advances an rng on every pass:
// AtRandom$ picks are engine-drawn by construction; an AILogic$ Random
// choice must be on an ask that carries AIRandom (so bots and the no-ask
// stand-in draw), or be a named, bounded exception.
func TestAIRandomRepeatCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	gotOther := map[string]bool{}
	var gotGated []string
	for _, card := range reg.AllCards() {
		for _, face := range card.Faces {
			names := make([]string, 0, len(face.SVars))
			for name := range face.SVars {
				names = append(names, name)
			}
			sort.Strings(names)
			gatedMember := false
			for _, name := range names {
				rep := cards.ResolveSVar(face.SVars, name)
				if rep == nil || (rep.API != "Repeat" && rep.API != "RepeatEach") {
					continue
				}
				gated := rep.API == "Repeat" && (strings.TrimSpace(rep.Params["RepeatCheckSVar"]) != "" ||
					strings.TrimSpace(rep.Params["RepeatDefined"]) != "")
				for _, body := range repeatBody(face.SVars, rep.Params["RepeatSubAbility"]) {
					sa := cards.ResolveSVar(face.SVars, body)
					if sa == nil || strings.TrimSpace(sa.Params["AILogic"]) != "Random" {
						continue
					}
					key := face.Name + "/" + body
					if !aiRandomAskAPIs[sa.API] {
						gotOther[key] = true
						if _, ok := aiRandomRepeatOtherAPI[key]; !ok {
							t.Errorf("%s: AILogic$ Random on api:%s inside a Repeat body; its ask does not carry AIRandom, so a fixed answer can re-ask forever", key, sa.API)
						}
						continue
					}
					if !aiLogicRandom(sa) {
						t.Errorf("%s: AILogic$ Random does not parse as AIRandom (CharmOf.AILogicRandom)", key)
					}
					if gated {
						gatedMember = true
					}
				}
			}
			if gatedMember {
				gotGated = append(gotGated, face.Name)
			}
		}
	}
	for key := range aiRandomRepeatOtherAPI {
		if !gotOther[key] {
			t.Errorf("stale aiRandomRepeatOtherAPI entry %s: no longer an AILogic$ Random Repeat-body carrier", key)
		}
	}
	sort.Strings(gotGated)
	if strings.Join(gotGated, "|") != strings.Join(gatedRandomRepeats, "|") {
		t.Errorf("gated Repeat bodies re-asking an AILogic$ Random choice = %q, want %q (a new member needs a no-ask and a bot livelock test)", gotGated, gatedRandomRepeats)
	}
}

// repeatBody returns, in walk order, every SVar name a Repeat body can run.
func repeatBody(svars map[string]string, start string) []string {
	var out []string
	seen := map[string]bool{}
	stack := []string{strings.TrimSpace(start)}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == "" || seen[n] {
			continue
		}
		sa := cards.ResolveSVar(svars, n)
		if sa == nil {
			continue
		}
		seen[n] = true
		out = append(out, n)
		for _, k := range []string{"SubAbility", "Choices", "RepeatSubAbility", "FallbackAbility"} {
			for _, next := range strings.Split(sa.Params[k], ",") {
				stack = append(stack, strings.TrimSpace(next))
			}
		}
	}
	return out
}
