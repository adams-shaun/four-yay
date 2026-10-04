package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("Endure", effEndure) }

// endureSpiritToken is the corpus's 0/0 white Spirit token script
// (.cards/tokenscripts/w_x_x_spirit.txt, PT:*/*). Its two dynamic sides ride
// a layer-7b SetPower$/SetToughness$ continuous effect registered per mint,
// exactly as api:Token's TokenPower$/TokenToughness$ rider does.
const endureSpiritToken = "w_x_x_spirit"

// effEndure implements CR 701.63 (Endure N). An affected permanent's
// controller chooses to put N +1/+1 counters on it OR create an N/N white
// Spirit creature token (CR 701.63a). The choice is a real mid-resolution
// election answered in place through the resolution kernel's tape (AskTape),
// never a silent default: a host with no tape run observes the ask through
// Host.Ask and the deterministic R-9 stand-in takes the counter branch, which
// matches Forge's "unless they put counters" reading.
//
// CR 701.63b: endure 0 does nothing -- no counters, no token -- so the whole
// effect is a no-op before any ask.
//
// A permanent that has left the battlefield cannot receive counters, so its
// controller has no choice to make: the token is created (the "unless they
// put N +1/+1 counters on that permanent" clause can never be satisfied).
//
// The mint goes through Host.EmitTokenCreate, the ordinary TokenCreate event,
// so a CreateToken replacement (Doubling Season, Anointed Procession) mints
// its extras and the dynamic P/T rider lands on EVERY mint the plan produced,
// not just the first. Mutation is entirely through events: CounterChange
// folds through events.Apply and runs the AddCounter replacement pipeline
// (Hardened Scales, Doubling Season), never a direct state write.
func effEndure(h Host, c *Ctx, sa *cards.SA) {
	n := Num(h, c, sa, "Num", 1)
	if n <= 0 {
		return // CR 701.63b
	}
	g := h.Game()
	for _, t := range Defined(h, c, sa) {
		if t.IsPlayer || t.Obj == 0 {
			continue
		}
		o := g.Obj(t.Obj)
		player := c.Controller
		if o != nil {
			player = o.Controller
		}
		// CR 701.63a: counters are only possible while the permanent is on the
		// battlefield. Once it has departed the only branch left is the token.
		canCounter := o != nil && o.Zone == state.ZBattlefield
		branch := ""
		if canCounter {
			d := &decision.Decision{Player: player, Kind: decision.KChoose, Min: 1, Max: 1,
				Source: c.Source, ResumeKind: "endure", ResumeSA: sa,
				Prompt: "Choose how this permanent endures"}
			d.Options = []decision.Option{
				{Index: 0, Kind: "endure_counters", Label: "Put +1/+1 counters on it", Obj: t.Obj, Player: player},
				{Index: 1, Kind: "endure_spirit", Label: "Create a Spirit", Obj: t.Obj, Player: player},
			}
			if ans, ok := AskTape(h, d); ok && len(ans) > 0 {
				branch = ans[0].Kind
			} else {
				// R-9 no-tape stand-in: the counter branch (Forge's literal
				// "unless they put counters" default).
				branch = "endure_counters"
			}
		} else {
			branch = "endure_spirit"
		}
		switch branch {
		case "endure_counters":
			if canCounter {
				h.Emit(events.Event{Kind: events.CounterChange, Obj: t.Obj, Counter: "P1P1", Amount: n})
			}
		case "endure_spirit":
			endureCreateSpirit(h, c, player, n)
		default:
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: player,
				Text: "Endure: unknown choice " + branch})
		}
	}
}

// endureCreateSpirit mints one white Spirit token whose power and toughness
// are both n. The base script is the corpus's 0/0 white Spirit
// (w_x_x_spirit); each mint EmitTokenCreate returns takes the layer-7b set.
func endureCreateSpirit(h Host, c *Ctx, player state.PlayerID, n int32) {
	g := h.Game()
	if _, ok := g.Tokens[endureSpiritToken]; !ok {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: player,
			Text: "Endure: missing Spirit token script " + endureSpiritToken})
		return
	}
	mints := h.EmitTokenCreate(events.Event{Kind: events.TokenCreate, Player: player, Text: endureSpiritToken})
	for _, id := range mints {
		if g.Obj(id) == nil {
			continue
		}
		h.AddContinuous(state.ContinuousEffect{
			Source:       id,
			Controller:   player,
			Affects:      "Card.Self",
			Layer:        state.LPT,
			Sub:          state.SubSet,
			SetPower:     n,
			SetToughness: n,
			HasSet:       true,
			Permanent:    true,
		})
	}
}
