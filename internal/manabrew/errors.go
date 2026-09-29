package manabrew

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// The response half of the ManaBrew mapping (scoping spec §6.4, §6.5). The
// prompt half lives in the prompt_*.go files; this file carries the
// TranslateResponse entry point, the §6.4 error mapping, the Pending carry,
// and the shared helpers the prompt builders reuse (view lookup, source card,
// mana production).
//
// The §6.4 table is the one home of the error mapping: a ManaBrew check and
// its gorge equivalent are listed here and nowhere else. Every response is
// translated through Validate (decision/decision.go), never a parallel
// rules-side reimplementation, so a rejection is exactly what the engine's
// own gate would say -- the legal-answer rule has one home.

// Pending is what the transport holds between a Prompt and its
// TranslateResponse: the prompt it sent, the decision it was built from, and
// the seat view it was sent with (the pass policy's exhaustStack snapshot
// reads the view's stack; the §6.4 "current prompt" is the prompt's id).
// A transport that refreshes its view mid-prompt may refresh Pending.View
// with it.
type Pending struct {
	Prompt   mb.PromptMessage
	Decision *decision.Decision
	// View is the seat view the prompt was sent with. The zero View (no
	// stack entries) makes a pass policy's exhaustStack snapshot empty, so
	// the first later stack object stops it early -- the safe direction.
	View view.View
}

// UndoRequest marks the restoreSnapshot outcome: the caller should submit an
// undo request for the connection's seat (host/undo.go's Registry.Undo). The
// marker carries nothing -- G-10: checkpointId discovery does not exist, so
// restoreSnapshot works only as "undo my last answer", which is why
// promptPriority's parser accepts nothing but the current promptId as the
// checkpoint.
type UndoRequest struct{}

// Outcome is one translated response. Exactly one arm is meaningful:
//   - Err set: the §6.4 error mapping produced a ProtocolError; the prompt
//     stays open and the client may answer again (gorge does not re-send it).
//   - Undo set: restoreSnapshot; the caller submits a undo request.
//   - Queued: the concede directive could not be answered now (no priority
//     decision open for this seat) and is queued (G-2); the caller answers
//     the seat's next priority decision with ConcedeIntent.
//   - Intent set: the response is a legal answer; the caller submits it. A
//     pass response also sets Policy.
type Outcome struct {
	Intent *decision.Intent
	Undo   *UndoRequest
	// Policy is the connection's pass policy a pass response installs. A
	// plain pass (no until, no exhaustStack) installs an inactive policy,
	// which is how a policy ends.
	Policy *PassPolicy
	// Queued reports a concede directive parked behind a non-priority ask.
	Queued bool
	Err    *mb.ProtocolError
}

// errCode is the §6.4 error mapping's one constructor: a closed-vocabulary
// code, a human message and the id of the prompt that stays open (nil when
// nothing is pending -- the very thing a stalePrompt error reports).
func errCode(code mb.ErrorCode, msg string, promptID *int64) *mb.ProtocolError {
	return &mb.ProtocolError{Code: code, Message: msg, PromptID: promptID}
}

func idPtr(i int64) *int64 { return &i }

// TranslateResponse maps one client→engine message against the connection's
// pending prompt and seat into an Outcome. A decode failure of the message
// itself is the transport's problem (mb.ClientMessage's strict decoders
// reject it before this runs).
//
// seat is the connection's bound seat. On a claim-bound connection it always
// matches the pending decision's player (the §6.4 row calls the check
// defensive), but it is kept because an unboken claim must never become an
// intent.
func (t *Translator) TranslateResponse(msg mb.ClientMessage, p *Pending, seat state.PlayerID) Outcome {
	switch v := msg.Value.(type) {
	case mb.ClientDirective:
		return t.translateDirective(v, p, seat)
	case mb.ClientResponse:
		return t.translateResponse(v, p, seat)
	default:
		return Outcome{Err: errCode(mb.CodeInvalidShape, "message kind is not a client→engine message", nil)}
	}
}

// translateDirective maps the out-of-band concede directive (G-2): an
// immediate priority decision for this seat is answered now with the
// concede option; anything else queues the concession for the seat's next
// priority decision.
func (t *Translator) translateDirective(d mb.ClientDirective, p *Pending, seat state.PlayerID) Outcome {
	if d.Directive.Type != "concede" {
		// Unreachable through the decoder (DirectiveInput accepts only
		// "concede"); kept as defence so a hand-built value cannot queue.
		return Outcome{Err: errCode(mb.CodeInvalidShape, "unknown directive "+d.Directive.Type, nil)}
	}
	if p != nil && p.Decision != nil && p.Decision.Kind == decision.KPriority && p.Decision.Player == seat {
		intent, err := ConcedeIntent(p.Decision)
		if err != nil {
			return Outcome{Err: errCode(mb.CodeInvalidShape, err.Error(), nil)}
		}
		return Outcome{Intent: &intent}
	}
	return Outcome{Queued: true}
}

// translateResponse is the §6.4 checks in order: something pending, the
// prompt id, the seat, then the prompt-type/action-type pairing, then the
// per-kind parser (which ends every leg in Decision.Validate).
func (t *Translator) translateResponse(r mb.ClientResponse, p *Pending, seat state.PlayerID) Outcome {
	if p == nil || p.Decision == nil {
		return Outcome{Err: errCode(mb.CodeStalePrompt, "no prompt is open", nil)}
	}
	cur := p.Prompt.PromptID
	if r.PromptID != cur {
		return Outcome{Err: errCode(mb.CodeStalePrompt,
			fmt.Sprintf("response names promptId %d; the open prompt is %d", r.PromptID, cur), idPtr(cur))}
	}
	if seat != p.Decision.Player {
		return Outcome{Err: errCode(mb.CodeWrongPlayer,
			fmt.Sprintf("connection seat %s is not the deciding player %s", playerID(seat), playerID(p.Decision.Player)), idPtr(cur))}
	}
	in := p.Prompt.Input.Value
	out := r.Action.Output.Value
	if in == nil || out == nil {
		return Outcome{Err: errCode(mb.CodeInvalidShape, "response carries no prompt or output payload", idPtr(cur))}
	}
	switch in.PromptType() {
	case "chooseAction":
		if o, ok := out.(mb.PassOutput); ok {
			return t.parseChooseActionPass(o, p)
		}
		if o, ok := out.(mb.RestoreSnapshotOutput); ok {
			return t.parseChooseActionUndo(o, p)
		}
		if o, ok := out.(mb.ActOutput); ok {
			return t.parseChooseActionAct(o, p)
		}
		return Outcome{Err: errCode(mb.CodeWrongPromptType,
			fmt.Sprintf("prompt chooseAction does not take a %s response; expected pass, restoreSnapshot or act", out.OutputType()), idPtr(cur))}
	case "chooseBoardTargets":
		if _, ok := out.(mb.BoardTargetsDecision); !ok {
			return Outcome{Err: errCode(mb.CodeWrongPromptType,
				fmt.Sprintf("prompt chooseBoardTargets does not take a %s response", out.OutputType()), idPtr(cur))}
		}
		return t.parseBoardTargets(out, p)
	case "chooseAttackers":
		if _, ok := out.(mb.DeclareAttackersDecision); !ok {
			return Outcome{Err: errCode(mb.CodeWrongPromptType,
				fmt.Sprintf("prompt chooseAttackers does not take a %s response", out.OutputType()), idPtr(cur))}
		}
		return t.parseAttackers(out, p)
	case "chooseBlockers":
		if _, ok := out.(mb.DeclareBlockersDecision); !ok {
			return Outcome{Err: errCode(mb.CodeWrongPromptType,
				fmt.Sprintf("prompt chooseBlockers does not take a %s response", out.OutputType()), idPtr(cur))}
		}
		return t.parseBlockers(out, p)
	case "mulligan":
		if _, ok := out.(mb.MulliganDecision); !ok {
			return Outcome{Err: errCode(mb.CodeWrongPromptType,
				fmt.Sprintf("prompt mulligan does not take a %s response", out.OutputType()), idPtr(cur))}
		}
		return t.parseMulligan(out, p)
	case "mulliganPutBack":
		if _, ok := out.(mb.MulliganPutBackDecision); !ok {
			return Outcome{Err: errCode(mb.CodeWrongPromptType,
				fmt.Sprintf("prompt mulliganPutBack does not take a %s response", out.OutputType()), idPtr(cur))}
		}
		return t.parseMulliganPutBack(out, p)
	case "chooseNumber":
		if _, ok := out.(mb.NumberDecision); !ok {
			return Outcome{Err: errCode(mb.CodeWrongPromptType,
				fmt.Sprintf("prompt chooseNumber does not take a %s response", out.OutputType()), idPtr(cur))}
		}
		return t.parseChooseNumber(out, p)
	case "chooseCards":
		if _, ok := out.(mb.ChooseCardsDecision); !ok {
			return Outcome{Err: errCode(mb.CodeWrongPromptType,
				fmt.Sprintf("prompt chooseCards does not take a %s response", out.OutputType()), idPtr(cur))}
		}
		return t.parseChooseCards(out, p)
	case "chooseColor":
		if _, ok := out.(mb.ColorDecision); !ok {
			return Outcome{Err: errCode(mb.CodeWrongPromptType,
				fmt.Sprintf("prompt chooseColor does not take a %s response", out.OutputType()), idPtr(cur))}
		}
		return t.parseChooseColor(out, p)
	case "chooseBoolean":
		if _, ok := out.(mb.BooleanDecision); !ok {
			return Outcome{Err: errCode(mb.CodeWrongPromptType,
				fmt.Sprintf("prompt chooseBoolean does not take a %s response", out.OutputType()), idPtr(cur))}
		}
		return t.parseChooseBoolean(out, p)
	case "chooseFromSelection":
		if _, ok := out.(mb.SelectionDecision); !ok {
			return Outcome{Err: errCode(mb.CodeWrongPromptType,
				fmt.Sprintf("prompt chooseFromSelection does not take a %s response", out.OutputType()), idPtr(cur))}
		}
		return t.parseChooseFromSelection(out, p)
	case "scry":
		if _, ok := out.(mb.ScryDecision); !ok {
			return Outcome{Err: errCode(mb.CodeWrongPromptType,
				fmt.Sprintf("prompt scry does not take a %s response", out.OutputType()), idPtr(cur))}
		}
		return t.parseArrangeScry(out, p)
	case "reorder":
		// "reorder" is shared by two decision Kinds (promptOrder's
		// KTriggerOrder and promptArrange's KArrange single-list shape);
		// only the pending decision's own Kind says which reversal applies
		// (parseTriggerOrder's DIRECTION FLIP vs parseArrangeReorder's none).
		if _, ok := out.(mb.ReorderDecision); !ok {
			return Outcome{Err: errCode(mb.CodeWrongPromptType,
				fmt.Sprintf("prompt reorder does not take a %s response", out.OutputType()), idPtr(cur))}
		}
		if p.Decision.Kind == decision.KTriggerOrder {
			return t.parseTriggerOrder(out, p)
		}
		return t.parseArrangeReorder(out, p)
	case "payManaCost":
		// parsePayManaCost switches on the output type itself (act / pay /
		// cancel all answer this one prompt type) and already ends every
		// leg, including the mismatched-shape leg, in a CodeWrongPromptType
		// Outcome, so no separate type check is needed here.
		return t.parsePayManaCost(out, p)
	case "gameOver":
		return Outcome{Err: errCode(mb.CodeWrongPromptType, "gameOver carries no response", idPtr(cur))}
	default:
		// A prompt kind whose MB ticket has not landed: its own stub never
		// emits one, so a response to it cannot be mapped.
		return Outcome{Err: errCode(mb.CodeWrongPromptType,
			fmt.Sprintf("prompt %s has no response mapping", in.PromptType()), idPtr(cur))}
	}
}

// parsePlayerID reverses playerID's "player-<seat>" mint. A ManaBrew client
// echoes ids it was sent, so only the mint's own shape is legal.
func parsePlayerID(s string) (state.PlayerID, bool) {
	rest, ok := strings.CutPrefix(s, "player-")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 0 {
		return 0, false
	}
	return state.PlayerID(n), true
}

// findCard resolves an object id in a seat view, across every zone list the
// view carries: the battlefield of each player, the deciding player's hand,
// the stack (a spell's own CardView, projected at the spell object's id --
// view.stackViews sets StackView.Card.ID to the same id as the StackView
// entry itself, so a card mid-cast, such as the source of an announced
// CR 601.2g mana payment window, resolves here too), and the viewer's
// library top. It is a lookup for prompt builders only -- redaction already
// decided what the view shows.
func findCard(v *view.View, id state.ObjID) *view.CardView {
	if v == nil {
		return nil
	}
	for _, p := range v.Players {
		if p.ID == v.Viewer && p.LibraryTop != nil && p.LibraryTop.ID == id {
			return p.LibraryTop
		}
		for i := range p.Battlefield {
			if p.Battlefield[i].ID == id {
				return &p.Battlefield[i]
			}
		}
		if p.Hand != nil && p.ID == v.Viewer {
			for i := range p.Hand {
				if p.Hand[i].ID == id {
					return &p.Hand[i]
				}
			}
		}
	}
	for i := range v.Stack {
		if v.Stack[i].Card != nil && v.Stack[i].Card.ID == id {
			return v.Stack[i].Card
		}
	}
	return nil
}

// playerLabel names a seat from the view ("" when absent).
func playerLabel(v *view.View, p state.PlayerID) string {
	if v == nil {
		return ""
	}
	for _, pv := range v.Players {
		if pv.ID == p {
			return pv.Name
		}
	}
	return ""
}

// typeWords splits a view CardView's type line the way state.go's projection
// does: the half before the em-dash separator holds the types.
func typeWords(c *view.CardView) []string {
	if c == nil {
		return nil
	}
	head := strings.SplitN(c.Types, "—", 2)[0]
	return strings.Fields(head)
}

// hasType reports whether a card's type half names typ exactly.
func hasType(c *view.CardView, typ string) bool {
	for _, w := range typeWords(c) {
		if w == typ {
			return true
		}
	}
	return false
}

// manaProductions turns a card's view-side ManaProduction into the wire
// Mana list of an activateAbility action: every colour slot the face
// statically names, plus the five colours of an any-colour source with no
// explicit slots. It never says which of a dual's two abilities was meant --
// the action is the OFFER, the engine picks the ability at activation.
func manaProductions(v *view.View, id state.ObjID) []mb.Mana {
	c := findCard(v, id)
	if c == nil || c.Produces == nil {
		return nil
	}
	colors := []mb.ManaColor{mb.ColorWhite, mb.ColorBlue, mb.ColorBlack, mb.ColorRed, mb.ColorGreen, mb.ColorColorless}
	out := make([]mb.Mana, 0, 6)
	for i, n := range c.Produces.Colour {
		if n > 0 {
			out = append(out, mb.Mana{Color: colors[i], Amount: int(n)})
		}
	}
	if len(out) == 0 && c.Produces.Any {
		for i := 0; i < 5; i++ {
			out = append(out, mb.Mana{Color: colors[i], Amount: 1})
		}
	}
	return out
}

// sourceCard projects the pending decision's source object as the prompt's
// SourceCard. The view has already redacted whatever the seat may not see,
// and a face-down source carries an empty identity exactly as its zone
// projection does.
func (t *Translator) sourceCard(v *view.View, id state.ObjID) *mb.CardDto {
	if v == nil || id == 0 {
		return nil
	}
	c := findCard(v, id)
	if c == nil {
		return nil
	}
	card, ok := t.visibleCard(*c).Value.(mb.VisibleCard)
	if !ok {
		return nil
	}
	dto := card.CardDto
	return &dto
}

// ConcedeIntent is the intent that answers a priority decision with its
// concede option (G-2's answer half, shared by the directive arm above and
// by a transport replaying a queued concession). It goes through
// Decision.Validate like every other answer, so a queue replayed at the
// wrong moment is a reject, not a rules bypass.
func ConcedeIntent(d *decision.Decision) (decision.Intent, error) {
	if d == nil {
		return decision.Intent{}, fmt.Errorf("no pending decision to concede")
	}
	if d.Kind != decision.KPriority {
		return decision.Intent{}, fmt.Errorf("concede is only offered at priority, not %s", d.Kind)
	}
	for _, opt := range d.Options {
		if opt.Kind == "concede" {
			in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}
			if err := d.Validate(in); err != nil {
				return decision.Intent{}, err
			}
			return in, nil
		}
	}
	return decision.Intent{}, fmt.Errorf("the open decision offers no concede option")
}
