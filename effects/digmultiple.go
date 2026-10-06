package effects

import (
	"strconv"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// effDigMultiple gives each ChangeValid alternative its own selection slot.
// The window is observed privately unless Reveal$ makes it public. Choices
// are made before any card leaves the library, so the replayed tape sees the
// same window on re-entry.
func effDigMultiple(h Host, c *Ctx, sa *cards.SA) {
	p := DigMultipleOf(sa)
	n := numText(h, c, p.Num, 1)
	if n < 0 {
		n = 0
	}
	g := h.Game()
	for _, owner := range definedPlayers(h, c, sa) {
		lib := zoneOf(g, state.ZLibrary, owner)
		count := int(n)
		if count > len(lib) {
			count = len(lib)
		}
		window := append([]state.ObjID(nil), lib[:count]...)
		if len(window) == 0 {
			continue
		}
		if p.Reveal {
			h.Emit(events.Event{Kind: events.Note, Player: owner, IDs: window})
		} else {
			emitLook(h, []state.PlayerID{owner}, state.ZLibrary, window, "looks at the top of the library")
		}
		d := &decision.Decision{Player: owner, Kind: decision.KChoose, Min: 0,
			Max: len(p.Specs), Source: c.Source, ResumeKind: "digmultiple", ResumeSA: sa,
			DistinctTypePicks: true, Prompt: "Choose at most one card for each listed type"}
		for _, id := range window {
			var matching []string
			for i, spec := range p.Specs {
				if MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
					matching = append(matching, strconv.Itoa(i))
				}
			}
			if len(matching) == 0 {
				continue
			}
			name := "a card"
			if o := g.Obj(id); o != nil && o.Face() != nil {
				name = o.Face().Name
			}
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "digmultiple", Obj: id,
				Player: owner, Label: name, SetProps: matching})
		}
		best := d.MaxDistinctTypeChoices()
		if !p.Optional {
			d.Min = len(best)
		}
		var picked []state.ObjID
		if len(d.Options) > 0 {
			if answer, ok := AskTape(h, d); ok {
				picked = answerObjs(answer)
			} else {
				// R-9 host: a maximum feasible selection in offer order.
				for _, i := range best {
					picked = append(picked, d.Options[i].Obj)
				}
				h.Emit(events.Event{Kind: events.Note, Player: owner, Obj: c.Source, Secret: true,
					Text: "takes the first matching card(s) (no engine host to ask)"})
			}
		}
		selected := make(map[state.ObjID]bool, len(picked))
		for _, id := range picked {
			selected[id] = true
		}
		var rest []state.ObjID
		for _, id := range window {
			if !selected[id] {
				rest = append(rest, id)
			}
		}
		// Green Sun's Twilight postpones both piles: its subabilities move
		// Remembered and Imprinted from the unchanged library window.
		if p.ChangeLater {
			if p.Remember {
				for _, id := range picked {
					c.Remembered = append(c.Remembered, state.Target{Obj: id})
				}
			}
			if p.ImprintRest {
				for _, id := range rest {
					h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source, IDs: []state.ObjID{id}})
				}
			}
			continue
		}
		chosen := state.ObjID(0)
		if p.ChooseAmount == 1 && len(picked) > 0 {
			choice := &decision.Decision{Player: owner, Kind: decision.KChoose, Min: 1, Max: 1,
				Source: c.Source, ResumeKind: "digmultiple_chosen", ResumeSA: sa,
				Prompt: "Choose one selected card to put onto the battlefield"}
			for i, id := range picked {
				choice.Options = append(choice.Options, decision.Option{Index: i, Kind: "digmultiple_chosen", Obj: id, Player: owner})
			}
			chosen = picked[0]
			if ans, ok := AskTape(h, choice); ok {
				chosen = ans[0].Obj
			}
		}
		for _, id := range picked {
			dest := p.Dest
			if id == chosen {
				dest = p.ChosenZone
			}
			if !p.Reveal {
				// Forge's look-then-reveal cards publicly identify only the
				// chosen cards, not the unchosen private library window.
				h.Emit(events.Event{Kind: events.Note, Player: owner, IDs: []state.ObjID{id}})
			}
			ev := moveZoneEvent(c, id, state.ZLibrary, dest)
			ev.Player, ev.Secret = owner, true
			h.Emit(ev)
			if p.Remember {
				c.Remembered = append(c.Remembered, state.Target{Obj: id})
			}
		}
		if p.Rest == state.ZLibrary {
			if len(rest) > 1 && !p.RandomRest {
				arr := &decision.Decision{Player: owner, Kind: decision.KArrange, Min: len(rest), Max: len(rest),
					Source: c.Source, ResumeKind: "digmultiple_rest", ResumeSA: sa,
					Prompt: "Put the remaining cards on the bottom of your library in any order"}
				for i, id := range rest {
					name := "a card"
					if o := g.Obj(id); o != nil && o.Face() != nil {
						name = o.Face().Name
					}
					arr.Options = append(arr.Options, decision.Option{Index: i, Kind: "digmultiple_rest", Obj: id, Label: name, Player: owner})
				}
				if ans, ok := AskTape(h, arr); ok {
					rest = answerObjs(ans)
				}
			}
			moveRestToBottom(h, g, owner, rest, p.RandomRest)
		} else {
			for _, id := range rest {
				ev := moveZoneEvent(c, id, state.ZLibrary, p.Rest)
				ev.Player, ev.Secret = owner, true
				h.Emit(ev)
			}
		}
	}
}
