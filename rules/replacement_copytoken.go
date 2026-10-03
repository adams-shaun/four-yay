package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Token COPIES and the CreateToken replacement class (CR 111.1, 614.1a,
// 706.2): "create a token that's a copy of <permanent>" is an effect creating
// a token, so Doubling Season, Parallel Lives, Anointed Procession, Ojer Taq,
// Chatterfang and every other R:Event$ CreateToken line apply to it exactly
// as they apply to a scripted token. The copy mints (DB$ CopyPermanent's
// CopyToken + MoveZone, Myriad's MyriadCopy, Encore's CardToken) are not
// TokenCreate events, so the ordinary emit-time replacement pass never saw
// them and a token copy was created once whatever doubled tokens.
//
// The copy effects carry per-copy riders (granted triggers, set P/T, tapped
// and attacking, "exile it at end of turn") they must apply to EVERY copy,
// and a cursor (TokenRest) that resumes a parked mint, so the replacement is
// taken as a PROPOSAL before minting -- the Scry/RollDice proposal pattern:
// a synthetic TokenCreate naming the copied object (Obj set, no script) is
// passed through the ordinary replacement collection, which never logs it.
// The plan the CreateToken drive builds from it answers how many copies the
// caller mints; scripted mints a body adds or substitutes (Chatterfang's
// Squirrels, Divine Visitation's Angels) are emitted here, past the
// replacement pass, the way emitTokenPlanMints emits them.
//
// Deliberately not posed for a copy: the elections (an Optional$ body, the
// chosen-copy TokenScript$ Chosen family) are declined and competing bodies
// apply in scan order instead of through the CR 616.1 order ask, because a
// proposal cannot park the copy effect mid-resolution. A ReplaceController
// body leaves the copy under its creator, with a Note.

// isCopyTokenProposal reports whether ev is ProposeCopyTokens' synthetic
// event rather than a real scripted TokenCreate (which always names a script
// and never an object).
func isCopyTokenProposal(ev events.Event) bool {
	return ev.Kind == events.TokenCreate && ev.Text == "" && ev.Obj != 0
}

// ProposeCopyTokens satisfies effects' copy-token proposal: the number of
// token copies of src that an effect creating n of them under player
// actually creates once the CreateToken replacements apply. It emits the
// replacement plan's scripted mints, so a caller asks once per creation and
// keeps the answer across a resumed mint.
func (e *Engine) ProposeCopyTokens(player state.PlayerID, src state.ObjID, n int32) int32 {
	if n <= 0 || src == 0 || e.applyingReplacement || e.G.Obj(src) == nil {
		return n
	}
	replaced, handled := e.applyReplacementsDispatch(events.Event{Kind: events.TokenCreate,
		Obj: src, Player: player, Amount: n})
	if !handled {
		return n
	}
	return replaced.Amount
}

// continueCopyTokenProposal drives the proposal's plan: n copy mints of
// ev.Obj, every applicable match applied in scan order without asking. The
// returned event's Amount is the number of copies the caller mints.
func (e *Engine) continueCopyTokenProposal(ev events.Event, matches []replMatch) (events.Event, bool) {
	plan := make([]tokenPlanMint, ev.Amount)
	for i := range plan {
		plan[i] = tokenPlanMint{copyOf: ev.Obj}
	}
	for _, m := range matches {
		body := m.repl.With
		if body == nil || body.API != "ReplaceToken" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(body.Params["TokenScript"]), "Chosen") ||
			strings.TrimSpace(body.Params["ValidChoices"]) != "" ||
			strings.EqualFold(m.repl.ParamStr(cards.PKOptional), "True") {
			continue
		}
		plan = e.applyTokenReplacementToPlan(ev, plan, m)
	}
	copies := int32(0)
	moved := false
	for _, mint := range plan {
		if mint.copyOf == 0 {
			continue
		}
		copies++
		if mint.hasController && mint.controller != ev.Player {
			moved = true
		}
	}
	if moved {
		e.emit(events.Event{Kind: events.Note, Obj: ev.Obj, Player: ev.Player,
			Text: "a ReplaceController token replacement does not apply to a token copy; the copy is created under its creator"})
	}
	for _, mint := range plan {
		if mint.copyOf != 0 {
			continue
		}
		saved := e.applyingReplacement
		e.applyingReplacement = true
		e.emit(events.Event{Kind: events.TokenCreate, Player: tokenMintPlayer(mint, ev), Text: mint.script})
		e.applyingReplacement = saved
	}
	return events.Event{Kind: events.TokenCreate, Obj: ev.Obj, Player: ev.Player, Amount: copies}, true
}
