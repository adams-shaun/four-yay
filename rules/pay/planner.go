package pay

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/effects/params"
	"github.com/adams-shaun/gorge/events"
	costvocab "github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

// GainLifeCostPlayers lists the seats a GainLife cost part's Spec names
// relative to the payer, in APNAP order starting at the payer (the same walk
// every multi-player effect uses). Spec is Forge's raw player word
// (Player.Opponent / Player.Other); both mean "a player other than the
// payer", and the engine evaluates them through the ONE shared player-spec
// evaluator (effects.MatchesPlayerSpec) so this can never disagree with the
// ValidPlayer$ rider arm that reads the same call. The payer is never its
// own gain-life target ("an opponent"/"each other player"), so a part that
// somehow matched the payer is skipped -- harmless for the corpus spellings,
// and it keeps the payer's own life out of a cost payment.
func GainLifeCostPlayers(e Engine, payer state.PlayerID, part costvocab.CostPart) []state.PlayerID {
	var out []state.PlayerID
	for _, p := range e.Game().AliveFrom(payer) {
		if p == payer {
			continue
		}
		if effects.MatchesPlayerSpec(e.Game(), part.Spec, p, payer) {
			out = append(out, p)
		}
	}
	return out
}

// PayGainLifeCost settles the payer's GainLife<N/Player...> cost parts
// (Forge CostGainLife; Invigorate/Reverent Silence/Skyshroud Cutter): each
// part has every player its Spec names relative to the payer gain N life as
// one POSITIVE LifeChange per player. That is exactly the oracle for the
// /* "each other player" spelling and exact in the two-seat game for the
// bare Player.Opponent spelling; in a larger pod the bare spelling pays
// EVERY opponent rather than one chosen opponent (a documented deviation --
// the alternative-cost route has no mid-cast choose-an-opponent ask). The
// emit routes through applyReplacements/applyLifeReplacements, so a CR 616
// GainLife replacement and the CantGainLife static apply for free.
func PayGainLifeCost(e Engine, payer state.PlayerID, parts []costvocab.CostPart) {
	for _, part := range parts {
		if part.N <= 0 {
			continue
		}
		for _, p := range GainLifeCostPlayers(e, payer, part) {
			e.Emit(events.Event{Kind: events.LifeChange, Player: p, Amount: part.N})
		}
	}
}

// ResolveChosenColors resolves a SetColor$/AddColor$ value against the static
// host's own recorded "as this enters, choose a color" / CR 903.4b pregame
// choice (state.Object.ChosenColor, set by the Choose event the ask emitted).
// It is the layer-5 twin of resolveChosenTypes. A value of "ChosenColor"
// resolves to the host's recorded colour -- the event records a single WUBRG
// letter (rules/resolution.go resumeETBEntry), but a full colour word is accepted too so
// the two spellings cannot drift -- and a host with NO recorded choice fails
// closed: ok=false, the caller emits nothing and the object keeps its printed
// colours (today's shipped behaviour for the whole family).
//
// Everything else passes through the ordinary colour-word parser. A bare
// WUBRG letter is accepted directly (the layer-5 walk at ~1652 reads
// strings.IndexByte("WUBRG", l[0]), so a letter element is already legal),
// which is the shape the recorded choice itself carries; a value the parser
// cannot fully recognise still fails closed, exactly as before.
func ResolveChosenColors(raw string, o *state.Object) ([]string, bool) {
	if strings.EqualFold(strings.TrimSpace(raw), "ChosenColor") {
		if o == nil || o.ChosenColor == "" {
			return nil, false
		}
		if cols, ok := effects.ColorLetters(o.ChosenColor); ok && len(cols) > 0 {
			return cols, true
		}
		// A bare WUBRG letter (the recorded form) bypasses the word parser.
		if l := strings.ToUpper(strings.TrimSpace(o.ChosenColor)); len(l) == 1 && strings.IndexByte("WUBRG", l[0]) >= 0 {
			return []string{l}, true
		}
		return nil, false
	}
	return effects.ColorLetters(raw)
}

// RawBaseCost is id's printed mana cost, without any cost modifier applied:
// the CR 601.2f "mana cost or alternative cost" basis onto which the chosen
// {X} and the RaiseCost/ReduceCost composition (manaToPay) are built. A
// missing object or a Face()-less one degrades to the zero Cost rather than
// panicking, matching adjustedCost's own guard.
func RawBaseCost(e Engine, p state.PlayerID, id state.ObjID) costvocab.Cost {
	o := e.Game().Obj(id)
	if o == nil || o.Face() == nil {
		return costvocab.Cost{}
	}
	return FaceCost(e, o.Face())
}

// CommanderIdentityColours expands seat p's commander colour identity to the
// colours it names, in fixed WUBRG order (the order cards.Face's colour bits
// are declared in, and the order deck/deck.go's identity checks read — no map
// range, so the offer order is deterministic). The identity is the bitwise
// union of every commander's full-card identity (cards.Card.ColourIdentity
// unions over faces), read off the live command-zone objects
// (state.Player.Commanders, populated at genesis and stable across zone
// moves). A seat with no commanders — or commanders whose identity is empty
// (colourless, CR 903.4) — yields a nil slice: "any color" of an empty
// identity is no colour at all, so the caller keeps its fail-closed
// behaviour.
func CommanderIdentityColours(e Engine, p state.PlayerID) []string {
	if int(p) >= len(e.Game().Players) {
		return nil
	}
	var m uint8
	for _, cid := range e.Game().Players[p].Commanders {
		o := e.Game().Obj(cid)
		if o == nil || o.Card == nil {
			continue
		}
		m |= o.Card.ColourIdentity()
		// CR 903.4b: a commander whose printed CDA says "choose a color before
		// the game begins" derives its identity from the recorded choice. Gate
		// on the CDA static, never the bare ChosenColor field: a commander with
		// an ordinary "as this enters" colour choice must not leak its
		// battlefield choice into its identity, and before the pregame answer
		// (or for a non-commander) ChosenColor is empty anyway.
		if o.Card.Faces[0] != nil && o.Card.Faces[0].CommanderColourChoiceCDA() {
			if cols, ok := ResolveChosenColors("ChosenColor", o); ok {
				for _, l := range cols {
					if len(l) == 0 {
						continue
					}
					if i := strings.IndexByte("WUBRG", l[0]); i >= 0 {
						m |= 1 << uint(i)
					}
				}
			}
		}
	}
	var cols []string
	for i, sym := range []string{"W", "U", "B", "R", "G"} {
		if m&(1<<uint(i)) != 0 {
			cols = append(cols, sym)
		}
	}
	return cols
}

// PaymentPlanPoolAccepted is paymentPlanPoolOK, relaxed inside a
// PotentialPaymentPlans query (paymentPlanPotentialPool): there the witness
// is never submitted as an Intent.Payment -- the seat taps its sources on
// the manual surface and the ordinary payment spends the pool -- so snow,
// persistent and producer-typed units (Treasure, Cave, Desert, artifact
// mana) are ordinary mana of their colour. Restricted mana, which only some
// spells may spend, is still declined.
func PaymentPlanPoolAccepted(e Engine, p state.PlayerID) bool {
	pl := &e.Game().Players[p]
	if e.Session().PaymentPlanPotentialPool {
		return len(pl.RestrictedMana) == 0
	}
	return PlanPoolOK(pl)
}

// PaymentPlanChoiceColours returns the concrete colours a choice-shaped
// Produced$ resolves to for source id, or nil when the production is fixed
// or cannot be resolved. The order is deterministic: WUBRG for Produced$ Any
// and a commander identity is already WUBRG (commanderIdentityColours), and
// the ability's own token order for a Combo -- the same order
// manaAbilityComboColours and askManaColor use. A bare Chosen/ChosenColor
// with nothing recorded, a Combo whose tokens name no plain colour, and an
// empty commander identity all yield nil: V1 fails closed rather than
// inventing a colour.
func PaymentPlanChoiceColours(e Engine, id state.ObjID, ma *cards.SA) []string {
	raw := params.ManaOf(ma).Produced
	switch paymentPlanChoiceColoursCodes.Code(string(raw)) {
	case paymentPlanChoiceColoursAny:
		return []string{"W", "U", "B", "R", "G"}
	case paymentPlanChoiceColoursChosen:
		if col := ChosenProducedColour(e.Game(), id); col != "" {
			return []string{col}
		}
		return nil
	case paymentPlanChoiceColoursColorIdentity:
		return CommanderIdentityColours(e, PlanController(e.Game(), id))
	}
	// Reuse the manual wheel's own flattener: it substitutes a recorded
	// Chosen tail and dedups a recorded colour equal to a fixed token, so the
	// plan and the wheel cannot disagree about a Combo that resolves cleanly.
	if cols, ok := ManaAbilityComboColours(ma, ChosenProducedColour(e.Game(), id)); ok {
		return cols
	}
	if !strings.HasPrefix(raw, "Combo ") {
		return nil
	}
	// A Combo still naming a token manaAbilityComboColours cannot flatten (a
	// Chosen with nothing recorded, a ColorIdentity) has no single colour
	// list; walk its tokens so a fixed token ("Combo U Chosen" with nothing
	// recorded) still yields its own colour, and fail closed on any token this
	// engine cannot resolve ("Combo Any", "Special ...").
	var cols []string
	for _, tok := range strings.Fields(raw) {
		switch {
		case tok == "Combo":
		case tok == "Chosen" || tok == "ChosenColor":
			if col := ChosenProducedColour(e.Game(), id); col != "" {
				cols = AppendColourOnce(cols, col)
			}
		case tok == "ColorIdentity":
			for _, col := range CommanderIdentityColours(e, PlanController(e.Game(), id)) {
				cols = AppendColourOnce(cols, col)
			}
		case len(tok) == 1 && strings.ContainsRune("WUBRG", rune(tok[0])):
			cols = AppendColourOnce(cols, tok)
		default:
			return nil
		}
	}
	if len(cols) == 0 {
		return nil
	}
	return cols
}

// PaymentPlanHandDemand measures the acting player's OWN hand's colour
// demand (spec 5 key 6): for each WUBRG colour, the largest number of that
// colour's pips on any single nonland card in hand other than the card being
// cast. It reads only the acting player's own hand and the printed mana
// costs, so an opponent's hand and every other zone stay out of the rank.
func PaymentPlanHandDemand(e Engine, p state.PlayerID, exclude state.ObjID) [5]int {
	var demand [5]int
	for _, id := range e.Game().Zone(state.ZHand, p) {
		if id == exclude {
			continue
		}
		o := e.Game().Obj(id)
		if o == nil || o.Face() == nil || o.Face().IsLand() {
			continue
		}
		pips := CostPips(RawBaseCost(e, p, id))
		for c := range demand {
			if pips[c] > demand[c] {
				demand[c] = pips[c]
			}
		}
	}
	return demand
}

type paymentPlanChoiceColoursCode uint16

const (
	paymentPlanChoiceColoursAny paymentPlanChoiceColoursCode = iota + 1
	paymentPlanChoiceColoursChosen
	paymentPlanChoiceColoursColorIdentity
)

var paymentPlanChoiceColoursCodes = state.NewStrCodes(
	state.StrEntry[paymentPlanChoiceColoursCode]{Key: "Any", Val: paymentPlanChoiceColoursAny},
	state.StrEntry[paymentPlanChoiceColoursCode]{Key: "Chosen", Val: paymentPlanChoiceColoursChosen},
	state.StrEntry[paymentPlanChoiceColoursCode]{Key: "ChosenColor", Val: paymentPlanChoiceColoursChosen},
	state.StrEntry[paymentPlanChoiceColoursCode]{Key: "ComboChosen", Val: paymentPlanChoiceColoursChosen},
	state.StrEntry[paymentPlanChoiceColoursCode]{Key: "ColorIdentity", Val: paymentPlanChoiceColoursColorIdentity},
)

// PaymentPlanProductions lists the concrete Produced$ values a plan can
// execute for ma: the fixed declaration itself, or each colour a choice
// shape resolves to (the withProduced rewrite execution activates).
func PaymentPlanProductions(e Engine, id state.ObjID, ma *cards.SA) []string {
	mp := params.ManaOf(ma)
	raw := mp.Produced
	if mp.CountsAny {
		return PaymentPlanChoiceColours(e, id, ma)
	}
	return []string{raw}
}

// PaymentPlanInterferenceCarriers lists, in object-ID order, every object
// any of whose card faces or mutated under-cards prints a Taps/TapsForMana
// trigger or a ProduceMana replacement. It is zone-agnostic -- the matchers
// own the zone gates -- so it changes only when an object is created or a
// logged event runs, and is memoised on exactly that key.
func PaymentPlanInterferenceCarriers(e Engine) []state.ObjID {
	if e.Session().PaymentPlanCarriersValid && e.Session().PaymentPlanCarriersObjs == len(e.Game().Objs) && e.Session().PaymentPlanCarriersEvents == len(e.Log().Events) {
		return e.Session().PaymentPlanCarriers
	}
	carries := func(f *cards.Face) bool {
		if f == nil {
			return false
		}
		for _, t := range f.Triggers {
			if t.Mode == "Taps" || t.Mode == "TapsForMana" {
				return true
			}
		}
		for _, r := range f.Repls {
			if r.Event == "ProduceMana" {
				return true
			}
		}
		return false
	}
	var out []state.ObjID
	for i := range e.Game().Objs {
		o := &e.Game().Objs[i]
		found := o.Card != nil && slices.ContainsFunc(o.Card.Faces, carries)
		for j := 0; !found && j < len(o.MergedCards); j++ {
			found = carries(o.MergedFaceAt(j))
		}
		if found {
			out = append(out, o.ID)
		}
	}
	// A fresh slice every rebuild: a Clone never shares this memo, and a
	// caller may still be walking the previous one.
	e.Session().PaymentPlanCarriers, e.Session().PaymentPlanCarriersValid = out, true
	e.Session().PaymentPlanCarriersObjs, e.Session().PaymentPlanCarriersEvents = len(e.Game().Objs), len(e.Log().Events)
	return out
}

// PaymentPlanQueryBegin installs a query scope -- a still-valid enclosing
// scope is reused -- and returns its undo for paymentPlanQueryEnd, without
// a closure:
//
//	defer e.paymentPlanQueryEnd(e.paymentPlanQueryBegin())
//
// A scope is pure per-query cache; the one a finished query leaves is reset
// and reused by the next (paymentPlanQueryFree), never while installed,
// kept, or owned by another engine (a by-value Engine copy).
func PaymentPlanQueryBegin(e Engine) QueryTok {
	if e.Session().PlanQuery.Valid(e.Log()) {
		return QueryTok{}
	}
	q := e.Session().PlanQueryFree
	if q != nil && q.Owner == e.Session() {
		e.Session().PlanQueryFree = nil
		units, alts, classes, arena, plans := q.Units, q.Alts, q.Classes, q.AltArena, q.Plans
		clear(units)
		clear(alts)
		clear(classes)
		clear(arena)
		clear(plans)
		*q = PlanQuery{Owner: e.Session(), Units: units, Alts: alts, Classes: classes, AltArena: arena[:0], Plans: plans[:0]}
	} else {
		q = &PlanQuery{Owner: e.Session()}
	}
	q.LogLen = len(e.Log().Events)
	if q.LogLen > 0 {
		q.LogBase = &e.Log().Events[0]
	}
	q.Installs = 1
	t := QueryTok{Q: q, Prev: e.Session().PlanQuery}
	e.Session().PlanQuery = q
	return t
}

// PaymentPlanQueryEnd undoes one paymentPlanQueryBegin (or
// paymentPlanQueryResumeBegin).
func PaymentPlanQueryEnd(e Engine, t QueryTok) {
	if t.Q == nil {
		return
	}
	e.Session().PlanQuery = t.Prev
	t.Q.Installs--
	if t.Q != e.Session().PlanQueryKept {
		PaymentPlanQueryRecycle(e, t.Q)
	}
}

// PaymentPlanQueryRecycle hands a finished scope to the free slot: one this
// engine made, installed nowhere (installs 0) and not kept.
func PaymentPlanQueryRecycle(e Engine, q *PlanQuery) {
	if q != nil && q.Owner == e.Session() && q.Installs == 0 && q != e.Session().PlanQueryKept {
		e.Session().PlanQueryFree = q
	}
}

func UnlessPaymentCandidates(e Engine, u *UnlessPayment, zone state.Zone, kind string, part costvocab.CostPart) []state.ObjID {
	// The dedup is over the union of every component's picks, not just this
	// one's list: one card must not pay two parts, and a card already
	// sacrificed has left its zone anyway, so the wider union only ever
	// removes an already-impossible candidate. Revealed and beheld cards are
	// NOT removed from the hand, which is exactly why they need the explicit
	// exclusion.
	used := make([]state.ObjID, 0, len(u.Sacs)+len(u.Discards)+len(u.Reveals)+len(u.Beholds)+len(u.Returns)+len(u.Exiles))
	used = append(used, u.Sacs...)
	used = append(used, u.Discards...)
	used = append(used, u.Reveals...)
	used = append(used, u.Beholds...)
	used = append(used, u.Returns...)
	used = append(used, u.Exiles...)
	return UnlessCandidatesFor(e, u.Payer, u.Ctx, zone, kind, part, used)
}

func RecordUnlessPaymentPick(e Engine, u *UnlessPayment, kind string, ids []state.ObjID) {
	switch recordUnlessPaymentPickCodes.Code(string(kind)) {
	case recordUnlessPaymentPickSacrifice:
		u.Sacs = append(u.Sacs, ids...)
	case recordUnlessPaymentPickRevealcost:
		u.Reveals = append(u.Reveals, ids...)
	case recordUnlessPaymentPickBeholdcost:
		u.Beholds = append(u.Beholds, ids...)
	case recordUnlessPaymentPickReturncost:
		u.Returns = append(u.Returns, ids...)
	case recordUnlessPaymentPickExilecost:
		u.Exiles = append(u.Exiles, ids...)
	default:
		u.Discards = append(u.Discards, ids...)
	}
}

func UnlessCountersAffordable(e Engine, u *UnlessPayment) bool {
	return UnlessCountersAffordableFor(e, u.Cost, u.Ctx, u.StackObj)
}

type recordUnlessPaymentPickCode uint16

const (
	recordUnlessPaymentPickSacrifice recordUnlessPaymentPickCode = iota + 1
	recordUnlessPaymentPickRevealcost
	recordUnlessPaymentPickBeholdcost
	recordUnlessPaymentPickReturncost
	recordUnlessPaymentPickExilecost
)

var recordUnlessPaymentPickCodes = state.NewStrCodes(
	state.StrEntry[recordUnlessPaymentPickCode]{Key: "sacrifice", Val: recordUnlessPaymentPickSacrifice},
	state.StrEntry[recordUnlessPaymentPickCode]{Key: "revealcost", Val: recordUnlessPaymentPickRevealcost},
	state.StrEntry[recordUnlessPaymentPickCode]{Key: "beholdcost", Val: recordUnlessPaymentPickBeholdcost},
	state.StrEntry[recordUnlessPaymentPickCode]{Key: "returncost", Val: recordUnlessPaymentPickReturncost},
	state.StrEntry[recordUnlessPaymentPickCode]{Key: "exilecost", Val: recordUnlessPaymentPickExilecost},
)
