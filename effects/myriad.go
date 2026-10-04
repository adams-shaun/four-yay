package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("Myriad", effMyriad) }

// effMyriad implements CR 702.109's Myriad: when a creature with Myriad
// attacks, for each opponent OTHER than the defending player, the controller
// creates a token that is a copy of the attacking creature, tapped and
// attacking that opponent. The trigger (cards/keywords.go) fires on the
// source's own DeclareAttackers event, so the combat is already in progress.
// For EACH eligible opponent, CR 702.109 says the controller MAY create a
// copy. The choices are posed one at a time and resume with a deterministic
// candidate cursor: an answer creates (or declines) precisely that opponent's
// token before asking the next one. This avoids turning several independent
// may choices into a mandatory all-or-none operation.
//
// events.MyriadCleanup exiles these marked tokens as end combat ends. The
// copy is the source's current card/face, preserving its printed
// characteristics for the token's battlefield lifetime.
func effMyriad(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	src := g.Obj(c.Source)
	if src == nil || src.Face() == nil {
		return
	}
	eligible := myriadOpponents(g, c)
	start := 0

	for ; start < len(eligible); start++ {
		q := eligible[start]
		d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose,
			Min: 1, Max: 1, Source: c.Source, ResumeKind: "myriad", ResumeSA: sa,
			ResumeTarget: start,
			Prompt:       "Myriad: create a copy attacking " + g.Players[q].Name + "?",
			Options: []decision.Option{
				{Index: 0, Kind: "yes", Label: "Create a copy attacking " + g.Players[q].Name, Obj: c.Source, Player: c.Controller},
				{Index: 1, Kind: "no", Label: "Don't create a copy", Obj: c.Source, Player: c.Controller},
			}}
		if ans, ok := AskTape(h, d); ok {
			// The resolution kernel's answer in hand: the "myriad" arm's
			// yes creates this opponent's copy, then the walk asks the next
			// opponent, exactly as the re-entry does from ResumeTarget+1.
			if len(ans) == 1 && ans[0].Kind == "yes" {
				myriadCreate(h, g, c, q)
			}
			continue
		}
		// A host without decisions takes the legal optional decline for this
		// and every remaining opponent (R-9); it does not manufacture a token.
		return
	}
}

// myriadCreate creates the Myriad copy of the source attacking opponent q.
func myriadCreate(h Host, g *state.Game, c *Ctx, q state.PlayerID) {
	// Player is the token's controller (the attacking player) and IDs[0]
	// is the opponent it attacks. want is the ID the new copy will get
	// (state.Game.AddObject assigns g.NextID then increments it, the
	// same prediction effects/token.go's TokenCreate loop relies on),
	// captured before the mint so the follow-up MoveZone below names
	// the right object. MyriadCopy only mints the token (in the
	// untracked ZLibrary state AddObject leaves it in); the MoveZone
	// that actually seats it on the battlefield is a genuine
	// ChangesZone-matchable event, so the token's own ETB triggers
	// and every other "a creature enters" trigger observe its entry
	// exactly like an ordinary cast or reanimation (CR 702.109 grants
	// no special exemption from that).
	// The CreateToken replacements size the creation (Doubling
	// Season's "twice that many"): each copy is its own mint and entry.
	copies := proposeCopyTokens(h, c.Controller, c.Source, 1)
	for k := int32(0); k < copies; k++ {
		want := g.NextID
		h.Emit(events.Event{Kind: events.MyriadCopy, Obj: c.Source, Player: c.Controller, IDs: []state.ObjID{state.ObjID(q)}})
		if g.Obj(want) != nil {
			h.Emit(events.Event{Kind: events.MoveZone, Obj: want, From: state.ZLibrary, To: state.ZBattlefield})
		}
	}
}

// myriadOpponents returns the players Myriad considers, in APNAP order. The
// defending player is carried by triggerRemembered as the final player target,
// so it survives the stack object's context rebuild on every resumed choice.
func myriadOpponents(g *state.Game, c *Ctx) []state.PlayerID {
	defender := c.Controller
	for i := len(c.Remembered) - 1; i >= 0; i-- {
		if c.Remembered[i].IsPlayer {
			defender = c.Remembered[i].Player
			break
		}
	}
	out := make([]state.PlayerID, 0)
	for _, q := range g.AliveFrom(c.Controller) {
		if q != c.Controller && q != defender {
			out = append(out, q)
		}
	}
	return out
}
