package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("Empower", effEmpower) }

// empowerTokenKey is the Forge tokenscript stem of the generic empower token
// (.cards/tokenscripts/u_empower.txt): a blue 0-loyalty Planeswalker with
// "[-1]: Surveil 1." and "[-3]: Draw a card.". Forge's EmpowerEffect first
// tries "u_empower_<lowercased Type$>" and falls back to this stem, then
// adds the Type$ word to the token and names it "<Type$> Token" (CR 111.4).
// The corpus ships no per-type variant at the pin, so every carrier lands on
// the fallback; the per-type key is still tried first, exactly as Forge does.
const empowerTokenKey = "u_empower"

// effEmpower implements Reality Fracture's empower keyword action (35 corpus
// carriers, every one `Empower | Type$ Jace | Num$ <n|X|SVar>`; reminder:
// "Put N loyalty counters on a Jace token you control. If you don't control
// one, first create a blue Jace planeswalker token with "[-1]: Surveil 1"
// and "[-3]: Draw a card.""), following Forge's EmpowerEffect:
//
//  1. N is Num$ (default 1), read BEFORE the token is created -- Overwrite
//     the Multiverse's X (Remembered$Amount), Avatar's creature count and
//     Violent Echoes' Excess SVar are all fixed at that point. A negative N
//     places nothing.
//  2. The empowering player is the resolving controller (Forge's default
//     Defined$ You; no carrier names another player). If they control no
//     TOKEN that is a <Type$> -- read through the layer-aware type test, so a
//     non-token Jace planeswalker (Jace, Reality Sculptor) never qualifies --
//     a token is created first, even when N is 0 (that token then has 0
//     loyalty and dies to state-based actions, as in Forge).
//  3. The created token gets the Type$ word and the CR 111.4 name
//     "<Type$> Token" as permanent self-sourced layer-4 / layer-3 grants
//     (the effAmass "it's also a <Type>" precedent): they live as long as the
//     token does. They are not copiable values, so a COPY of the token would
//     lack the Jace type -- no corpus card copies a planeswalker token.
//  4. N loyalty counters go on ONE Jace token the player controls. With
//     several candidates (a pre-existing pair, or the two mints a CreateToken
//     doubler such as Anointed Procession makes of the one token) the player
//     chooses: a KChoose answered through the shared "counter_pick" resume
//     arm. The R-9 no-host fallback (and botpolicy's first-option answer)
//     take the first candidate in battlefield order. The counters are one
//     ordinary CounterChange event, so AddCounter replacements (Doubling
//     Season) and "whenever you put one or more loyalty counters" triggers
//     see a normal placement.
//
// An absent Type$ or a missing token script is a loud Note that creates and
// places nothing.
func effEmpower(h Host, c *Ctx, sa *cards.SA) {
	// fx42 scoping: consume the answered pick first, so a nested
	// PutCounter/Empower later in the chain cannot inherit it.
	pickAns, pickDone := c.CounterPick, c.CounterPickDone
	c.CounterPick, c.CounterPickDone = nil, false
	g := h.Game()
	typ := strings.TrimSpace(sa.ParamStr(cards.PKType))
	if typ == "" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "Empower: no Type$ to empower"})
		return
	}
	if rest := resumingMint(c, sa); rest != nil {
		// The token's mint parked behind a CR 616.1 replacement-order ask and
		// the answer has minted it: grant the riders to every mint the answer
		// produced, then place the counters with the amount frozen on the
		// first pass.
		var mints []state.ObjID
		for _, id := range rest.Parked {
			if g.Obj(id) != nil {
				empowerGrant(h, id, typ)
				mints = append(mints, id)
			}
		}
		empowerPlace(h, c, sa, typ, rest.Amount, mints, nil, false)
		return
	}
	n := Num(h, c, sa, "Num", 1)
	if n < 0 {
		n = 0
	}
	if pickDone {
		// Re-entry after the "which Jace token" answer: the token already
		// exists (the first pass created it or found it), so nothing is
		// created again.
		empowerPlace(h, c, sa, typ, n, nil, pickAns, true)
		return
	}
	if len(empowerCandidates(h, c, typ)) > 0 {
		empowerPlace(h, c, sa, typ, n, nil, nil, false)
		return
	}
	key := empowerTokenKey + "_" + strings.ToLower(typ)
	if _, ok := g.Tokens[key]; !ok {
		key = empowerTokenKey
	}
	if _, ok := g.Tokens[key]; !ok {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "Empower: no empower token script available (" + key + ")"})
		return
	}
	wasSuspended := h.Suspended()
	mints := h.EmitTokenCreate(events.Event{Kind: events.TokenCreate, Player: c.Controller, Text: key})
	var live []state.ObjID
	for _, id := range mints {
		if g.Obj(id) != nil {
			empowerGrant(h, id, typ)
			live = append(live, id)
		}
	}
	if !wasSuspended && h.Suspended() {
		// The mint parked behind a replacement-order ask: the counters wait
		// for the mints the answer produces (TokenRest re-entry above).
		_ = suspendMint(h, c, TokenRest{SA: sa, Amount: n, Script: key})
		return
	}
	empowerPlace(h, c, sa, typ, n, live, nil, false)
}

// empowerGrant makes a freshly minted empower token a <typ> named
// "<typ> Token": permanent self-sourced grants that end with the token.
func empowerGrant(h Host, id state.ObjID, typ string) {
	o := h.Game().Obj(id)
	if o == nil {
		return
	}
	h.AddContinuous(state.ContinuousEffect{
		Source: id, Controller: o.Controller, Affects: "Card.Self",
		Layer: state.LType, AddTypes: []string{typ}, Permanent: true,
	})
	h.AddContinuous(state.ContinuousEffect{
		Source: id, Controller: o.Controller, Affects: "Card.Self",
		Layer: state.LText, SetName: typ + " Token", Permanent: true,
	})
}

// empowerCandidates is every battlefield token the resolving controller
// controls whose current types include typ, in battlefield order.
func empowerCandidates(h Host, c *Ctx, typ string) []state.ObjID {
	g := h.Game()
	sc := c.SpecContext(c.Controller)
	var out []state.ObjID
	for _, id := range g.Zone(state.ZBattlefield, c.Controller) {
		o := g.Obj(id)
		if o == nil || !o.IsToken || o.Controller != c.Controller || o.Face() == nil {
			continue
		}
		if hasTypeCtx(o, typ, sc) {
			out = append(out, id)
		}
	}
	return out
}

// empowerPlace puts n loyalty counters on one Jace token. minted is the set
// this resolution just created (they carry the type grant, which the
// resolution's published type table does not show yet); otherwise the
// candidates are re-read from the battlefield. done/ans is the answered pick.
func empowerPlace(h Host, c *Ctx, sa *cards.SA, typ string, n int32, minted, ans []state.ObjID, done bool) {
	g := h.Game()
	if n <= 0 {
		// Nothing to place (empower 0 still created its token above): no
		// zero CounterChange, so no "counters put" trigger can see one.
		return
	}
	cands := minted
	if cands == nil {
		cands = empowerCandidates(h, c, typ)
	}
	if done {
		for _, id := range ans {
			if o := g.Obj(id); o != nil && o.Zone == state.ZBattlefield && o.Controller == c.Controller {
				h.Emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "LOYALTY", Amount: n})
				return
			}
		}
		// The chosen token left while the decision was outstanding: no
		// counters (the counter_pick zone-guard convention).
		return
	}
	if len(cands) == 0 {
		return
	}
	if len(cands) > 1 {
		d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose,
			Min: 1, Max: 1, Source: c.Source,
			ResumeKind: "counter_pick", ResumeSA: sa,
			ResumeRemembered: copyTargets(c.Remembered),
			Prompt:           "Empower " + typ + " " + strconv.Itoa(int(n)) + " — choose a " + typ + " token you control"}
		for _, id := range cands {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options),
				Kind: "counter_pick", Label: typ + " Token", Obj: id, Player: c.Controller})
		}
		if Ask(h, d) == AskAsked {
			return // suspended; the answer re-enters with Ctx.CounterPick set.
		}
	}
	h.Emit(events.Event{Kind: events.CounterChange, Obj: cands[0], Counter: "LOYALTY", Amount: n})
}
