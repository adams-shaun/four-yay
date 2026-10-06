package effects

import (
	"strconv"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// effDelayedTrigger implements Mode$ Phase delayed triggers -- the
// "at the beginning of the next end step, return it" shape (Flickerwisp and
// its family, CR 603.7). It registers a delayed trigger by emitting a
// DelayedRegister event, which events.Apply folds into state.Game.Delayed so
// the registration survives replay (a delayed trigger is registered during
// one resolution and fires later, in general a different turn). The engine
// then, on entering the registered phase, mints a triggered-ability stack
// object for it through events.DelayedPush and it resolves like any other
// triggered ability.
//
// The registration carries the source object (whose face's SVar table holds
// the Execute$ sub-ability), the controller, the phase to fire in, the
// Execute$ SVar name, and the Remembered captured at registration -- which
// is what a later Defined$ DelayTriggerRememberedLKI resolves against when
// the delayed trigger fires (Flickerwisp's DelTrig remembers the exiled
// permanent via the ChangeZone's RememberChanged$ True, so TrigBounce knows
// which object to return).
//
// Mode$ Phase and the event-matched delayed modes all use the same
// registration event. Event-matched bodies are stored inline because a
// DelayedTrigger SA is not itself an SVar that events.Apply could resolve.
//
// Every parameter is read through the compiled DelayedTriggerParams
// (delayedtrigger_params.go), registration texts included.
func effDelayedTrigger(h Host, c *Ctx, sa *cards.SA) {
	dp := DelayedTriggerOf(sa)
	noteUnreadParams(h, c, "DelayedTrigger", dp.Unread)
	mode := dp.Mode
	if mode == "SpellCast" {
		effDelayedTriggerSpellCast(h, c, dp)
		return
	}
	if mode != "Phase" && !effectOneShotDelayedMode(mode) {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "registers a delayed trigger at " + mode + " (not implemented)"})
		return
	}
	var step state.Step
	if mode == "Phase" {
		set, unknown := state.ParsePhases(dp.Phase)
		if len(unknown) > 0 {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "registers a delayed trigger at unrecognized phase " + dp.Phase})
			return
		}
		// Register the first listed phase still ahead; firing consumes the
		// registration, so a multi-step Phase$ value remains one-shot.
		var ok bool
		step, ok = state.EarliestAfter(set, h.Game().Step)
		if !ok {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "registers a delayed trigger with no Phase"})
			return
		}
	}
	// An absent RememberObjects$ (and the bare RememberedLKI spelling) keeps
	// the resolving chain's capture. Any other recognised value REPLACES that
	// capture, including with an empty set: the delayed body acts on the
	// objects its own parameter names, not also on the card/player that led to
	// this chain (Kharasha Foothills and Shredder, Shadow Master). An unknown
	// value is loud and preserves the historical chain-capture fallback.
	remembered := c.Remembered
	replacedRemembered := false
	if spec := dp.RememberObjects; spec != "" && spec != "RememberedLKI" {
		if ts, known := knownDefinedTargets(h, c, spec); known {
			remembered = copyTargets(ts)
			replacedRemembered = true
		} else {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unmodelled DelayedTrigger RememberObjects$ " + spec})
		}
	}
	// NextTurn$ True (Mishra's/Urza's/Lodestone Bauble's slowtrip: "draw a
	// card at the beginning of the NEXT turn's upkeep"): the one-shot fires
	// in a LATER turn only. The decode folds Amount into the registration's
	// MinTurn (the same bound the ExtraTurn grant's registration carries),
	// so the first Upkeep still inside the current turn does not consume the
	// registration — the exact defect a bauble activated during its own
	// upkeep would otherwise hit. An absent flag keeps the unbounded fire
	// every earlier registration had (Amount zero).
	amount := int32(0)
	if dp.NextTurn {
		amount = int32(h.Game().Turn + 1)
	}
	// The registration's ValidPlayer$ rides the event's Text next to the
	// phase: "<Phase>|VP=<value>". The rules-side delayed scan gates the
	// fire on it at the phase occurrence (Necropotence's "YOUR next end
	// step" -- a phase the gate fails leaves the one-shot registration
	// pending for the first later occurrence that matches), and the view
	// layer strips the suffix for display.
	//
	// The Phase registration's IsPresent$/PresentZone$/PresentCompare$
	// condition (Bank Job's "at the beginning of the next end step, if that
	// card is still exiled") rides the same Text as further pipe suffixes.
	// The rules-side delayed scan gates the fire on it at the phase
	// occurrence, evaluating Card.IsTriggerRemembered against the
	// registration's own remembered capture; a registration with no
	// IsPresent$ carries none of the spelling and fires ungated exactly as
	// before. The values are Forge tokens with no "|", so the decode's
	// LastIndex strips are exact. Both are compiled into PhaseText.
	text := dp.PhaseText
	// RememberChain$ False (this repo's own generated-SA param, the
	// Annihilator$-marker precedent: no raw corpus card carries it, only
	// cards/keywords.go's generated Mobilize delay SVar does): the
	// registration keeps only what THIS resolving chain itself remembered
	// beyond the referents its triggering event captured -- Ctx.Captured is
	// exactly the part of Remembered the trigger put there (the attacking
	// creature, the defending player), RememberTokens$ True put the minted
	// tokens in the chain part -- so Mobilize's end-step sacrifice touches
	// the Warrior tokens and never the creature that merely triggered. The
	// default (absent) keeps the whole-chain capture every earlier
	// registration had, byte for byte.
	if !replacedRemembered && dp.RememberChainFalse {
		chain := make([]state.Target, 0, len(c.Remembered))
		for _, t := range c.Remembered {
			captured := false
			for _, cp := range c.Captured {
				if cp == t {
					captured = true
					break
				}
			}
			if !captured {
				chain = append(chain, t)
			}
		}
		remembered = chain
	}
	exec := dp.Execute
	if mode != "Phase" {
		if exec == "" {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "registers a delayed " + mode + " trigger with no Execute"})
			return
		}
		eventText := dp.EventText
		if dp.ThisTurn {
			eventText += "|TT=" + strconv.Itoa(int(h.Game().Turn))
		}
		h.Emit(events.Event{Kind: events.DelayedRegister, Obj: c.Source,
			Player: c.Controller, Step: h.Game().Step, Counter: exec,
			IDs: encodeRemembered(remembered), Text: eventText})
		return
	}
	if exec == "" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "registers a delayed trigger with no Execute"})
		return
	}
	// RememberNumber$ True (Mana Drain, Plasm Capture, Scattering Stroke):
	// Forge's DelayedTriggerEffect copies the host's remembered Integers onto
	// the delayed trigger, which its body reads back through
	// Count$TriggerRememberAmount ("add an amount of {C} equal to that
	// spell's mana value"). The chain's remembered number is the Counter
	// primitive's RememberCounteredCMC$ binding; it rides "|RN=<n>", appended
	// LAST so the decode strips it first. An unbound chain remembers nothing
	// and the registration is byte-identical to before.
	if dp.RememberNumber && c.Num.RememberedCMCBound {
		text += "|RN=" + strconv.Itoa(int(c.Num.RememberedCMC))
	}
	h.Emit(events.Event{Kind: events.DelayedRegister, Obj: c.Source,
		Player: c.Controller, Step: step, Counter: exec, Amount: amount,
		IDs: encodeRemembered(remembered), Text: text})
}

// effDelayedTriggerSpellCast registers the event-matched delayed shape: a
// Mode$ SpellCast DelayedTrigger (Mistrise Village's "{U}, {T}: The next
// spell you cast this turn can't be countered") fires on a spell's
// PutOnStack exactly like checkEventDelayedTriggers' keyword-minted
// registrations do. The registration is one-shot (the DelayedPush that
// fires it removes it), so "the NEXT spell" is exactly one spell. The
// SA's own trigger clauses (ValidCard$, ValidActivatingPlayer$) are stored
// INLINE in the event's Text (compiled into SpellCastText) — a face
// Ability's DelayedTrigger has no SVar name of its own for the decode to
// reference — and the fire-time matcher re-parses them against the actual
// cast. ThisTurn$ True (Mistrise) bounds the registration to the CURRENT
// turn ("...you cast THIS TURN"): the expiry rides "|TT=<turn>" and folds
// into state.DelayedTrigger.MaxTurn; a turn that ends with the registration
// unfired leaves it inert forever (skipped, never removed — removal would
// need its own event). Static$ True (the corpus's only value, 8 raw
// DelayedTrigger lines) marks Forge's static-style registration; every
// registration here is already source-independent once created (CR 603.7),
// so the gate below documents the carrier and a future non-True value gets
// the loud Note the fail-closed convention takes.
func effDelayedTriggerSpellCast(h Host, c *Ctx, dp *DelayedTriggerParams) {
	if st := dp.Static; st != "" && !isTrue(st) {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unmodelled DelayedTrigger Static$ " + st})
	}
	exec := dp.Execute
	if exec == "" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "registers a delayed SpellCast trigger with no Execute"})
		return
	}
	text := dp.SpellCastText
	if dp.ThisTurn {
		text += "|TT=" + strconv.Itoa(int(h.Game().Turn))
	}
	h.Emit(events.Event{Kind: events.DelayedRegister, Obj: c.Source,
		Player: c.Controller, Step: h.Game().Step, Counter: exec, Text: text})
}
