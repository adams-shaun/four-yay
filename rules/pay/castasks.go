package pay

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	costvocab "github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

// The cast's payment asks and their answer records (lasagna spec §9.2, E7
// flow slice 4): the Convoke/Harmonize/Improvise/waterbend announcement, the
// flexible-pip announcement (CR 601.2b) and the Optional$ ManaConvert
// election, each posed through Engine.Ask (AskCast), and the record of every
// payment-shaped cast answer into the CastPayment / PaidCost pair. What the
// cast does around them -- the stage order, the cost composition each prices
// against, the non-payment answers -- is the engine's flow.

// ConvokeOffer is what the engine composes for a cast's Convoke-family
// announcement: who casts what, which of the four contributions the cast may
// take, the mana cost the contributions pay toward, and the waterbend cap.
type ConvokeOffer struct {
	Player state.PlayerID
	Card   state.ObjID
	// Convoke, Harmonize, Improvise and Waterbend are the contributions the
	// cast may announce (the cast-only keywords are false for an ability).
	Convoke, Harmonize, Improvise, Waterbend bool
	// Mana is the composed mana cost still owed; HasX is an unannounced {X}
	// (every generic contribution stays possible until X is announced).
	Mana Cost
	HasX bool
	// WaterbendN is the fixed waterbend amount; WaterbendOpen is a cap not
	// yet known (a Waterbend<X> or an X-raised amount).
	WaterbendN    int32
	WaterbendOpen bool
}

// ConvokeAsk poses the announcement of every permanent used for Convoke,
// Harmonize, Improvise or a waterbend cost. It is deliberately before the
// mana window: tapping is part of paying, so a chosen creature cannot first
// be used as a mana source. It reports whether it asked.
func ConvokeAsk(e Engine, cp *CastPayment, paid *PaidCost, in ConvokeOffer) bool {
	mana := in.Mana
	hasX := in.HasX
	if !mana.HasManaPayment() && !hasX {
		return false
	}
	g := e.Game()
	r := e.Chars()
	name := g.Obj(in.Card).Face().Name
	d := &decision.Decision{Player: in.Player, Kind: decision.KChoose, Min: 0, Source: in.Card}
	sawCreature, sawArtifact := false, false
	for _, id := range g.Zone(state.ZBattlefield, in.Player) {
		o := g.Obj(id)
		if o == nil || o.Tapped || o.Face() == nil || o.BestowedAttached() || o.ReconfiguredAttached() || cp.Committed(paid, id) {
			continue
		}
		group := fmt.Sprintf("payment:%d", id)
		if in.Harmonize && o.EffectiveIsCreature() && (mana.Generic > 0 || hasX) {
			// The reduction offered is the creature's ACTUAL power (CR
			// 702.46a), the same number the Harmonize payment credits: a
			// printed 1/1 currently boosted to 4 funds four generic, and a
			// printed 4/4 reduced to 1 funds only one.
			if p := r.Power(id); p > 0 {
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "harmonize", Obj: id,
					Group: group, Amount: int(p), Label: "Tap " + o.Face().Name + " (reduce by " + strconv.Itoa(int(p)) + ")"})
				sawCreature = true
			}
		}
		if in.Convoke && o.EffectiveIsCreature() {
			for _, color := range []byte{'W', 'U', 'B', 'R', 'G'} {
				if mana.Colored[state.ManaIndex(color)] > 0 && strings.Contains(r.Colors(id), string(color)) {
					d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "convoke_" + string(color), Obj: id,
						Group: group, Label: "Tap " + o.Face().Name + " for " + string(color)})
					sawCreature = true
				}
			}
			if mana.Generic > 0 || hasX {
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "convoke_generic", Obj: id,
					Group: group, Label: "Tap " + o.Face().Name + " for 1"})
				sawCreature = true
			}
		}
		// CR 702.66a: Improvise's artifacts -- artifact creatures included,
		// the card is an artifact independently of being a creature -- each
		// pay one generic. The shared payment group makes the object's
		// Convoke and Improvise options mutually exclusive, so one artifact
		// can never be committed to both payments.
		genericOffered := false
		if in.Improvise && o.EffectiveIsArtifact() && (mana.Generic > 0 || hasX) {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "improvise_generic", Obj: id,
				Group: group, Label: "Tap " + o.Face().Name + " for 1"})
			sawArtifact = true
			genericOffered = true
		}
		if in.Convoke && o.EffectiveIsCreature() && (mana.Generic > 0 || hasX) {
			genericOffered = true
		}
		// Waterbend: an artifact or creature not already offered a generic
		// payment by the spell's own Convoke/Improvise may tap for {1} of
		// the waterbend amount (the same payment group keeps one object to
		// one contribution).
		if in.Waterbend && !genericOffered && (o.EffectiveIsArtifact() || o.EffectiveIsCreature()) && (mana.Generic > 0 || hasX) {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "waterbend_generic", Obj: id,
				Group: group, Label: "Tap " + o.Face().Name + " to waterbend for 1"})
			if o.EffectiveIsCreature() {
				sawCreature = true
			} else {
				sawArtifact = true
			}
		}
	}
	if len(d.Options) == 0 {
		return false
	}
	// The prompt names what is actually offered: a mixed Convoke/Improvise
	// spell offers both creatures and artifacts, an Improvise-only one only
	// artifacts, and the Convoke/Harmonize shapes only creatures.
	switch {
	case sawCreature && sawArtifact:
		d.Prompt = "Choose permanents to help pay for " + name
	case sawArtifact:
		d.Prompt = "Choose artifacts to help pay for " + name
	default:
		d.Prompt = "Choose creatures to help pay for " + name
	}
	// The announcement cannot tap more creatures than the cost can absorb:
	// each chosen contribution reduces exactly one outstanding slot (a
	// colour pip or one generic), so Max is the outstanding slot count.
	// With an unfixed {X} the generic requirement is not yet known (CR
	// 601.2b announces Convoke before X), so the bound is left open and the
	// X ask prices the announcement against every candidate X instead; the
	// engine's absorption gate plus that pricing close the rest.
	d.Max = len(d.Options)
	if !hasX {
		if slots := int(mana.Colored.Total() + mana.Generic); slots < d.Max {
			d.Max = slots
		}
	}
	if in.Waterbend && !in.Convoke && !in.Harmonize && !in.Improvise && !in.WaterbendOpen && int(in.WaterbendN) < d.Max {
		// Only waterbend taps are offered: at most the waterbend amount. A
		// Waterbend<X> amount is not known until X is announced, so its cap
		// is left open.
		d.Max = int(in.WaterbendN)
	}
	e.Ask(AskCast, d)
	return true
}

// ManaAsk offers the payer's choice for the next unsettled hybrid or
// Phyrexian pip of cost (CR 601.2b), one decision per pip. Every face is
// gated on feasible -- the engine's whole-cost feasibility for the face
// folded in as its final resolved face -- so a player is never offered a
// payment a complete assignment cannot pay. The valid options keep their
// AnnouncePip order, so the deterministic fallback (index 0) always picks a
// legal payment. An empty menu (the state shifted since the offer gate
// measured it) offers the first alternative rather than nothing. It reports
// whether it asked.
func ManaAsk(e Engine, cp *CastPayment, player state.PlayerID, card state.ObjID, cost Cost, feasible func(PipAlt) bool) bool {
	if cp.PayIdx >= cost.AnnPipCount() {
		return false
	}
	alts := AnnouncePip(cost, cp.PayIdx)
	d := &decision.Decision{Player: player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Choose how to pay a mana symbol of " + e.Game().Obj(card).Face().Name,
		Source: card}
	addPip := func(alt PipAlt) {
		switch {
		case alt.Color != 0:
			d.Options = append(d.Options, decision.Option{Index: len(d.Options),
				Kind: "pay_" + string(alt.Color), Label: "Pay " + string(alt.Color), Amount: 1})
		case alt.Generic > 0:
			d.Options = append(d.Options, decision.Option{Index: len(d.Options),
				Kind: "pay_generic", Label: fmt.Sprintf("Pay %d generic", alt.Generic), Amount: int(alt.Generic)})
		case alt.Life > 0:
			d.Options = append(d.Options, decision.Option{Index: len(d.Options),
				Kind: "pay_life", Label: "Pay 2 life", Amount: 2})
		}
	}
	seen := map[byte]bool{}
	seenGeneric := false
	for _, alt := range alts {
		switch {
		case alt.Color != 0:
			if seen[alt.Color] {
				continue
			}
			seen[alt.Color] = true
			if feasible(alt) {
				addPip(alt)
			}
		case alt.Generic > 0:
			if seenGeneric {
				continue
			}
			seenGeneric = true
			if feasible(alt) {
				addPip(alt)
			}
		case alt.Life > 0:
			if feasible(alt) {
				addPip(alt)
			}
		}
	}
	if len(d.Options) == 0 {
		addPip(alts[0])
	}
	e.Ask(AskCast, d)
	return true
}

// ManaConvertAsk poses the Optional$ ManaConvert election once for the cast
// (optional reports whether the cast has an optional conversion). A
// mandatory conversion remains automatic; an optional one is offered even
// when the ordinary pool already pays, because declining is a meaningful
// choice and the grant may matter to a later repricing. It reports whether
// it asked.
func ManaConvertAsk(e Engine, cp *CastPayment, player state.PlayerID, card state.ObjID, optional bool) bool {
	if cp.ManaConvertDone {
		return false
	}
	cp.ManaConvertDone = true
	if !optional {
		return false
	}
	e.Ask(AskCast, &decision.Decision{Player: player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Use optional mana conversion?", Source: card,
		Options: []decision.Option{
			{Index: 0, Kind: "manaconvert", Label: "Use mana conversion", Obj: card, Player: player},
			{Index: 1, Kind: "manaconvert", Label: "Don't use mana conversion", Obj: card, Player: player},
		}})
	return true
}

// CastPayAnswer is the cast state a payment answer records into beside the
// CastPayment: the paid lists, the composed cost whose part lists the
// cursors walk, and the cast's {X} (a dynamic tapXType election announces
// it unless XDone).
type CastPayAnswer struct {
	Paid  *PaidCost
	Cost  *Cost
	X     *int32
	XDone bool
}

// RecordCastPaymentAnswer records a payment-shaped cast answer of kind (the
// chosen option's kind) into cp and in, and reports whether kind was one; the
// engine records every other cast answer itself.
func RecordCastPaymentAnswer(cp *CastPayment, in CastPayAnswer, kind string, chosen []decision.Option) bool {
	paid, cost := in.Paid, in.Cost
	switch castPayCodes.Code(kind) {
	case castPayManaconvert:
		// Option 0 is the affirmative election. The decision is deliberately
		// positional rather than label-based so a translated label cannot
		// alter the payment semantics.
		cp.ManaConvertUse = len(chosen) > 0 && chosen[0].Index == 0
	case castPaySacrifice:
		for _, o := range chosen {
			paid.Sacs = append(paid.Sacs, o.Obj)
			cp.SacPaid++
		}
		part := cost.Sac[cp.SacPart]
		total := int(part.N)
		if part.Announced {
			total = int(*in.X)
		}
		if cp.SacPaid >= total {
			cp.SacPart++
			cp.SacPaid = 0
		}
	case castPaySubcounter:
		// The chosen counter-removal pick of a SubCounter cost part: a
		// wildcard "Any" part records one counter unit per answer (the ask
		// loop keeps asking until the part is fully paid), a fixed-kind part
		// records its one object and advances.
		wildcard := cp.SubCounterPart < len(cost.SubCounter) && strings.EqualFold(cost.SubCounter[cp.SubCounterPart].Spec, "Any")
		for _, o := range chosen {
			cp.SubCounterPays = append(cp.SubCounterPays, SubCounterPay{Part: cp.SubCounterPart, Obj: o.Obj, Kind: o.Counter})
		}
		if !wildcard {
			cp.SubCounterPart++
		}
	case castPayDiscard:
		for _, o := range chosen {
			paid.Discards = append(paid.Discards, o.Obj)
		}
		cp.DiscardPart++
	case castPayExilecost:
		for _, o := range chosen {
			paid.Exiles = append(paid.Exiles, o.Obj)
		}
		cp.ExilePart++
	case castPayRevealcost:
		for _, o := range chosen {
			paid.Reveals = append(paid.Reveals, o.Obj)
			paid.RevealHandArm = append(paid.RevealHandArm, true)
		}
		cp.RevealPart++
	case castPayRevealOrChoose:
		// Either-or cost, REVEAL arm: the elected hand cards are a real
		// public reveal (RevealHandArm true; EmitChoiceCosts announces them).
		for _, o := range chosen {
			paid.Reveals = append(paid.Reveals, o.Obj)
			paid.RevealHandArm = append(paid.RevealHandArm, true)
		}
		cp.RevealOrChoosePart++
	case castPayChoosecost:
		// Either-or cost, CHOOSE arm: a permanent the payer controls, elected
		// at cast time. It rides the same paid list the `Revealed$<Property>`
		// refs read (Forge's CostReveal owns both arms) but RevealHandArm is
		// false, so EmitChoiceCosts announces a choice, never a reveal.
		for _, o := range chosen {
			paid.Reveals = append(paid.Reveals, o.Obj)
			paid.RevealHandArm = append(paid.RevealHandArm, false)
		}
		cp.RevealOrChoosePart++
	case castPayBeholdcost:
		for _, o := range chosen {
			paid.Beholds = append(paid.Beholds, o.Obj)
		}
		cp.BeholdPart++
	case castPayTapcost:
		var part costvocab.CostPart
		if cp.TapPart < len(cost.TapPermanent) {
			part = cost.TapPermanent[cp.TapPart]
		}
		for _, o := range chosen {
			paid.Taps = append(paid.Taps, o.Obj)
		}
		cp.TapPart++
		// A dynamic X-form part whose tap election ANNOUNCED the count (no
		// other announce-bearing part ran the X ask first): the chosen count
		// is the cast's {X} (CR 601.2b), which the pay-time CastInfo then
		// carries to resolution and Count$xPaid reads. A part whose cost
		// pre-announced the X (XDone) settles exactly that value and must not
		// overwrite it.
		if part.Dyn == "X" && !in.XDone {
			*in.X = int32(len(chosen))
		}
	case castPayBlightcost:
		for _, o := range chosen {
			paid.Blights = append(paid.Blights, o.Obj)
		}
		cp.BlightPart++
	case castPayColour:
		// A hybrid or Phyrexian pip paid with pool mana: record which colour.
		if len(chosen) > 0 {
			cp.PayColor[state.ManaIndex(chosen[0].Kind[4])]++
		}
		cp.PayIdx++
	case castPayLife:
		// A Phyrexian face (plain or hybrid) paid with two life.
		cp.PayLife += 2
		cp.PayIdx++
	case castPayGeneric:
		// A monocolour hybrid pip paid with its generic face.
		if len(chosen) > 0 {
			cp.PayGeneric += int32(chosen[0].Amount)
		}
		cp.PayIdx++
	case castPayConvoke:
		for _, choice := range chosen {
			color := byte(0)
			if strings.HasPrefix(choice.Kind, "convoke_") && choice.Kind != "convoke_generic" {
				color = choice.Kind[len("convoke_")]
			}
			cp.Convoke = append(cp.Convoke, ConvokePayment{ID: choice.Obj, Color: color,
				CountsMana: strings.HasPrefix(choice.Kind, "convoke_"), Waterbend: choice.Kind == "waterbend_generic"})
		}
	case castPayHarmonize:
		for _, choice := range chosen {
			cp.Convoke = append(cp.Convoke, ConvokePayment{ID: choice.Obj, Power: int32(choice.Amount)})
		}
	case castPayDone:
		// CR 601.2g: the player declines further mana abilities; pay the cost.
		cp.WindowDone = true
	default:
		return false
	}
	return true
}

type castPayCode uint16

const (
	castPayManaconvert castPayCode = iota + 1
	castPaySacrifice
	castPaySubcounter
	castPayDiscard
	castPayExilecost
	castPayRevealcost
	castPayRevealOrChoose
	castPayChoosecost
	castPayBeholdcost
	castPayTapcost
	castPayBlightcost
	castPayColour
	castPayLife
	castPayGeneric
	castPayConvoke
	castPayHarmonize
	castPayDone
)

var castPayCodes = state.NewStrCodes(
	state.StrEntry[castPayCode]{Key: "manaconvert", Val: castPayManaconvert},
	state.StrEntry[castPayCode]{Key: "sacrifice", Val: castPaySacrifice},
	state.StrEntry[castPayCode]{Key: "subcounter", Val: castPaySubcounter},
	state.StrEntry[castPayCode]{Key: "discard", Val: castPayDiscard},
	state.StrEntry[castPayCode]{Key: "exilecost", Val: castPayExilecost},
	state.StrEntry[castPayCode]{Key: "revealcost", Val: castPayRevealcost},
	state.StrEntry[castPayCode]{Key: "revealorchoose", Val: castPayRevealOrChoose},
	state.StrEntry[castPayCode]{Key: "choosecost", Val: castPayChoosecost},
	state.StrEntry[castPayCode]{Key: "beholdcost", Val: castPayBeholdcost},
	state.StrEntry[castPayCode]{Key: "tapcost", Val: castPayTapcost},
	state.StrEntry[castPayCode]{Key: "blightcost", Val: castPayBlightcost},
	state.StrEntry[castPayCode]{Key: "pay_W", Val: castPayColour},
	state.StrEntry[castPayCode]{Key: "pay_U", Val: castPayColour},
	state.StrEntry[castPayCode]{Key: "pay_B", Val: castPayColour},
	state.StrEntry[castPayCode]{Key: "pay_R", Val: castPayColour},
	state.StrEntry[castPayCode]{Key: "pay_G", Val: castPayColour},
	state.StrEntry[castPayCode]{Key: "pay_C", Val: castPayColour},
	state.StrEntry[castPayCode]{Key: "pay_life", Val: castPayLife},
	state.StrEntry[castPayCode]{Key: "pay_generic", Val: castPayGeneric},
	state.StrEntry[castPayCode]{Key: "convoke_W", Val: castPayConvoke},
	state.StrEntry[castPayCode]{Key: "convoke_U", Val: castPayConvoke},
	state.StrEntry[castPayCode]{Key: "convoke_B", Val: castPayConvoke},
	state.StrEntry[castPayCode]{Key: "convoke_R", Val: castPayConvoke},
	state.StrEntry[castPayCode]{Key: "convoke_G", Val: castPayConvoke},
	state.StrEntry[castPayCode]{Key: "convoke_generic", Val: castPayConvoke},
	state.StrEntry[castPayCode]{Key: "improvise_generic", Val: castPayConvoke},
	state.StrEntry[castPayCode]{Key: "waterbend_generic", Val: castPayConvoke},
	state.StrEntry[castPayCode]{Key: "harmonize", Val: castPayHarmonize},
	state.StrEntry[castPayCode]{Key: "done", Val: castPayDone},
)
