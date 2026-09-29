package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	Register("GainLife", effGainLife)
	Register("LoseLife", effLoseLife)
	Register("ExchangeLife", effExchangeLife)
	Register("ExchangeLifeVariant", effExchangeLifeVariant)
}

// effGainLife and effLoseLife both clamp a negative LifeAmount$ to zero,
// mirroring Ruling T14-f's DealDamage/Mana clamps: LifeChange's Apply case is
// a plain "+= Amount", so an unclamped negative would silently flip the
// direction of the effect (a life-gain spell that drains, or vice versa)
// instead of doing nothing.
func effGainLife(h Host, c *Ctx, sa *cards.SA) {
	n := Num(h, c, sa, "LifeAmount", 1)
	if n < 0 {
		n = 0
	}
	for _, t := range actingPlayers(h, c, sa) {
		h.Emit(events.Event{Kind: events.LifeChange, Player: t, Amount: n})
	}
}

// effExchangeLife exchanges two players' totals. A single target exchanges
// with the controller; a two-target ability exchanges those two targets.
// The deltas are computed from the same snapshot, not from intermediate life.
func effExchangeLife(h Host, c *Ctx, sa *cards.SA) {
	players := actingPlayers(h, c, sa)
	var a, b state.PlayerID
	switch len(players) {
	case 1:
		a, b = c.Controller, players[0]
	case 2:
		a, b = players[0], players[1]
	default:
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "ExchangeLife: expected one or two players"})
		return
	}
	if a == b || int(a) >= len(h.Game().Players) || int(b) >= len(h.Game().Players) {
		return
	}
	oldA, oldB := h.Game().Players[a].Life, h.Game().Players[b].Life
	if sa.Params["RememberOwnLoss"] == "True" || sa.Params["RememberDifference"] == "True" {
		c.ExchangeNumber, c.ExchangeNumberBound = 0, true
	}
	if oldA == oldB {
		return
	}
	first := events.Event{Kind: events.LifeChange, Player: a, Amount: oldB - oldA}
	second := events.Event{Kind: events.LifeChange, Player: b, Amount: oldA - oldB}
	if exchange, ok := h.(interface {
		ExchangeLife(events.Event, events.Event, state.PlayerID, int32, *Ctx, bool)
	}); ok {
		beforeController := h.Game().Players[c.Controller].Life
		exchange.ExchangeLife(first, second, c.Controller, beforeController, c, sa.Params["RememberOwnLoss"] == "True")
	} else {
		h.Emit(first)
		h.Emit(second)
		if sa.Params["RememberOwnLoss"] == "True" && (c.Controller == a || c.Controller == b) {
			before := oldA
			if c.Controller == b {
				before = oldB
			}
			if loss := before - h.Game().Players[c.Controller].Life; loss > 0 {
				c.ExchangeNumber = loss
			}
		}
	}
	if sa.Params["RememberDifference"] == "True" {
		diff := oldA - oldB
		if diff < 0 {
			diff = -diff
		}
		c.ExchangeNumber = diff
		c.ExchangeNumberBound = true
	}
}

// effExchangeLifeVariant exchanges the selected player's life total with the
// source creature's current derived power or toughness. The rules engine owns
// the transaction so replacement effects can settle before the characteristic
// setter is installed.
func effExchangeLifeVariant(h Host, c *Ctx, sa *cards.SA) {
	targets := actingPlayers(h, c, sa)
	if len(targets) != 1 {
		return
	}
	target := targets[0]
	player := target
	source := h.Game().Obj(c.Source)
	if source == nil || source.Zone != state.ZBattlefield || source.Face() == nil {
		return
	}

	mode := sa.Params["Mode"]
	var oldCharacteristic int32
	var setPower, setToughness bool
	switch mode {
	case "Power":
		oldCharacteristic = h.Power(c.Source)
		setPower = true
	case "Toughness":
		oldCharacteristic = h.Toughness(c.Source)
		setToughness = true
	default:
		return
	}
	oldLife := h.Game().Players[player].Life
	life := events.Event{Kind: events.LifeChange, Player: player,
		Amount: oldCharacteristic - oldLife}
	if exchange, ok := h.(interface {
		ExchangeLifeVariant(events.Event, state.ObjID, state.PlayerID, int32, bool, bool)
	}); ok {
		exchange.ExchangeLifeVariant(life, c.Source, c.Controller, oldLife, setPower, setToughness)
		return
	}
	h.Emit(life)

	ce := state.ContinuousEffect{
		Source: c.Source, Controller: c.Controller, Affects: "Card.Self",
		Layer: state.LPT, Sub: state.SubSet, HasSet: true,
		SetPower: oldLife, SetToughness: oldLife,
		SetPowerPresent: setPower, SetToughnessPresent: setToughness,
		StaticSet: true,
	}
	h.AddContinuous(ce)
}

func effLoseLife(h Host, c *Ctx, sa *cards.SA) {
	n := Num(h, c, sa, "LifeAmount", 1)
	if n < 0 {
		n = 0
	}
	// A single "each opponent loses life" instruction is simultaneous even
	// though its per-player LifeChange events are serialized in the log. Keep
	// the whole operation in the shared boundary so LifeLostAll sees one group.
	if b, ok := h.(interface {
		BeginLifeLossBatch()
		EndLifeLossBatch()
	}); ok {
		b.BeginLifeLossBatch()
		defer b.EndLifeLossBatch()
	}
	var total int32
	for _, t := range actingPlayers(h, c, sa) {
		h.Emit(events.Event{Kind: events.LifeChange, Player: t, Amount: -n})
		total += n
	}
	// Forge's AFLifeLost is the sum requested by this LoseLife instruction.
	// Write even zero: the source can retain a value from an earlier resolution.
	h.Emit(events.Event{Kind: events.StoreSVar, Obj: c.Source, Text: "AFLifeLost", Amount: total})
}
