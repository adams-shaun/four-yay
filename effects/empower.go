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
//     chooses: a KChoose ("counter_pick") answered in place. The R-9 no-host fallback (and botpolicy's first-option answer)
//     take the first candidate in battlefield order. The counters are one
//     ordinary CounterChange event, so AddCounter replacements (Doubling
//     Season) and "whenever you put one or more loyalty counters" triggers
//     see a normal placement.
//
// An absent Type$ or a missing token script is a loud Note that creates and
// places nothing.
func effEmpower(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	typ := strings.TrimSpace(sa.ParamStr(cards.PKType))
	if typ == "" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "Empower: no Type$ to empower"})
		return
	}
	n := Num(h, c, sa, "Num", 1)
	if n < 0 {
		n = 0
	}
	if len(empowerCandidates(h, c, typ)) > 0 {
		empowerPlace(h, c, sa, typ, n, nil)
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
		// A resolution-time window opened during the mint: no counters
		// are placed.
		return
	}
	empowerPlace(h, c, sa, typ, n, live)
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
func empowerPlace(h Host, c *Ctx, sa *cards.SA, typ string, n int32, minted []state.ObjID) {
	if n <= 0 {
		// Nothing to place (empower 0 still created its token above): no
		// zero CounterChange, so no "counters put" trigger can see one.
		return
	}
	cands := minted
	if cands == nil {
		cands = empowerCandidates(h, c, typ)
	}
	if len(cands) == 0 {
		return
	}
	if len(cands) > 1 {
		d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose,
			Min: 1, Max: 1, Source: c.Source,
			ResumeKind: "counter_pick", ResumeSA: sa,
			Prompt: "Empower " + typ + " " + strconv.Itoa(int(n)) + " — choose a " + typ + " token you control"}
		for _, id := range cands {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options),
				Kind: "counter_pick", Label: typ + " Token", Obj: id, Player: c.Controller})
		}
		if ans, ok := AskTape(h, d); ok {
			// The "counter_pick" answer in hand.
			empowerPlaceAnswered(h, c, n, counterAnswerObjs(ans))
			return
		}
	}
	h.Emit(events.Event{Kind: events.CounterChange, Obj: cands[0], Counter: "LOYALTY", Amount: n})
}

// empowerPlaceAnswered places the n loyalty counters on the first answered
// token still on the battlefield under the empowering player's control. A
// chosen token that left while the decision was outstanding takes no
// counters (the counter_pick zone-guard convention).
func empowerPlaceAnswered(h Host, c *Ctx, n int32, ans []state.ObjID) {
	g := h.Game()
	for _, id := range ans {
		if o := g.Obj(id); o != nil && o.Zone == state.ZBattlefield && o.Controller == c.Controller {
			h.Emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "LOYALTY", Amount: n})
			return
		}
	}
}
