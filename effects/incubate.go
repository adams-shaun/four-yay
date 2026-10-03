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
// machinery -- Doubling Season, Anointed Procession -- sees it exactly as it
// sees every other mint) and the counters are the ordinary CounterChange
// event. Because a CreateToken replacement can rewrite one would-be token
// into several mints, the mint goes through h.EmitTokenCreate and the
// counters land on EVERY mint the plan produced, not just the first: each
// mint's CounterChange is its own event, so an AddCounter replacement
// doubles that mint's counters independently (CR 614.5/616.1e).
func effIncubate(h Host, c *Ctx, sa *cards.SA) {
	if rest := resumingMint(c, sa); rest != nil && len(rest.Players) == 1 {
		// A repeat's mint parked behind a CR 616.1 order ask and the
		// answer has minted it: counter it, then run the repeats after it
		// with the values the first pass resolved.
		parked := rest.Parked
		if parked == nil {
			parked = []state.ObjID{}
		}
		incubateLoop(h, c, sa, rest.Players[0], rest.Script, rest.Amount, rest.Count, int32(rest.Next), parked, len(rest.Minted) > 0)
		return
	}
	g := h.Game()
	n := Num(h, c, sa, "Amount", 1)
	if n < 0 {
		return
	}
	owner := c.Controller
	if def := strings.TrimSpace(sa.ParamStr(cards.PKDefined)); def != "" {
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
	if ts := strings.TrimSpace(sa.ParamStr(cards.PKTokenScript)); ts != "" {
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
	incubateLoop(h, c, sa, owner, key, n, times, 0, nil, false)
}

// incubateLoop is effIncubate's create-and-counter repeat from repeat start.
// parked (on a TokenRest re-entry) is what the answer minted for repeat
// start, whose mint parked the resolution behind a CR 616.1 order ask; a
// repeat whose mint parks hands the rest of the loop to the host the same
// way. EmitTokenCreate returns EVERY object the emit created -- an ordinary
// emit the single mint, a CreateToken replacement the whole rewritten plan
// (Doubling Season's pair, Anointed Procession's pair) -- so the counters
// land on every mint the resolution actually produced: before the park, and
// on the re-entry alike.
func incubateLoop(h Host, c *Ctx, sa *cards.SA, owner state.PlayerID, key string, n, times int32, start int32, parked []state.ObjID, countered bool) {
	g := h.Game()
	for i := start; i < times; i++ {
		if parked != nil && i == start {
			// The parked repeat: the answer minted it (or the rest of its
			// rewritten plan), so its counters are owed. A repeat whose
			// first mints landed and took their counters before the park
			// (Minted, countered) owes only what the answer minted.
			if len(parked) == 0 {
				if countered {
					// The repeat's mints landed (and took their counters)
					// before the rest of its plan parked, and the answer
					// minted nothing more.
					continue
				}
				// Nothing landed and the answer minted nothing: the plan
				// rounded to zero; stopping the repeat keeps the loop
				// total.
				return
			}
			for _, want := range parked {
				if g.Obj(want) == nil {
					continue
				}
				if n > 0 {
					h.Emit(events.Event{Kind: events.CounterChange, Obj: want,
						Counter: "P1P1", Amount: n})
				}
			}
			continue
		}
		wasSuspended := h.Suspended()
		minted := h.EmitTokenCreate(events.Event{Kind: events.TokenCreate, Player: owner, Text: key})
		if !wasSuspended && h.Suspended() {
			// The mint parked the resolution behind a replacement-order ask.
			// Every mint that landed takes its counters now (each its own
			// event, so an AddCounter replacement doubles it independently),
			// and the rest of the loop -- this repeat's remaining mints and
			// the repeats after it -- resumes with the answer, so no later
			// repeat is logged ahead of this one's parked mints.
			var landed []state.ObjID
			for _, id := range minted {
				if g.Obj(id) == nil {
					continue
				}
				if n > 0 {
					h.Emit(events.Event{Kind: events.CounterChange, Obj: id,
						Counter: "P1P1", Amount: n})
				}
				landed = append(landed, id)
			}
			if suspendMint(h, c, TokenRest{SA: sa, Next: int(i), Minted: landed, Players: []state.PlayerID{owner},
				Script: key, Amount: n, Count: times}) {
				return
			}
			if len(landed) > 0 {
				continue
			}
			return
		}
		if len(minted) == 0 {
			// The mint folded nowhere (an invalid owner): nothing to
			// counter, and stopping the repeat keeps the loop total.
			return
		}
		for _, want := range minted {
			if g.Obj(want) == nil {
				continue
			}
			if n > 0 {
				h.Emit(events.Event{Kind: events.CounterChange, Obj: want,
					Counter: "P1P1", Amount: n})
			}
		}
	}
}
