package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TapOrUntap is Forge's TapOrUntapEffect (50 raw corpus lines over 48 files
// at pin 95f04e8a04c8925fa97cb226fc3341cabcc90a53 — Merrow Reejerey, Twiddle,
// Vedalken Anatomist, Elder Druid, Mind Over Matter, ...): "tap or untap
// target permanent". The target is chosen by the ordinary target ask (the
// ValidTgts$ answer rides Ctx.Targets exactly as it does for DB$ Tap); the
// tap-vs-untap ELECTION is a real mid-resolution KChoose posed to the
// resolving ability's controller, one per target, re-entering through rules'
// "taporuntap" resume arm and Ctx.TapOrUntap (consumed and cleared, the fx42
// scoping discipline).
//
// Deterministic contract (the R-9 no-ask stand-in mirror): the offered
// option list is ordered so option 0 is the state-CHANGING choice — "untap"
// first when the target is tapped, "tap" first when it is untapped — and
// option 1 is the no-op. A host that cannot ask and a bot that clamps
// therefore both take option 0 and always move the permanent, never
// silently nothing; the no-ask fallback emits no extra Note, the same
// silent stand-in effChooseType's fallback applies (R-9). Both options are
// always legal (tapping a tapped permanent is a legal no-op, CR 701.21c's
// tap action on an already-tapped permanent does nothing), so the
// strict-supersets never-ask rule does not apply and the ask is posed
// whenever a host can take it.
func init() {
	Register("TapOrUntap", effTapOrUntap)
}

// effTapOrUntap resolves each Defined$ permanent with the controller's
// tap-or-untap election. Tapper$ names the player whose tap it is (the
// provenance Taps triggers read), exactly effTap's read; the ELECTION is
// always the resolving ability's controller's. SubAbility$ chains are
// resolved by the ordinary Resolve walk, not here.
func effTapOrUntap(h Host, c *Ctx, sa *cards.SA) {
	tapper := c.Controller
	if spec := strings.TrimSpace(sa.ParamStr(cards.PKTapper)); spec != "" {
		if ps := definedPlayerIDs(h, c, spec); len(ps) > 0 {
			tapper = ps[0]
		}
	}
	for _, t := range Defined(h, c, sa) {
		if t.IsPlayer {
			continue
		}
		o := h.Game().Obj(t.Obj)
		if o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		// The re-entry answer for THIS target (or, for a malformed answer
		// that named no object, the first pending one). Consumed and cleared
		// so a later target poses its own ask and a nested TapOrUntap in the
		// same walk cannot inherit the outer answer (fx42 scoping).
		if c.TapOrUntapDone && (c.TapOrUntapObj == t.Obj || c.TapOrUntapObj == 0) {
			answer := c.TapOrUntap
			c.TapOrUntap, c.TapOrUntapObj, c.TapOrUntapDone = "", 0, false
			applyTapOrUntap(h, o, answer, tapper)
			continue
		}
		seat := c.Controller
		opts := []decision.Option{
			{Kind: "tap", Label: "Tap " + o.Face().Name, Obj: o.ID, Player: seat},
			{Kind: "untap", Label: "Untap " + o.Face().Name, Obj: o.ID, Player: seat},
		}
		if o.Tapped {
			opts[0], opts[1] = opts[1], opts[0]
		}
		for i := range opts {
			opts[i].Index = i
		}
		d := &decision.Decision{Player: seat, Kind: decision.KChoose, Min: 1, Max: 1,
			ResumeKind: "taporuntap", ResumeSA: sa, Source: c.Source,
			Prompt: "Tap or untap " + o.Face().Name + "?"}
		d.Options = opts
		if ans, ok := AskTape(h, d); ok {
			// The resolution kernel's answer in hand: the "taporuntap"
			// arm's election for this target (an empty answer degrades to
			// "tap", the arm's conservative read), then the next target.
			answer := ""
			if len(ans) > 0 {
				answer = ans[0].Kind
			}
			applyTapOrUntap(h, o, answer, tapper)
			continue
		}
		if Ask(h, d) == AskAsked {
			return
		}
		// No host (an effects-package test double, a fuzz run): option 0,
		// the state-changing choice — the exact mirror of botpolicy's clamp
		// answer for a bot-driven game.
		applyTapOrUntap(h, o, opts[0].Kind, tapper)
	}
}

// applyTapOrUntap emits the elected event. "untap" goes through TryUntap so
// a stun counter is removed instead (CR 122.1d), the shared boundary the
// untap step uses; "tap" (and the malformed-answer default) goes through
// EmitTap so the Taps/TapsForMana matcher sees the tapper's provenance, the
// same boundary effTap uses. Both no-op when the permanent is already in
// the elected state, the same guard effTap and TryUntap apply.
func applyTapOrUntap(h Host, o *state.Object, answer string, tapper state.PlayerID) {
	if answer == "untap" {
		TryUntap(h, o.ID)
		return
	}
	if o.Tapped {
		return
	}
	h.EmitTap(o.ID, tapper, false)
}
