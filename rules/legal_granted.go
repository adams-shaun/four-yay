package rules

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// grantedAbility is one ability a continuous ability grant (CR 613.1f,
// state.ContinuousEffect.AddAbilities -- a Saga chapter's Animate) gives an
// object right now: the parsed AB and the SVar name on the granting face's
// table that re-resolves it.
type grantedAbility struct {
	sa *cards.SA
	// source is the object the grant came from (state.ContinuousEffect.Source):
	// the static's own permanent, which need not be the affected object the
	// ability is activated from. It is threaded into decision.Option.GrantSource
	// so the activation resolves the SVar body from here while the minted
	// ability's Source stays the recipient.
	source state.ObjID
	svar   string
	// gained marks an ability granted off a FOREIGN card's compiled face
	// (state.ContinuousEffect.GainedFaces): sa is that card's own ability,
	// gainedFrom is the foreign object id (in the scoped zone) and
	// gainedIdx is the index of sa in that face's Abilities. The activation
	// mints through GainedAbilityPush, which names both so a replay
	// re-resolves the identical SA; a zero gainedFrom means the ordinary
	// SVar-anchored grant. The grant's GainsValidAbilities$ filter and
	// GainsAbilitiesLimitPerTurn$ cap are applied at collection (inside
	// grantedAbilities), the one home both the offer loop and the mana
	// collector read, so no consumer can widen the grant.
	gained     bool
	gainedFrom state.ObjID
	gainedIdx  int
	// gainedFace is the foreign face sa was compiled on (gained only): the
	// SVar table a gained mana ability resolves against (gainedManaRef).
	gainedFace *cards.Face
}

// grantedAbilities collects the activated abilities the battlefield's
// AddAbilities grants give id right now. Each entry is gated on the granting
// effect actually applying to id (its Affects spec, "You" bound to the
// effect's own controller) and re-resolved against the granting source's
// face SVar table -- the name travels, never a parsed copy, so a replay
// re-executing the grant reads the identical body. Only AB$ lines grant;
// a name whose body is missing or is not an AB degrades to no grant (the
// same totality stance every SVar resolution takes). Order: active()'s own
// stable layer/timestamp sort, names in the grant's own order.
//
// bindGrantedCostReferents binds every OriginalHost cost part of a granted
// ability to the GRANTOR (the Equipment or Aura whose static grants it):
// Exile<1/OriginalHost> (The Dominion Bracelet), Sac<1/OriginalHost> (Blazing
// Torch, Spare Dagger, Ninja's Kunai and five more equipment grants) and
// tapXType<1/OriginalHost> (Fishing Pole). The spec alone cannot name the
// grantor -- the filter's source is the RECIPIENT carrying the ability -- so
// before the bind these parts matched nothing and the granted ability was
// never offered.
func bindGrantedCostReferents(cost *Cost, grantor state.ObjID) {
	if cost == nil || grantor == 0 {
		return
	}
	bind := func(parts []CostPart) {
		for i := range parts {
			if strings.EqualFold(strings.TrimSpace(parts[i].Spec), "OriginalHost") {
				parts[i].Referent = grantor
			}
		}
	}
	bind(cost.Exile)
	bind(cost.Sac)
	bind(cost.TapPermanent)
}

func (e *Engine) grantedAbilities(p state.PlayerID, id state.ObjID) []grantedAbility {
	var out []grantedAbility
	ces := e.active()
	// No grant anywhere in the list is one board-wide fact per active()
	// build (active_summary.go): skip the per-object scan outright. The scan
	// itself reads each entry in place rather than copying it.
	if !e.activeSummaryOf(ces).hasGrants {
		return nil
	}
	// CR 613.1f: a recipient that lost all abilities keeps only the grants
	// that are not older than the removal (abilityloss.go). Read lazily, at
	// the first grant that applies to id: most grants match no given object,
	// and both checks are pure reads, so their order changes no answer.
	var lossStamp uint32
	lost, lossRead := false, false
	for i := range ces {
		ce := &ces[i]
		if len(ce.AddAbilities) == 0 && len(ce.GainedFaces) == 0 {
			continue
		}
		if !e.matchesSpecFrom(ce.Affects, id, ce.Controller, ce.Source) {
			continue
		}
		if !lossRead {
			lossStamp, lost = e.abilityLoss(e.G.Obj(id))
			lossRead = true
		}
		if lost && lossStamp > ce.Timestamp {
			continue
		}
		// A has-all-abilities-of grant (GainsAbilitiesOf$): each named
		// foreign face's own compiled Abilities are the recipient's to
		// activate -- ACTIVATED abilities only (the parameter means exactly
		// that; the triggered half rides GainedTriggerFaces and the
		// granted-trigger walk reads only that). The face's `Abilities` slice
		// holds only AB-kind SAs (cards' parser appends A: lines as AB/SP/ST
		// kinds; a gained activated ability is the AB ones), matched by the
		// same cards.IsManaAbilityAPI split the offer loop applies, so a gained
		// mana ability flows through the payment path like any other. The
		// grant's GainsValidAbilities$ filter (Sharkey's
		// `Activated.!ManaAbility`, Nicol Bolas Dragon-God's
		// `Activated.Loyalty`) and its GainsAbilitiesLimitPerTurn$ per-turn
		// cap are applied HERE -- the one home both the offer loop and the
		// mana collector (rules/mana_activation.go) read, so no consumer can
		// widen the grant. The index is the face-local position, which
		// GainedAbilityPush re-resolves against the same face a replay
		// rebuilds.
		for _, gf := range ce.GainedFaces {
			if gf.Face == nil {
				continue
			}
			for i, ab := range gf.Face.Abilities {
				if ab == nil || ab.Kind != "AB" {
					continue
				}
				if !e.gainsValidAbilitiesAdmits(ce.GainsValidAbilities, ab) {
					continue
				}
				if ce.GainsLimitPerTurn > 0 &&
					e.gainedActivationsThisTurn(id, gf.Obj, i) >= ce.GainsLimitPerTurn {
					continue
				}
				out = append(out, grantedAbility{sa: ab, source: ce.Source,
					gained: true, gainedFrom: gf.Obj, gainedIdx: i, gainedFace: gf.Face})
			}
		}
		if len(ce.AddAbilities) == 0 {
			continue
		}
		src := e.G.Obj(ce.Source)
		if src == nil || src.Face() == nil {
			continue
		}
		for _, nm := range ce.AddAbilities {
			svars := ce.SVars
			grantor := ce.AbilityGrantor
			if grantor == 0 {
				grantor = ce.Source
			}
			if svars == nil {
				svars = src.Face().SVars
			}
			ab := cards.ResolveSVar(svars, nm)
			if ab == nil || ab.Kind != "AB" {
				continue
			}
			out = append(out, grantedAbility{sa: ab, source: grantor, svar: nm})
		}
	}
	return out
}

// grantedKeywordLines returns the DERIVED granted-keyword lines id carries
// right now that no compiled face of its pile already expands, restricted to
// the four heads that mint an activated ability (cards.GrantedKeywordAbility:
// Cycling, TypeCycling, Saddle, Crew). A printed K: line is expanded at link
// time into the pile's own abilities (the pile walk offers that one); a
// layer-6 AddKeyword$ grant (CR 613.1f -- Tectonic Reformation, Rhet-Tomb
// Mystic, Jo Grant, Homing Sliver, Jandor, Kotori, Astor, Swift
// Reconfiguration) exists only in the derived keyword list and needs the
// synthesis the offer's granted-keyword block runs. Coverage is decided
// against the pile's compiled facts in both directions -- a face whose own
// keyword list holds the line (the expansion the link built, or one the
// printed-A:-line guard suppressed) and a compiled ability tagged
// KeywordLine = the line -- so a card that both prints and is granted the
// same line is offered once. Duplicate derived entries (two identical grants)
// collapse to one; distinct lines (Cycling:2 and Cycling:R) each offer, the
// way two distinct printed K:Cycling lines would. Deterministic: Derived's
// slice order, dedup by first occurrence -- no map range reaches the list.
func (e *Engine) grantedKeywordLines(id state.ObjID) []string {
	if !e.mayDeriveKeywordLine(id) {
		if derivedMemoVerify {
			if got := e.grantedKeywordLinesFull(id); len(got) != 0 {
				panic(fmt.Sprintf("rules: granted-keyword precheck skipped obj %d but the derived walk grants %q", id, got))
			}
		}
		return nil
	}
	return e.grantedKeywordLinesFull(id)
}

// mayDeriveKeywordLine is grantedKeywordLines' exact precheck
// (keywordmay.go): without a possible Cycling/TypeCycling/Saddle/Crew head in
// the derived list the answer is nil, and the offer walk skips the full layer
// walk it otherwise paid for every card in every offered zone.
func (e *Engine) mayDeriveKeywordLine(id state.ObjID) bool {
	return e.mayHaveDerivedKeywordAnyH(id, kwhCycling, kwhTypeCycling, kwhSaddle, kwhCrew)
}

// grantedKeywordLinesFull is grantedKeywordLines without the precheck.
func (e *Engine) grantedKeywordLinesFull(id state.ObjID) []string {
	var out []string
	for _, k := range e.Derived(id).Keywords {
		if !grantedKeywordLinesFullKeys.Has(cards.KeywordHead(k)) {
			continue
		}
		dup := false
		for _, have := range out {
			if strings.EqualFold(have, k) {
				dup = true
				break
			}
		}
		if dup || e.printedKeywordCovered(id, k) || cards.GrantedKeywordAbility(k) == nil {
			continue
		}
		out = append(out, k)
	}
	return out
}

// printedKeywordCovered reports whether a compiled face of id's pile already
// carries the granted keyword line -- either as a keyword entry of its own
// (the printed K: line the link expanded, or one the printed-A:-line guard
// suppressed) or as a compiled ability tagged KeywordLine = line. A stale
// object reads covered (withhold), the offer walk's degradation direction.
func (e *Engine) printedKeywordCovered(id state.ObjID, line string) bool {
	o := e.G.Obj(id)
	if o == nil {
		return true
	}
	for i := 0; i < o.PileFaceCount(); i++ {
		pf, ok := o.PileFaceAt(i)
		if !ok || pf.Face == nil {
			continue
		}
		for _, k := range pf.Face.Keywords {
			if strings.EqualFold(k, line) {
				return true
			}
		}
		for _, ab := range pf.Face.Abilities {
			if strings.EqualFold(ab.ParamStr(cards.PKKeywordLine), line) {
				return true
			}
		}
	}
	return false
}

// gainsValidAbilitiesAdmits reports whether the gained activated ability ab is
// inside the granting static's GainsValidAbilities$ filter. The filter is
// comma alternatives, each `Activated` plus optional dot qualifiers, and an
// ABSENT filter admits everything. The corpus vocabulary (measured over the
// 31 GainsAbilitiesOf files): `Activated` (Drana and Linvala),
// `Activated.!ManaAbility` (Sharkey), `Activated.!Loyalty` (Scheming Fence),
// `Activated.Loyalty` (Nicol Bolas Dragon-God, Kasmina). A qualifier this
// build does not model fails closed -- that alternative admits nothing -- the
// filter convention every spec reader takes, so an unknown restriction can
// never widen the grant. The base word itself must be `Activated`
// (case-insensitive): a differently-named base admits nothing.
func (e *Engine) gainsValidAbilitiesAdmits(spec string, ab *cards.SA) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return true
	}
	for alt := range strings.SplitSeq(spec, ",") {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		// The token splits on the FIRST dot: the base kind, then the qualifier
		// tail (kept whole -- a qualifier this grammar does not name fails
		// closed below rather than being silently dropped).
		dot := strings.IndexByte(alt, '.')
		base, tail := alt, ""
		if dot >= 0 {
			base, tail = alt[:dot], alt[dot+1:]
		}
		if !strings.EqualFold(base, "Activated") {
			continue
		}
		ok := true
		for q := range strings.SplitSeq(tail, ".") {
			switch gainsValidAbilitiesAdmitsCodes.Code(string(strings.TrimSpace(q))) {
			case gainsValidAbilitiesAdmitsEmpty:
				// A trailing dot ("Activated."): no qualifier, vacuous.
			case gainsValidAbilitiesAdmitsManaAbility:
				ok = ok && !cards.IsManaAbilityAPI(ab.API)
			case gainsValidAbilitiesAdmitsLoyalty:
				ok = ok && !e.isLoyaltyAbility(ab)
			case gainsValidAbilitiesAdmitsLoyaltyX:
				ok = ok && e.isLoyaltyAbility(ab)
			default:
				// Unmodelled qualifier: fail closed for this alternative.
				ok = false
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// gainedActivationsThisTurn counts how many times the ACTIVATED ability at
// foreign face-local index idx of the foreign card `foreign` has been
// activated FROM the affected object id this turn. GainedAbilityPush events
// name all three (Obj = recipient, IDs[0] = foreign card, Amount = index), so
// the replayable log is the memory -- the loyaltyActivationsThisTurn fold
// pattern. TurnChange resets the count, and so does the RECIPIENT's zone
// change (CR 400.7: a Mairsil that leaves and returns is a new object, and
// "Mairsil can activate each of those abilities only once each turn" is
// about that object). The caged card's own zone never matters here.
func (e *Engine) gainedActivationsThisTurn(id, foreign state.ObjID, idx int) int {
	used := 0
	// CR 400.7: a recipient that changed zones is a new object with its own
	// per-turn allowance, so only its current stint's activations count.
	for _, ev := range e.L.Events[e.objectStintStart(id):] {
		switch ev.Kind {
		case events.TurnChange:
			if int(ev.Player) < len(e.G.Players) {
				used = 0
			}
		case events.GainedAbilityPush, events.ManaActivate:
			// A gained MANA ability never goes on the stack, so its
			// activation is the ManaActivate marker carrying the same
			// (recipient, foreign card, index) triple (gainedManaRef); a
			// printed mana marker carries no IDs and never matches.
			if ev.Obj == id && len(ev.IDs) > 0 && ev.IDs[0] == foreign && int(ev.Amount) == idx {
				used++
			}
		}
	}
	return used
}

var grantedKeywordLinesFullKeys = state.NewNameSet("Cycling", "TypeCycling", "Saddle", "Crew")

type gainsValidAbilitiesAdmitsCode uint16

const (
	gainsValidAbilitiesAdmitsEmpty gainsValidAbilitiesAdmitsCode = iota + 1
	gainsValidAbilitiesAdmitsManaAbility
	gainsValidAbilitiesAdmitsLoyalty
	gainsValidAbilitiesAdmitsLoyaltyX
)

var gainsValidAbilitiesAdmitsCodes = state.NewStrCodes(
	state.StrEntry[gainsValidAbilitiesAdmitsCode]{Key: "", Val: gainsValidAbilitiesAdmitsEmpty},
	state.StrEntry[gainsValidAbilitiesAdmitsCode]{Key: "!ManaAbility", Val: gainsValidAbilitiesAdmitsManaAbility},
	state.StrEntry[gainsValidAbilitiesAdmitsCode]{Key: "!Loyalty", Val: gainsValidAbilitiesAdmitsLoyalty},
	state.StrEntry[gainsValidAbilitiesAdmitsCode]{Key: "Loyalty", Val: gainsValidAbilitiesAdmitsLoyaltyX},
)
