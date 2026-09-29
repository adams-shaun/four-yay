package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	Register("GainLife", effGainLife)
	Register("LoseLife", effLoseLife)
	Register("SetLife", effSetLife)
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

// effSetLife implements CR 119.5: "If an effect would cause a player's life
// total to become a certain number, that player gains or loses the amount of
// life necessary to make it that number. If the effect would lower the
// player's life total below 0, that player loses the game." The SET is a
// delta LifeChange -- a positive delta is a gain (CR 119.5's own words) and a
// negative one a loss -- so the ordinary GainLife/LifeReduced replacement
// machinery (a CantGainLife lock, a lifegain doubler) applies exactly as it
// does to api:GainLife/api:LoseLife, with no separate set-the-field path that
// could bypass it.
//
// `Defined$`/`ValidTgts$` select the affected players through the shared
// actingPlayers grammar (default: the resolving ability's controller, i.e.
// Forge's paramOrDefault("Defined", "You")). LifeAmount$ is resolved ONCE
// through NumResolvedStrict, before any target is touched: an unresolvable
// amount is a loud degrade (a Note and no life change) rather than Num's
// documented degrade-to-zero, because a set-to-zero would silently lose a
// player the game. A zero delta emits nothing -- a life total that already
// equals the target has neither gained nor lost life.
func effSetLife(h Host, c *Ctx, sa *cards.SA) {
	target, ok := NumResolvedStrict(h, c, sa, "LifeAmount", 0)
	if !ok {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "SetLife: unresolvable LifeAmount"})
		return
	}
	for _, t := range actingPlayers(h, c, sa) {
		if int(t) >= len(h.Game().Players) {
			continue
		}
		delta := target - h.Game().Players[t].Life
		if delta == 0 {
			continue
		}
		h.Emit(events.Event{Kind: events.LifeChange, Player: t, Amount: delta})
	}
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
