package rules

import (
	"math"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// potentialUnbounded is the per-unit amount one indeterminate mana source
// contributes to the hypothetical potential pool. A source whose Amount$ is
// an X, a Y or a Count$ expression (an Urza land, Gaea's Cradle, Priest of
// Titania) could produce any amount this turn; the stop decision the
// projection serves must never lose an action to a source the engine cannot
// statically price, so such a source is priced unbounded rather than at zero.
// It is math.MaxInt32, the largest representable mana cost. Potential-pool
// additions saturate at that sentinel and Mana.Total does too, so several
// open sources cannot wrap the hypothetical pool below a payable cost.
const potentialUnbounded int32 = math.MaxInt32

// PotentialMana is the hypothetical pool the potential-action walk prices
// against: the seat's floating pool PLUS what every untapped mana source it
// controls could produce. It is deliberately an OVER-bound, the direction the
// auto-pass doctrine names safe (a wrongly withheld pass costs one idle stop;
// a wrongly eaten window loses the player's action):
//
//   - a source with a single fixed-colour production (a Plains) adds its
//     literal Amount (default 1);
//   - a source with several fixed productions (a Volcanic Island's two
//     abilities, a "Produced$ GW" line) adds every face -- the seat can only
//     tap for one at a time, but the bound must not lose a colour;
//   - a source with an alternative or variable production (Produced$ Any /
//     Combo Any / Chosen / an unknown token) or an indeterminate Amount$ (an
//     Urza land, Gaea's Cradle, Elvish Archdruid) contributes
//     potentialUnbounded to EVERY unit -- the one shape a fixed vector cannot
//     represent honestly, and the shape the old client-side bound priced at
//     zero (the Tron + Karn defect);
//   - a mana ability WITH a paid activation cost (Wasteland's
//     "T, Sac<1/CARDNAME>", a "{1}, {T}: add {C}{C}" source) still counts, and
//     it counts even though the real floating pool does not already cover its
//     cost: the seat can tap a free source first, spend that mana on the paid
//     activation, and then tap the paid source. PotentialMana therefore runs a
//     FIXPOINT -- a source's paid mana ability is admitted the moment the
//     accumulated pool can cover its activation cost, and its own production
//     then feeds the next source -- so the pool grows the way a player would
//     actually sequence the activations (the Jitte-round finding: a Plains
//     plus an untapped "{1}, {T}: add {C}{C}" source must cover a {2} spell,
//     because the seat taps the Plains for {W}, pays the {1} to activate the
//     source, and casts from the {C}{C} it produced).
//
// Restriction-gated abilities (CantBeActivated) contribute nothing, and a
// source's own production never pays for that source's own paid activation
// (a source is admitted only when the pool built from OTHER sources already
// covers its cost, and each source contributes at most once -- the same
// single-tap-per-source semantics the engine's own tap-for-mana offer uses).
// The aggregate is a pure read: no event is emitted and no state field is
// written; the pool lives only in this return value.
//
// Determinism: the walk is over zone order, never a map range (the added set
// is only indexed, never ranged), so the aggregate is byte-stable run to run.
func (e *Engine) PotentialMana(p state.PlayerID) state.Mana {
	out := e.G.Players[p].Pool
	added := map[state.ObjID]bool{}
	for {
		progressed := false
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			if added[id] {
				continue
			}
			o := e.G.Obj(id)
			if o == nil || o.Face() == nil {
				continue
			}
			// Admit the source the first pass the accumulated pool covers at
			// least one of its mana abilities' activation costs. Adding every
			// currently-feasible face is the same single-source over-bound the
			// real offer walk's multi-ability source produces.
			var ses []*cards.SA
			for _, ma := range o.Face().ManaAbilities() {
				if !e.abilityRestricted(p, id, ma) && e.manaAbilityPayablePool(p, id, ma, &out) {
					ses = append(ses, ma)
				}
			}
			if len(ses) == 0 {
				continue
			}
			added[id] = true
			progressed = true
			for _, ma := range ses {
				addPotentialMana(&out, ma)
			}
		}
		if !progressed {
			break
		}
	}
	return out
}

// addPotentialMana folds one mana ability into the potential accumulator. The
// amount and production parsing mirrors AvailableMana's addAvailable (the
// executor's own effMana resolutions: blank Produced$ is one colourless) with
// one divergence: anything the executor cannot statically price -- an
// alternative production or an indeterminate amount -- is UNBOUNDED here
// rather than zero, because this bound must never lose an action.
func addPotentialMana(m *state.Mana, ma *cards.SA) {
	amt, indeterminate := potentialAmount(ma)
	raw := strings.TrimSpace(ma.Params["Produced"])
	// Blank Produced$ is the executor's own colourless default (effMana), NOT
	// an alternative production: normalize it to "C" BEFORE the open test so a
	// colourless source contributes one colourless rather than pricing the
	// whole pool unbounded (the blank-`producedOpen("")` bug: 99 of every
	// colour turned an unpayable {W} spell into a potential cast).
	if raw == "" {
		raw = "C"
	}
	if indeterminate || producedOpen(raw) {
		for i := range m {
			m[i] = saturatingPotentialMana(m[i], potentialUnbounded)
		}
		return
	}
	s := strings.NewReplacer("{", "", "}", "", " ", "").Replace(raw)
	for _, r := range s {
		i := state.ManaIndex(byte(r))
		m[i] = saturatingPotentialMana(m[i], amt)
	}
}

// saturatingPotentialMana adds a known non-negative production without
// letting the deliberately-unbounded potential sentinel wrap int32. This is
// projection-only; real mana still enters state through events.ManaAdd.
func saturatingPotentialMana(have, add int32) int32 {
	sum := int64(have) + int64(add)
	if sum >= math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(sum)
}

// producedOpen reports whether a Produced$ value names an ALTERNATIVE or
// unknown production the executor resolves at activation time rather than a
// fixed mana set: "Any"/"Combo Any" (any colour), "Chosen", or any token
// outside the WUBRGC faces. A fixed multi-face value ("GW") is not open --
// each face is folded additively as an over-bound. The blank case is kept
// only defensively: addPotentialMana normalizes a blank Produced$ to "C"
// before calling this, so a colourless source is never mistaken for an open
// production (the round-2 finding that granted 99 of every colour to a
// blank-Produced$ source).
func producedOpen(raw string) bool {
	switch raw {
	case "", "Any", "Combo Any", "Chosen":
		return true
	}
	s := strings.NewReplacer("{", "", "}", "", " ", "").Replace(raw)
	for _, r := range s {
		switch r {
		case 'W', 'U', 'B', 'R', 'G', 'C':
		default:
			return true
		}
	}
	return false
}

// potentialAmount is an ability's Amount$ as (literal, indeterminate): a
// blank Amount is the executor's own default of 1; a literal integer is used
// directly with the negative clamp the executor applies; anything else (an X,
// a Y, a Count$ expression, a Sacrificed$ reference) is indeterminate and
// prices unbounded upstream rather than at zero.
func potentialAmount(ma *cards.SA) (int32, bool) {
	raw := strings.TrimSpace(ma.Params["Amount"])
	if raw == "" {
		return 1, false
	}
	if v, err := strconv.Atoi(raw); err == nil {
		if v < 0 {
			return 0, false
		}
		return int32(v), false
	}
	return 0, true
}

// PotentialActions is the view's per-seat projection of every play the seat
// could still make after floating every mana its untapped sources could
// produce: the engine's own legal-offer walk (legalActionsPriced, the exact
// code that builds a priority decision's options) priced against
// PotentialMana. It carries every real play the walk can offer
// (potentialPlayKind) and never the mana tap, pass or concede, which every
// priority window offers and which are never a play. The walk's own gates
// (timing, restrictions, targets, non-mana costs, live RaiseCost/ReduceCost,
// X at 0) are the engine's, so the projection cannot disagree with the engine
// the way a client-side re-derivation does.
//
// This is a pure read and never touches an event; callers project it ONLY for
// the viewer's own seat (view/view.go gates it on p.ID == viewer), because
// the walk reads that seat's hand, command zone and graveyard -- projecting
// another seat's would leak their hidden zones (CR 400.2).
//
// Determinism: the walk is zone-order, never a map range, so the list is
// byte-stable run to run.
func (e *Engine) PotentialActions(p state.PlayerID) []decision.PotentialAction {
	if e.G.Over {
		return nil
	}
	pool := e.PotentialMana(p)
	var out []decision.PotentialAction
	for _, o := range e.legalActionsPriced(p, &pool) {
		if potentialPlayKind(o.Kind) {
			out = append(out, decision.PotentialAction{
				Kind: o.Kind, Obj: o.Obj, Ability: o.Ability, Mode: o.Mode, Label: o.Label,
			})
		}
	}
	return out
}

// potentialPlayKind is the projection's kind filter: every real play kind
// legalActionsPriced emits (measured from rules/legal.go and pinned by
// TestPotentialActionsProjectsEveryPlayKind, so a new kind in the walk fails
// until it is classified here or as never-a-play):
//
//   - "cast": hand, command zone, graveyard (flashback, escape, ...) and exile
//     casts, plus the special actions that ride the cast kind (foretell,
//     suspend), each with its Mode;
//   - "ability": printed and keyword-granted activated abilities (Equip among
//     them), and "granted": a max-speed static's AddAbility$ (rules/speed.go);
//   - "unlock" (a Room's locked door, CR 309.5), "turn_face_up" (the morph
//     family, CR 708.6) and "specialize" (Mode = the face index): the
//     mana-costed special actions, projected on an empty pool once the
//     hypothetical pool pays them (aph-web-manual-only-plays: before, they
//     were neither offered nor projected there, so the web's auto-pay mode
//     hid the manual taps they need);
//   - "play_land" and "station": never mana-costed, so never float-gated,
//     projected for completeness.
//
// The excluded kinds are "activate" (the mana tap), "pass" and "concede".
func potentialPlayKind(kind string) bool {
	switch kind {
	case "cast", "ability", "play_land", "granted", "unlock", "turn_face_up", "specialize", "station":
		return true
	}
	return false
}
