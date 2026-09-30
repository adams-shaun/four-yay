package manabrew

import (
	"strings"

	"github.com/adams-shaun/gorge/protocol"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// Synthetic ack-only prompts (scoping spec §6.3's "ManaBrew prompts gorge
// never emits" list, gap G-5, ticket MB-14): a public reveal and a dice roll
// have no ManaBrew prompt or state carrier of their own in the ordinary
// mapping -- Appendix A's `revealCards`/`diceRolled` prompt TYPES exist, but
// nothing in GameViewDto shows what was revealed or rolled, so a spec
// client sees nothing happened. This file turns the two Note shapes that
// record those actions into ack-only prompts a ManaBrew client can render:
// SyntheticPrompts never blocks the engine (gorge does not wait for the
// ack) and AcknowledgeSynthetic never returns an intent, so a synthetic
// prompt can never reach the engine and answering it (or not) can never
// change a replay.
//
// The Note shapes recognised here are not a heuristic invented for this
// ticket -- they are the SAME predicates the engine already treats as
// canonical:
//   - Public reveal: Kind Note, Text=="", len(IDs)>0, !Secret. This is
//     exactly view/describe.go's own Note case ("<player> reveals <ids>"):
//     effReveal's two emit sites, effMill's Show$, explore's top-card
//     reveal, a library search's/pick's Reveal$, a ChoosePlayer/ChooseCard
//     reveal and a Show$ sacrifice all share this one wire convention
//     (effects/cardflow.go's own comment: "No Text: the Note's payload is
//     the ids, and view.Describe renders them"). A private look
//     (effects/look.go's emitLook) sets Secret, so it never matches.
//   - Dice roll: effects/dice.go's DieRollNote/DieRollBatchNote text
//     prefixes ("rolls a d…" / "rolls dice: …"). internal/manabrew may not
//     import effects (the ManaBrew boundary, archtest), so the two prefixes
//     are duplicated here as string literals rather than as an import of
//     effects.DieRollNotePrefix/DieRollBatchNotePrefix; effects/dice.go
//     documents both as ONE canonical shape emitted from exactly one
//     call site (effRollDice), so the duplication is a stable contract,
//     not a guess.
//
// Known gap: RevealCardsInput.Cards is a full CardDto, but the pure
// translator sees only what the connected seat's OWN view carries (spec
// §5.1: no *state.Game, no rules import). A reveal from a zone the view
// exposes to every seat (battlefield, graveyard, exile, the stack) or to
// this seat in particular (its own hand, its own visible library top)
// resolves to a real CardDto; a reveal from a zone the view still redacts
// for this seat (an opponent's hand, a library card that is not on top)
// resolves to a blank-identity CardDto carrying only the id -- the same
// degrade the protocol itself already uses for a face-down permanent
// (Appendix A.2). The synthetic ack still fires; it is honest about what
// it could not show, rather than inventing a name.

// dieRollNotePrefix and dieRollBatchNotePrefix mirror effects/dice.go's
// DieRollNotePrefix/DieRollBatchNotePrefix (see the file doc above for why
// they are copied rather than imported).
const (
	dieRollNotePrefix      = "rolls a d"
	dieRollBatchNotePrefix = "rolls dice: "
)

// SyntheticNamespaceShift is G-5's separate promptId namespace: a synthetic
// promptId is -((event.Seq << SyntheticNamespaceShift) | n) - 1, n counting
// the synthetic prompts minted from that one event (always 0 today -- one
// event mints at most one synthetic prompt -- the shift leaves room for more
// without renumbering). The NEGATION is what makes the namespace truly
// disjoint rather than merely "far apart": an ordinary promptId is a bare
// decision.Seq (ids.go's promptID), which is always >= 0, while every
// synthetic id is < 0 -- so a later ordinary prompt can never reuse an
// earlier synthetic id (MBX-2), no matter how the two magnitudes grow.
// Within the negative side the encoding is injective in (eventSeq, n) for
// any eventSeq below 2^55, where the shift cannot overflow int64; a match
// with 2^55 events is not a reachable magnitude. (gameover.go's terminal
// GameOverPromptID takes math.MaxInt64, the one positive id beyond any real
// decision.Seq, completing the three-way split.)
const SyntheticNamespaceShift = 8

func syntheticPromptID(eventSeq uint64, n int) int64 {
	return -(int64(eventSeq)<<SyntheticNamespaceShift | int64(n)) - 1
}

// dieRollNote decodes a per-die roll Note (effects/dice.go's DieRollNote):
// the roller, the die's sides and natural roll, and the modified result.
// ok is false for any other Note.
func dieRollNote(ev protocol.Event) (roller state.PlayerID, sides, natural, result int32, ok bool) {
	if ev.Kind != "note" || !strings.HasPrefix(ev.Text, dieRollNotePrefix) || len(ev.Pairs) == 0 {
		return 0, 0, 0, 0, false
	}
	return state.PlayerID(ev.Player), int32(ev.Pairs[0][0]), int32(ev.Pairs[0][1]), ev.Amount, true
}

// dieRollBatchNote decodes a per-resolution roll Note (effects/dice.go's
// DieRollBatchNote): the roller and the batch's die count. ok is false for
// any other Note.
func dieRollBatchNote(ev protocol.Event) (roller state.PlayerID, count int32, ok bool) {
	if ev.Kind != "note" || !strings.HasPrefix(ev.Text, dieRollBatchNotePrefix) || len(ev.Pairs) == 0 {
		return 0, 0, false
	}
	return state.PlayerID(ev.Player), int32(ev.Pairs[0][0]), true
}

// isPublicRevealNote reports whether ev is the shared public-reveal Note
// shape (see the file doc). ids is ev.IDs re-typed to state.ObjID.
func isPublicRevealNote(ev protocol.Event) (revealer state.PlayerID, ids []uint32, ok bool) {
	if ev.Kind != "note" || ev.Text != "" || len(ev.IDs) == 0 || ev.Secret {
		return 0, nil, false
	}
	return state.PlayerID(ev.Player), ev.IDs, true
}

// diceGroup accumulates one DB$ RollDice resolution's per-die Notes until
// its closing batch Note arrives (effRollDice's own emission order: every
// per-die Note first, then exactly one batch Note, all for the same
// (Player, Obj), with nothing else emitted in between).
type diceGroup struct {
	player   state.PlayerID
	source   uint32
	sides    int32
	naturals []int32
	finals   []int32
}

// Synthetic scans evs (one seat's own EventsSeat slice, already redacted for
// that seat) for the reveal and dice-roll Note shapes and returns the
// ack-only prompts they mint, in event order. v is that seat's own view
// (nil is accepted; DecidingPlayerID then falls back to the acting player
// recorded on the Note, since there is no viewer to address the prompt to).
//
// A malformed or interrupted dice run (a per-die Note not immediately
// followed by more per-die Notes of the same roll or its own closing batch
// Note) is dropped rather than guessed at: effRollDice never produces that
// shape, so seeing it means something this function does not understand is
// going on, and silence is the safe direction for an OPTIONAL fidelity
// prompt.
func (t *Translator) Synthetic(evs []protocol.EventBody, v *view.View) []mb.EngineMessage {
	var out []mb.EngineMessage
	var pending *diceGroup
	for _, eb := range evs {
		ev := eb.Event
		if roller, sides, natural, result, ok := dieRollNote(ev); ok {
			if pending == nil || pending.player != roller || pending.source != ev.Obj {
				pending = &diceGroup{player: roller, source: ev.Obj, sides: sides}
			}
			pending.naturals = append(pending.naturals, natural)
			pending.finals = append(pending.finals, result)
			continue
		}
		if roller, _, ok := dieRollBatchNote(ev); ok {
			if pending != nil && pending.player == roller && pending.source == ev.Obj && len(pending.naturals) > 0 {
				out = append(out, t.diceRolledMessage(ev.Seq, v, pending))
			}
			pending = nil
			continue
		}
		pending = nil
		if revealer, ids, ok := isPublicRevealNote(ev); ok {
			out = append(out, t.revealCardsMessage(ev.Seq, v, revealer, ids))
		}
	}
	return out
}

// decidingPlayer is the seat a synthetic prompt is addressed to: the
// connected seat this view belongs to, or actor when there is no view to
// read (Synthetic's v==nil case).
func decidingPlayer(v *view.View, actor state.PlayerID) state.PlayerID {
	if v == nil {
		return actor
	}
	return v.Viewer
}

func (t *Translator) diceRolledMessage(eventSeq uint64, v *view.View, g *diceGroup) mb.EngineMessage {
	naturals := make([]int, len(g.naturals))
	finals := make([]int, len(g.finals))
	for i := range g.naturals {
		naturals[i] = int(g.naturals[i])
		finals[i] = int(g.finals[i])
	}
	entry := mb.DiceRollEntry{
		PlayerID:       playerID(g.player),
		Round:          1,
		NaturalResults: naturals,
		FinalResults:   finals,
		IgnoredRolls:   []int{},
	}
	input := mb.DiceRolledInput{
		PromptBase: mb.PromptBase{Presentation: mb.PromptPresentation{Title: "Dice roll", Targets: []mb.TargetRef{}}},
		Sides:      int(g.sides),
		Rolls:      []mb.DiceRollEntry{entry},
	}
	return mb.EngineMessage{Value: mb.PromptMessage{Kind: "prompt", AgentPrompt: mb.AgentPrompt{
		PromptID:         syntheticPromptID(eventSeq, 0),
		DecidingPlayerID: playerID(decidingPlayer(v, g.player)),
		Input:            mb.PromptInput{Value: input},
	}}}
}

func (t *Translator) revealCardsMessage(eventSeq uint64, v *view.View, revealer state.PlayerID, ids []uint32) mb.EngineMessage {
	cards := make([]mb.CardDto, 0, len(ids))
	zone := mb.ZoneLibrary
	zoneKnown := false
	for _, id := range ids {
		dto, z, ok := t.revealedCardDto(v, state.ObjID(id))
		cards = append(cards, dto)
		if ok && !zoneKnown {
			zone, zoneKnown = z, true
		}
	}
	input := mb.RevealCardsInput{
		PromptBase:    mb.PromptBase{Presentation: mb.PromptPresentation{Title: "Reveal", Targets: []mb.TargetRef{}}},
		Cards:         cards,
		Zone:          zone,
		OwnerPlayerID: playerID(revealer),
	}
	return mb.EngineMessage{Value: mb.PromptMessage{Kind: "prompt", AgentPrompt: mb.AgentPrompt{
		PromptID:         syntheticPromptID(eventSeq, 0),
		DecidingPlayerID: playerID(decidingPlayer(v, revealer)),
		Input:            mb.PromptInput{Value: input},
	}}}
}

// findPublicCard resolves an object id to its view.CardView and ManaBrew
// zone, across every zone the seat view ever exposes: the always-public
// zones (battlefield, graveyard, exile, the stack) plus this seat's own
// hand and its own (grant-visible) library top. It deliberately widens
// findCard (errors.go), which only searches the zones a DECISION target can
// come from, because a reveal can name a graveyard or exile card that a
// target walk never offers as a candidate.
func findPublicCard(v *view.View, id state.ObjID) (*view.CardView, mb.ZoneKind) {
	if v == nil {
		return nil, ""
	}
	for pi := range v.Players {
		p := &v.Players[pi]
		if p.ID == v.Viewer && p.LibraryTop != nil && p.LibraryTop.ID == id {
			return p.LibraryTop, mb.ZoneLibrary
		}
		for i := range p.Battlefield {
			if p.Battlefield[i].ID == id {
				return &p.Battlefield[i], mb.ZoneBattlefield
			}
		}
		if p.ID == v.Viewer {
			for i := range p.Hand {
				if p.Hand[i].ID == id {
					return &p.Hand[i], mb.ZoneHand
				}
			}
		}
		for i := range p.Graveyard {
			if p.Graveyard[i].ID == id {
				return &p.Graveyard[i], mb.ZoneGraveyard
			}
		}
		for i := range p.Exile {
			if p.Exile[i].ID == id {
				return &p.Exile[i], mb.ZoneExile
			}
		}
	}
	for i := range v.Stack {
		if v.Stack[i].Card != nil && v.Stack[i].Card.ID == id {
			return v.Stack[i].Card, mb.ZoneStack
		}
	}
	return nil, ""
}

// blankRevealCard is the degrade for a revealed id the connected seat's own
// view still redacts (an opponent's hand, a library card not on top): the
// same "carries only the id" shape the protocol already uses for a
// face-down permanent (Appendix A.2), applied here because RevealCardsInput
// has no hidden-card union member of its own.
func blankRevealCard(id state.ObjID) mb.CardDto {
	return mb.CardDto{
		ID: cardID(id), Identity: mb.CardIdentity{}, Color: []string{}, Types: []string{}, Subtypes: []string{},
		Supertypes: []string{}, ClassLevels: []mb.ClassLevelDto{}, SagaChapters: []mb.SagaChapterDto{},
		Choices: []mb.CardChoiceDto{}, Keywords: []string{}, Counters: map[string]int{},
		AttachmentIDs: []string{}, MergedCardIDs: []string{},
	}
}

// revealedCardDto resolves one revealed id to its wire CardDto, the zone it
// was found in, and whether resolution succeeded (see the file doc's known
// gap). A face-down card resolves to the same blank identity a hidden card
// carries everywhere else in the mapping -- its face is exactly as
// unavailable here as anywhere.
func (t *Translator) revealedCardDto(v *view.View, id state.ObjID) (mb.CardDto, mb.ZoneKind, bool) {
	c, zone := findPublicCard(v, id)
	if c == nil || c.FaceDown {
		return blankRevealCard(id), zone, false
	}
	if vis, ok := t.visibleCard(*c).Value.(mb.VisibleCard); ok {
		return vis.CardDto, zone, true
	}
	return blankRevealCard(id), zone, false
}

// AcknowledgeSynthetic validates a client's answer to a synthetic prompt
// (RevealCardsAcknowledged / DiceRolledAcknowledged) against the exact
// prompt it answers. It never returns an intent: success means only
// "discard the prompt, no event, nothing reaches the engine" -- the caller
// (the transport, or a test standing in for it) takes no further action on
// success. Compare TranslateResponse, which is what an ordinary decision's
// answer goes through instead.
func AcknowledgeSynthetic(msg mb.ClientMessage, prompt mb.PromptMessage) *mb.ProtocolError {
	resp, ok := msg.Value.(mb.ClientResponse)
	if !ok {
		return errCode(mb.CodeInvalidShape, "a synthetic prompt takes a response, not a directive", idPtr(prompt.PromptID))
	}
	if resp.PromptID != prompt.PromptID {
		return errCode(mb.CodeStalePrompt,
			"response names a different promptId than the open synthetic prompt", idPtr(prompt.PromptID))
	}
	out := resp.Action.Output.Value
	switch prompt.Input.Value.(type) {
	case mb.RevealCardsInput:
		if _, ok := out.(mb.RevealCardsAcknowledged); !ok {
			return errCode(mb.CodeWrongPromptType, "revealCards takes a revealCardsAcknowledged response", idPtr(prompt.PromptID))
		}
	case mb.DiceRolledInput:
		if _, ok := out.(mb.DiceRolledAcknowledged); !ok {
			return errCode(mb.CodeWrongPromptType, "diceRolled takes a diceRolledAcknowledged response", idPtr(prompt.PromptID))
		}
	default:
		return errCode(mb.CodeInvalidShape, "not a synthetic prompt", idPtr(prompt.PromptID))
	}
	return nil
}
