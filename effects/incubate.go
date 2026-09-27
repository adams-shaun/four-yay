package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("Incubate", effIncubate) }

// incubatorTokenKey is the Forge tokenscript stem of the standard Incubator
// token (.cards/tokenscripts/incubator_c_0_0_a_phyrexian.txt): an Artifact
// Incubator with the "{2}: Transform this token." activated ability
// (AB$ SetState | Cost$ 2 | Mode$ Transform) whose ALTERNATE face is the
// 0/0 Phyrexian artifact creature. Every corpus DB$ Incubate line creates
// this token (no carrier carries a TokenScript$ of its own -- measured at
// the corpus pin), so the primitive defaults to it and still honours an
// explicit TokenScript$ parameter should a future script name a variant
// (the corpus ships incubator_dark_confidant for exactly such a variant).
const incubatorTokenKey = "incubator_c_0_0_a_phyrexian"

// effIncubate implements the Incubate primitive (CR 701.57a-b, 30 corpus
// DB$ lines over 29 files): create an Incubator token with Amount$
// +1/+1 counters on it. The token's "{2}: Transform" half needs no engine
// support here -- the token script carries the AB$ SetState ability itself,
// and effSetState's FlipFace is what transforms the object; the +1/+1
// counters stay on the object through the flip (they live on
// state.Object, never on the face), so an Incubator with two counters
// transforms into a 2/2 Phyrexian artifact creature, exactly the oracle
// text.
//
// Amount$ resolves through the ordinary Num grammar, so the deck carrier's
// dynamic form (Chrome Host Seedshark's
// `Amount$ TriggeredSpellAbility$CardManaCostLKI` -- the cast spell's mana
// value) and the X form (Sunfall's "Incubate X") both resolve; a present
// but unresolvable value degrades to zero per Num's convention. A zero or
// negative amount still creates the token -- "incubate X" with X = 0 is a
// real Incubator token with no counters (Sunfall exiling zero creatures),
// and the CounterChange event is simply skipped -- matching Forge's
// IncubateEffect, which mints the token before applying counters.
//
// Times$ (Elesh Norn's back face "Incubate 2 five times", Glissa's "twice",
// Progenitor Exarch's Times$ X) repeats the whole create-and-counter step
// one token per repeat, in deterministic repeat order.
//
// Owner: the resolving controller by default. The one carrier that
// redirects (Excise the Imperfect's `Defined$ TargetedController`) resolves
// through the ordinary Defined machinery -- the targeted permanent's
// controller is the incubating player. A Defined$ that resolves to no
// player (an unbound selector binding) keeps the controller rather than
// creating nothing, since the corpus's only redirect always binds when its
// parent spell had a legal target.
//
// Token script: an unknown key is a loud Note and creates nothing, the same
// degrade effToken uses for an unknown TokenScript$.
//
// The mint is the ordinary TokenCreate event (so token-replacement
// machinery -- Doubling Season, Academy Manufactor -- sees it exactly as it
// sees every other mint) and the counters are the ordinary CounterChange
// event, the same two-event shape effAmass uses for its Army.
func effIncubate(h Host, c *Ctx, sa *cards.SA) {
	if rest := resumingMint(c, sa); rest != nil && len(rest.Players) == 1 {
		// A repeat's mint parked behind a CR 616.1 order ask and the
		// answer has minted it: counter it, then run the repeats after it
		// with the values the first pass resolved.
		parked := rest.Parked
		if parked == nil {
			parked = []state.ObjID{}
		}
		incubateLoop(h, c, sa, rest.Players[0], rest.Script, rest.Amount, rest.Count, int32(rest.Next), parked)
		return
	}
	g := h.Game()
	n := Num(h, c, sa, "Amount", 1)
	if n < 0 {
		return
	}
	owner := c.Controller
	if def := strings.TrimSpace(sa.Params["Defined"]); def != "" {
		for _, t := range Defined(h, c, sa) {
			if t.IsPlayer {
				owner = state.PlayerID(t.Player)
				break
			}
			if o := g.Obj(t.Obj); o != nil {
				owner = o.Controller
				break
			}
		}
	}
	key := incubatorTokenKey
	if ts := strings.TrimSpace(sa.Params["TokenScript"]); ts != "" {
		key = ts
	}
	if _, ok := g.Tokens[key]; !ok {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "Incubate: no Incubator token script available (" + key + ")"})
		return
	}
	times := Num(h, c, sa, "Times", 1)
	if times < 1 {
		times = 1
	}
	incubateLoop(h, c, sa, owner, key, n, times, 0, nil)
}

// incubateLoop is effIncubate's create-and-counter repeat from repeat start.
// parked (on a TokenRest re-entry) is what the answer minted for repeat
// start, whose mint parked the resolution behind a CR 616.1 order ask; a
// repeat whose mint parks hands the rest of the loop to the host the same
// way. The counters land on the repeat's first minted object, as they
// always have.
func incubateLoop(h Host, c *Ctx, sa *cards.SA, owner state.PlayerID, key string, n, times int32, start int32, parked []state.ObjID) {
	g := h.Game()
	for i := start; i < times; i++ {
		want := g.NextID
		if parked != nil && i == start {
			if len(parked) == 0 {
				return
			}
			want = parked[0]
		} else {
			wasSuspended := h.Suspended()
			minted := h.EmitTokenCreate(events.Event{Kind: events.TokenCreate, Player: owner, Text: key})
			if !wasSuspended && h.Suspended() && len(minted) == 0 &&
				suspendMint(h, c, TokenRest{SA: sa, Next: int(i), Players: []state.PlayerID{owner},
					Script: key, Amount: n, Count: times}) {
				return
			}
		}
		if o := g.Obj(want); o == nil {
			// The mint folded nowhere (an invalid owner): nothing to
			// counter, and stopping the repeat keeps the loop total.
			return
		}
		if n > 0 {
			h.Emit(events.Event{Kind: events.CounterChange, Obj: want,
				Counter: "P1P1", Amount: n})
		}
	}
}
