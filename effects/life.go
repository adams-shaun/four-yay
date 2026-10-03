package effects

import (
	"fmt"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
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
	if sa.ParamStr(cards.PKRememberOwnLoss) == "True" || sa.ParamStr(cards.PKRememberDifference) == "True" {
		// Lazily allocate the chain's shared ExchangeMemory (and re-publish it
		// through the seam, the way effFlipCoin publishes a lazily allocated
		// FlipMemory) so an ask this exchange's own walk poses LATER — a
		// replacement body's draw parking a Dredge ask — captures the pointer
		// onto its resume point, and the SubAbility$ continuation a resume
		// rebuilds still shares this same memory.
		m := c.ExchangeMemory
		if m == nil {
			m = &ExchangeMemory{Bound: true}
			c.ExchangeMemory = m
			if emh, ok := h.(exchangeMemoryHost); ok {
				emh.SetResolutionExchangeMemory(m)
			}
		}
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
		exchange.ExchangeLife(first, second, c.Controller, beforeController, c, sa.ParamStr(cards.PKRememberOwnLoss) == "True")
	} else {
		h.Emit(first)
		h.Emit(second)
		if sa.ParamStr(cards.PKRememberOwnLoss) == "True" && (c.Controller == a || c.Controller == b) {
			before := oldA
			if c.Controller == b {
				before = oldB
			}
			if loss := before - h.Game().Players[c.Controller].Life; loss > 0 {
				c.ExchangeMemory.Number = loss
			}
		}
	}
	if sa.ParamStr(cards.PKRememberDifference) == "True" {
		diff := oldA - oldB
		if diff < 0 {
			diff = -diff
		}
		c.ExchangeMemory.Number = diff
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

	mode := sa.ParamStr(cards.PKMode)
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
// equals the target has neither gained nor lost life. Redistribute$ True
// instead chooses a subset and a permutation of their original totals;
// all receipts still go through the same LifeChange replacement boundary.
func effSetLife(h Host, c *Ctx, sa *cards.SA) {
	if sa.ParamStr(cards.PKRedistribute) == "True" {
		// The only corpus shape is PlayerChoices$ Player / ChoiceAmount$ Any.
		// The first choice selects the recipients; subsequent choices consume
		// one original total per recipient. Chosen carries the subset followed
		// by the picked sources, and ChoiceTarget carries the assignment cursor.
		if sa.ParamStr(cards.PKPlayerChoices) != "Player" || sa.ParamStr(cards.PKChoiceAmount) != "Any" {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "SetLife: unsupported redistribution choices"})
			return
		}
		g := h.Game()
		i := c.ChoiceTarget - 1
		if c.ChoiceTarget == 0 {
			choice := ([]state.Target)(nil)

			pool := g.AliveFrom(c.Controller)
			d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Source: c.Source,
				Min: 0, Max: len(pool), Prompt: sa.ParamStr(cards.PKChoicePrompt),
				ResumeKind: "choice", ResumeSA: sa, ResumeTarget: 0}
			for j, p := range pool {
				d.Options = append(d.Options, decision.Option{Index: j, Kind: "player", Player: p, Label: g.Players[p].Name})
			}
			if ans, ok := AskTape(h, d); ok {
				// The resolution kernel's answer in hand: the recipient
				// subset the "choice" re-entry records below.
				choice = ChoiceAnswerTargets(ans)
			} else {
				switch Ask(h, d) {
				case AskAsked, AskNoHost:
					// With no host, choose nobody: the identity permutation.
					return
				}
			}

			c.Chosen = nil // this effect owns the resumed choice list
			choiceRecord(h, c, sa, choice, false)

			i = 0
		} else {
			// Before recording the answered assignment, the accumulated list
			// contains the subset and exactly i consumed sources.
			if i < 0 || i > len(c.Chosen) {
				return
			}
		}
		subsetSize := len(c.Chosen) - i

		for ; i < subsetSize; i++ {
			recipient := c.Chosen[i].Player
			d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Source: c.Source,
				Min: 1, Max: 1, Prompt: fmt.Sprintf("Choose a life total for %s", g.Players[recipient].Name),
				ResumeKind: "choice", ResumeSA: sa, ResumeTarget: 1 + i,
				ResumeChoices: append([]state.Target(nil), c.Chosen...), ResumeChosenValid: c.ChosenValid}
			for _, src := range c.Chosen[:subsetSize] {
				used := false
				for _, pick := range c.Chosen[subsetSize:] {
					if pick.Player == src.Player {
						used = true
						break
					}
				}
				if !used {
					d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "player", Player: src.Player,
						Label: fmt.Sprintf("%d life of %s", g.Players[src.Player].Life, g.Players[src.Player].Name)})
				}
			}
			if ans, ok := AskTape(h, d); ok {
				// The resolution kernel's answer in hand: the source total
				// the "choice" re-entry records for this recipient.
				choiceRecord(h, c, sa, ChoiceAnswerTargets(ans), false)
				continue
			}
			_ = Ask(h, d)

			// No host: keep the recipient's own total (identity). No
			// assignment is applied until all answers have been collected.
			c.Chosen = append(c.Chosen, state.Target{IsPlayer: true, Player: recipient})
		}
		// Nothing can change life between these consecutive choice asks;
		// snapshot the chosen pool before the first LifeChange so a
		// replacement on one receipt cannot alter a later source value.
		totals := make([]int32, subsetSize)
		for j, src := range c.Chosen[:subsetSize] {
			totals[j] = g.Players[src.Player].Life
		}
		for j, recipient := range c.Chosen[:subsetSize] {
			for k, src := range c.Chosen[:subsetSize] {
				if src.Player == c.Chosen[subsetSize+j].Player {
					if delta := totals[k] - g.Players[recipient.Player].Life; delta != 0 {
						h.Emit(events.Event{Kind: events.LifeChange, Player: recipient.Player, Amount: delta})
					}
					break
				}
			}
		}
		return
	}
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
