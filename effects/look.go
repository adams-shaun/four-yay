package effects

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// emitLook is the ONE construction point for a private look: an effect (or
// engine flow) that shows hidden-zone cards to a SUBSET of the table rather
// than revealing them to everyone. Every looker-scoped effect in the engine
// routes through here — the round-2 review of the Gitaxian Probe merge found
// one of them (RevealHand's Look$ arm) emitting its look as a PUBLIC Note,
// handing the target's whole hand to every seat and spectator, so the class
// was fixed by funnelling all of them through this helper:
//
//   - effReveal's Look$ arm (RevealHand/Reveal/PeekAndReveal with Look$ True;
//     28 corpus RevealHand lines — Gitaxian Probe, Glasses of Urza, Slayer's
//     Bounty): the activator looks at the target's hand. CR 701.20e: a card
//     looked at this way is shown only to the player the effect specifies.
//   - effDig's ask-path look (the window the library's owner is choosing
//     from).
//   - effRearrangeTopOfLibrary, effScry and effSurveil (the KArrange looks
//     at the top of the library).
//
// It emits ONE Secret Note per looker with Player = that looker, so rule (1)
// of view.RedactEvents scopes the payload (ids and text) to them alone:
// every other seat and a Public spectator get the bare shape (and the
// generic "looks at hidden cards" transcript line), and an Omniscient
// spectator keeps the payload only for a HAND look (From == ZHand — their
// visibility already includes every hand, per Ruling FL-9's library-order
// carve-out not applying to one). The looked-at zone rides From so the
// looker's transcript line can name it even after the cards have moved on;
// existing library-top callers pass ZLibrary, which is also the zero value
// their events always carried, so their encoded bytes are unchanged.
//
// text is the clause after the looker's name on their own copy's transcript
// line; the hand look passes "" so view.Describe's ids branch renders the
// full line (who looked, whose hand, which cards) from the payload itself.
func emitLook(h Host, lookers []state.PlayerID, from state.Zone, ids []state.ObjID, text string) {
	for _, looker := range lookers {
		h.Emit(events.Event{Kind: events.Note, Player: looker, From: from,
			IDs: ids, Text: text, Secret: true})
	}
}

// poseLookAck is the bare private look's pacing gate (lookack, task
// fb-20260917T232325Z-35cfca4b): the one information transfer in the engine
// that used to land as a Secret Note with NO decision attached, so a
// client's auto-passing priority streamed the line past before the player
// could read it. effReveal's two bare arms (NoReveal$ True — Mishra's
// Bauble — and the mandatory Look$ True arm — Gitaxian Probe) pose this ask
// BEFORE the note: a KChoose with ONE option ("Continue") to the looker,
// whose prompt names what is about to be looked at and where — the library
// is not projected to the looker's seat (view exposes only LibrarySize), so
// the modal is the readable surface, the same channel the PeekAndReveal
// optional branch uses. The decision is Min == Max == 1 over one option, so
// it is never an empty-only ask. The ask gates only the PACING: there is no
// decline (CR 701.20e requires the look to happen), so any answer — the
// single Continue option, a malformed empty one included — acknowledges.
// ResumeTarget is the index of the Defined$ target this ask belongs to, so
// every bare look in a multi-target walk (Case the Joint's Defined$ Player)
// poses its own ack.
//
// The ack is answered in place via AskTape; the caller emits the look right
// after, whether it was answered or no host could ask (R-9) — information is
// never lost to a host that cannot ask, the same deterministic degradation
// Scry/Surveil carry.
func poseLookAck(h Host, c *Ctx, sa *cards.SA, looker, owner state.PlayerID, zone state.Zone, ids []state.ObjID, targetIndex int) {
	zoneName := "library"
	if zone == state.ZHand {
		zoneName = "hand"
	}
	ownerPhrase := "your " + zoneName
	if owner != looker {
		ownerPhrase = "the target player's " + zoneName
	}
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		if o := h.Game().Obj(id); o != nil && o.Face() != nil {
			names = append(names, o.Face().Name)
		}
	}
	prompt := "You look at " + ownerPhrase
	if len(names) > 0 {
		prompt += ": " + strings.Join(names, ", ")
	} else {
		prompt += fmt.Sprintf(" (%d card(s))", len(ids))
	}
	d := &decision.Decision{Player: looker, Kind: decision.KChoose, Min: 1, Max: 1,
		ResumeKind: "look_ack", ResumeSA: sa, Source: c.Source, ResumeTarget: targetIndex,
		Prompt:  prompt,
		Options: []decision.Option{{Index: 0, Kind: "yes", Label: "Continue", Player: looker}}}
	// Any served answer acknowledges, so the caller emits the look now.
	AskTape(h, d)
}
