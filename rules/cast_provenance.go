// The rules-side split for the cast-provenance filter predicates (tasks
// castprov1 and castprov2). The predFn signature the effects filter runs on
// carries no Host, and a hand-origin cast deliberately carries no CastFlags
// bit (the flags mark alternative costs and origins only), so the provenance
// lives in the event log — Engine reads it through reverse PutOnStack scans,
// the same machinery the Count$ branch heads read. The tokens are therefore
// split OUT of the spec text at the rules-side match sites (where the
// Engine, and its log, is in scope) and evaluated there, with the remainder
// matched by the ordinary filter. The precedent is the spec-rewrite helpers
// the engine already keeps (trigmatch.SpellCastPermanentSpec, permanentCardSpec).
//
// The two families, and their exact semantics:
//
//   - wasCastFromYourHandByYou (castprov1): the object's LATEST PutOnStack
//     event names the cast, and it was from hand, by you.
//   - wasCastByYou (castprov2, the "When CARDNAME enters, if you cast it"
//     ETB family — Zacama, Marina Vendrell's Grimoire, Nine-Lives
//     Familiar's etbCounter gate): SOME PutOnStack event for this object
//     names you as caster — the oracle's "if you cast it" does not care
//     where from. A copy was never cast (the same IsCopy guard both reads
//     take). A card that left the battlefield after that cast and came
//     back without one (reanimated, blinked, Nine-Lives Familiar's own
//     return) is a new object that was never cast (CR 400.7) and reads
//     false. The hand scan (castprov1) still has the approximation: it reads
//     only the log's casts and cannot tell such a re-entry apart.
//
//   - wasCastFromYourHand (castprov3): the object's LATEST PutOnStack event
//     names the cast AND that cast came from a hand — ANY caster. The bare
//     spelling is the one the "from anywhere other than your hand" carriers
//     print (Vega the Watcher, Bilbo Thief in the Night, Mm'menon's
//     RestrictValid$); every carrier that needs player scoping supplies it
//     elsewhere (ValidActivatingPlayer$ You on the trigger lines, YouCtrl or
//     wasCastByYou in the same Affected$/Count spec), measured over the 46
//     raw carrier files. A copy was never cast (the same IsCopy guard both
//     existing families take); a card never put on the stack reads false.
//
//   - the origin-zone family (task wascastfrom): wasCastFromExile,
//     wasCastFromYourGraveyard, wasCastFromYourGraveyardByYou and
//     wasCastFromTheirHand — the object's LATEST PutOnStack cast came from
//     the named zone (and, for the YourGraveyard spellings, was made by the
//     evaluating you). The bare wasCastFromGraveyard spelling is NOT here:
//     it is the effects-side CastFlags predicate (FlagFlashback | Harmonize
//     | Escaped, effects/filter.go), which the rules-side strip must leave
//     in place for the effects-only call sites (the ConditionPresent gates,
//     the target ValidTgts$ matching) — and the log read and the flag read
//     agree on every shape the flag covers, so a rules-side site that strips
//     the other four never disagrees with an effects-side site that keeps it.
//     The flag set itself is state.WasCastFromGraveyard (flashback, harmonize,
//     jump-start, escaped), shared with the three effects call sites so the
//     two reads cannot drift.
//     The same latest-cast-wins and copy-was-never-cast guards the hand
//     families take apply here.
//
// castProvenanceAdmits is the combined entry point every match site calls:
// it evaluates all the families in one pass, so a future carrier mixing the
// tokens in one spec is covered by construction (measured: alex_wilder
// and quandrix_the_proof carry wasCastByYou AND the bare token in one
// alternative).

package rules

import (
	"fmt"
	"strings"
	"sync/atomic"
	"unsafe"

	"github.com/adams-shaun/gorge/events"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// admitProvenanceAlternatives splits spec into its comma alternatives (the
// same split the filter draws), strips the provenance predicate — both the
// positive and the !-negated spelling — from every alternative, drops the
// alternatives whose requirement is not met by holds (positive requires
// hold, negated requires !hold), and rejoins the survivors. ok is false when
// no alternative survives: the spec matches nothing. A spec without the
// predicate is returned unchanged with ok true, so every unrelated spec is
// byte-identical.
func admitProvenanceAlternatives(spec, pred string, holds bool) (string, bool) {
	if !strings.Contains(spec, pred) {
		return spec, true
	}
	neg := "!" + pred
	var b strings.Builder
	first := true
	alive := false
	for alt := range effects.FilterAlternatives(spec) {
		s1, hadPos := effects.StripPredicateToken(alt, pred)
		s2, hadNeg := effects.StripPredicateToken(s1, neg)
		if (hadPos && !holds) || (hadNeg && holds) {
			continue
		}
		if !first {
			b.WriteByte(',')
		}
		b.WriteString(s2)
		first = false
		alive = true
	}
	if !alive {
		return "", false
	}
	return b.String(), true
}

// castFromHandAdmitsWindow is castFromHandAdmits with the pre-push OFFER
// window fallback: when pendingCast is set (the layer walk is evaluating an
// AffectedZone$ Stack grant against the object being cast, before CR
// 601.2a's push), the log carries no PutOnStack yet, so "cast from your hand
// by you" is inferred from the offer-time object: its controller is the
// caster and its zone is the hand. The log read still wins whenever it has
// an answer (a real on-stack spell), so no pushed cast changes behaviour.
func (e *Engine) castFromHandAdmitsWindow(spec string, objID state.ObjID, you state.PlayerID, pendingCast bool) (string, bool) {
	if !strings.Contains(spec, "wasCastFromYourHandByYou") {
		return spec, true
	}
	holds := false
	if o := e.G.Obj(objID); o != nil && !o.IsCopy {
		holds = e.WasCastFromHandByYou(objID, you)
		if !holds && pendingCast {
			holds = o.Controller == you && o.Zone == state.ZHand
		}
	}
	return admitProvenanceAlternatives(spec, "wasCastFromYourHandByYou", holds)
}

// castAtAllAdmitsWindow is castAtAllAdmits with the pre-push OFFER window
// fallback (see castFromHandAdmitsWindow): an AffectedZone$ Stack grant whose
// Affected$ carries wasCastByYou (Zinnia's "Creature spells you cast have
// offspring {2}", Witherbloom's affinity, Prismari's storm, Quandrix's
// cascade, Silverquill's casualty, Mycosynth Golem's artifact affinity and
// the rest) evaluates against the object BEING CAST, which is still in hand
// at offer time and so has no PutOnStack in the log. "Cast by you" then
// holds iff the offer-time object's controller (the caster) is you. The log
// read wins whenever it has an answer, so a real on-stack spell is
// unchanged.
func (e *Engine) castAtAllAdmitsWindow(spec string, objID state.ObjID, you state.PlayerID, pendingCast bool) (string, bool) {
	if !strings.Contains(spec, "wasCastByYou") {
		return spec, true
	}
	holds := false
	if o := e.G.Obj(objID); o != nil && !o.IsCopy {
		holds = e.WasCastByYou(objID, you)
		if !holds && pendingCast {
			holds = o.Controller == you
		}
	}
	return admitProvenanceAlternatives(spec, "wasCastByYou", holds)
}

// specHasBareWasCast reports whether spec carries the bare wasCast
// predicate — positive or !-negated — as an EXACT token ("wasCastByYou" and
// the wasCastFrom* family carry it only as a substring). A boundary scan,
// not the FilterAlternatives iterator: matchesWithTypes calls this on the
// hot path and the derived-allocation budget is zero (TestDerivedWith
// ContinuousEffectsDoesNotAllocate).
func specHasBareWasCast(spec string) bool {
	for i := 0; ; {
		j := strings.Index(spec[i:], "wasCast")
		if j < 0 {
			return false
		}
		i += j
		end := i + len("wasCast")
		if end < len(spec) {
			switch c := spec[end]; {
			case c == 'B' || c == 'F': // wasCastByYou, wasCastFrom…
				i = end
				continue
			case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '$' || c == '_':
				i = end
				continue
			}
		}
		return true // a token boundary (end, '+', '.', ','…)
	}
}

// castAtAllBareAdmitsWindow is castAtAllBareAdmits with the pre-push OFFER
// window fallback (see castFromHandAdmitsWindow): an AffectedZone$ Stack
// grant whose Affected$ carries the bare token (Wort, the Raidmother's
// conspire grant) evaluates against the object BEING CAST, which is still
// in hand at offer time and so has no PutOnStack in the log. "Was cast"
// then holds because the offer-time object IS the spell being announced;
// the log read wins whenever it has an answer, so a real on-stack spell is
// unchanged.
func (e *Engine) castAtAllBareAdmitsWindow(spec string, objID state.ObjID, pendingCast bool) (string, bool) {
	if !specHasBareWasCast(spec) {
		return spec, true
	}
	holds := e.WasCast(objID)
	if !holds && pendingCast {
		holds = true
	}
	return admitProvenanceAlternatives(spec, "wasCast", holds)
}

// castFromHandAnyAdmitsWindow is castFromHandAnyAdmits with the pre-push
// OFFER window fallback (see castFromHandAdmitsWindow): a bare
// wasCastFromYourHand grant evaluated against the object being cast infers
// "came from a hand" from the offer-time object's zone. The log read wins
// whenever it has an answer.
func (e *Engine) castFromHandAnyAdmitsWindow(spec string, objID state.ObjID, pendingCast bool) (string, bool) {
	if strings.Contains(spec, "wasCastFromYourHandByYou") || !strings.Contains(spec, "wasCastFromYourHand") {
		return spec, true
	}
	holds := false
	if o := e.G.Obj(objID); o != nil && !o.IsCopy {
		holds = e.WasCastFromHand(objID)
		if !holds && pendingCast {
			holds = o.Zone == state.ZHand
		}
	}
	return admitProvenanceAlternatives(spec, "wasCastFromYourHand", holds)
}

// castProvenanceAdmits evaluates all four cast-provenance families of a
// Forge filter spec against objID — the hand-origin ByYou one
// (castFromHandAdmits), the any-origin one (castAtAllAdmits), the bare
// player-less hand one (castFromHandAnyAdmits) and the origin-zone one
// (castOriginAdmits, task wascastfrom) — chaining the splits so a
// spec carrying any (or several) of the tokens evaluates fully. The chain
// order is load-bearing: ByYou before bare (see castFromHandAnyAdmits's
// order invariant). This is the one entry point every rules-side match site
// calls, so a future carrier mixing the tokens is covered by construction.
func (e *Engine) castProvenanceAdmits(spec string, objID state.ObjID, you state.PlayerID) (string, bool) {
	return e.castProvenanceAdmitsWindow(spec, objID, you, false)
}

// castProvenanceAdmitsWindow is castProvenanceAdmits with the pre-push OFFER
// window fallback threaded through all three hand families (see
// castFromHandAdmitsWindow): the layer walk's AffectedZone$ Stack read sets
// pendingCast, every other match site passes false and keeps the log-only
// semantics. The origin-zone family (castOriginAdmits) is appended after
// them log-only — its own pre-push shape is castOriginAdmitsAtZone, taken
// by castProvenanceAdmitsPending. The chain order is unchanged: ByYou before bare.
func (e *Engine) castProvenanceAdmitsWindow(spec string, objID state.ObjID, you state.PlayerID, pendingCast bool) (string, bool) {
	gate := specProvenanceGate(spec)
	if provenanceGateVerify {
		if live := computeProvenanceGate(spec); live != gate {
			panic(fmt.Sprintf("rules: cached provenance gate %+v differs from the live one %+v for spec %q", gate, live, spec))
		}
		s, ok := e.castProvenanceAdmitsMasked(spec, gate, objID, you, pendingCast)
		ws, wok := e.castProvenanceAdmitsChain(spec, objID, you, pendingCast)
		if s != ws || ok != wok {
			panic(fmt.Sprintf("rules: masked provenance chain (%q, %v) differs from the full chain (%q, %v) for spec %q", s, ok, ws, wok, spec))
		}
		return s, ok
	}
	return e.castProvenanceAdmitsMasked(spec, gate, objID, you, pendingCast)
}

// castProvenanceAdmitsChain is the unmasked chain: every stage runs its own
// live guard. Verify mode compares the masked chain against it.
func (e *Engine) castProvenanceAdmitsChain(spec string, objID state.ObjID, you state.PlayerID, pendingCast bool) (string, bool) {
	s, ok := e.castFromHandAdmitsWindow(spec, objID, you, pendingCast)
	if !ok {
		return "", false
	}
	if s, ok = e.castAtAllAdmitsWindow(s, objID, you, pendingCast); !ok {
		return "", false
	}
	if s, ok = e.castAtAllBareAdmitsWindow(s, objID, pendingCast); !ok {
		return "", false
	}
	if s, ok = e.castFromHandAnyAdmitsWindow(s, objID, pendingCast); !ok {
		return "", false
	}
	if s, ok = e.castOriginAdmits(s, objID, you); !ok {
		return "", false
	}
	if s, ok = e.webSlingedAdmits(s, objID); !ok {
		return "", false
	}
	return e.castSaAdmits(s, objID)
}

// castProvenanceAdmitsMasked is the chain with each stage gated by the
// spec's cached guard bits (specProvenanceGate).
func (e *Engine) castProvenanceAdmitsMasked(spec string, gate provGate, objID state.ObjID, you state.PlayerID, pendingCast bool) (string, bool) {
	if !gate.may {
		return spec, true
	}
	// Each stage below is skipped when the spec's cached guard bit says its
	// own guard would return the input unchanged -- exact while no earlier
	// stage has rewritten the string (changed stays false). Once a stage has
	// fired, every later stage runs its live guard on the rewritten string,
	// exactly the pre-mask chain. Measured: a commander game's
	// "Card.Colorless+YouCtrl+YouOwn+wasCastFromHand+cmcGE7" carries "Cast"
	// (the effects-side wasCastFromHand) but no rules-side token, and paid
	// ~12 substring scans per layer-walk match before this mask.
	s, ok := spec, true
	changed := false
	if changed || gate.stages&provHandByYou != 0 {
		if s, ok = e.castFromHandAdmitsWindow(s, objID, you, pendingCast); !ok {
			return "", false
		}
		changed = true
	}
	if changed || gate.stages&provAtAllByYou != 0 {
		if s, ok = e.castAtAllAdmitsWindow(s, objID, you, pendingCast); !ok {
			return "", false
		}
		changed = true
	}
	if changed || gate.stages&provBare != 0 {
		if s, ok = e.castAtAllBareAdmitsWindow(s, objID, pendingCast); !ok {
			return "", false
		}
		changed = true
	}
	if changed || gate.stages&provHandAny != 0 {
		if s, ok = e.castFromHandAnyAdmitsWindow(s, objID, pendingCast); !ok {
			return "", false
		}
		changed = true
	}
	if gate.origin {
		s, ok = e.castOriginAdmits(s, objID, you)
		if !ok {
			return "", false
		}
		changed = true
	}
	if changed || gate.stages&provWebSlinged != 0 {
		if s, ok = e.webSlingedAdmits(s, objID); !ok {
			return "", false
		}
		changed = true
	}
	if changed || gate.stages&provCastSa != 0 {
		return e.castSaAdmits(s, objID)
	}
	return s, true
}

// The CastSa family (task castsa-provenance): Forge's "Card.CastSa Spell.<X>"
// card-level provenance predicate — the card's cast spell ability had
// property <X>. The corpus's X vocabulary splits by what the engine can
// answer:
//
//   - the mana-spend properties the payment path already encodes: a
//     Treasure/Cave/Desert/Artifact unit's spend is a tagged ManaAdd event
//     (state.TypedManaCounter, the castfilter2 encoding — no new event
//     needed; task mayplay-mfa added Artifact), and the cast's total spend
//     is the plain negative ManaAdd delta (trigmatch.ManaSpentForCast's read, Roiling
//     Vortex's convention). These five spellings are implemented here.
//     Spell.ManaFromArtifact (Shadow the Hedgehog's Chaos Control -- "mana
//     from an artifact was spent to cast it") is the artifact tag: a
//     producer that prints none of Treasure/Cave/Desert tags Artifact, so
//     the spend window's tagged ledger answers it exactly like its
//     siblings.
//
//   - the cast-mode/permission flags the pay-time CastInfo carries:
//     Spell.Mayhem (state.FlagMayhem), Spell.MayPlaySource
//     (state.FlagMayPlay) and Spell.Warp (state.FlagWarped) read the cast's
//     own CastFlags rather than a spend window. The independent flags let
//     sibling predicates share this event transport without changing the
//     event format. Spell.Warp is the warp alternative cast's provenance
//     (task mayplay-warp): modeFlags' "warped" case stamps it, and its
//     corpus carrier is Full Bore's `ConditionPresent$ Card.CastSa
//     Spell.Warp`.
//
// Like every provenance family the token is split OUT of the spec text at
// the rules-side match sites and the remainder matched by the ordinary
// filter; admitProvenanceAlternatives is the shared strip.
type castSaToken struct {
	token string
	// tag is the state.TypedMana index the property reads; -1 is the plain
	// total-spend read (ManaSpent EQ0 — "no mana was spent to cast it").
	// flag tokens ignore it.
	tag int
	// flag, when nonzero, marks a CAST-FLAG property: holds is read off the
	// object's latest cast's CastFlags (state.FlagMayhem), never off a spend
	// window. A stack copy never carries the bit (state.CastProvenanceFlags
	// strips it), so a copy reads false — the never-cast convention.
	flag uint64
}

var castSaTokens = []castSaToken{
	{token: "CastSa Spell.ManaFromTreasure", tag: state.TypedTreasure},
	{token: "CastSa Spell.ManaFromCave", tag: state.TypedCave},
	{token: "CastSa Spell.ManaFromDesert", tag: state.TypedDesert},
	{token: "CastSa Spell.ManaFromArtifact", tag: state.TypedArtifact},
	{token: "CastSa Spell.ManaSpent EQ0", tag: -1},
	{token: "CastSa Spell.Mayhem", tag: -1, flag: state.FlagMayhem},
	{token: "CastSa Spell.MayPlaySource", tag: -1, flag: state.FlagMayPlay},
	{token: "CastSa Spell.Warp", tag: -1, flag: state.FlagWarped},
}

// castSpendFacts is one cast's spend window: the total and per-tag mana the
// cast's player spent between that cast's PutOnStack and the read point.
// ok is false when the object has no PutOnStack cast in the log before the
// turn/seat boundary — a never-cast (cheated into play) or a cast older than
// this turn's provenance question.
type castSpendFacts struct {
	spent  int32
	tagged [4]int32
	ok     bool
}

// castSaTokensIn reports the CastSa tokens a spec carries, in table order.
func castSaTokensIn(spec string) []castSaToken {
	if !strings.Contains(spec, "CastSa") {
		return nil
	}
	var out []castSaToken
	for _, tok := range castSaTokens {
		if strings.Contains(spec, tok.token) {
			out = append(out, tok)
		}
	}
	return out
}

// castSaTokenHolds evaluates one CastSa property against a cast's spend
// window and cast flags. A flag token reads the flags; a spend token reads
// the window.
func castSaTokenHolds(t castSaToken, f castSpendFacts, flags uint64) bool {
	if t.flag != 0 {
		return flags&t.flag != 0
	}
	if !f.ok {
		return false
	}
	if t.tag < 0 {
		return f.spent == 0
	}
	return f.tagged[t.tag] > 0
}

// castSpendWindow reads the spend window of obj's LATEST PutOnStack cast:
// walking the log backward from its end, every negative ManaAdd by the
// cast's player accumulates into that cast's facts (total and, through the
// "TreasureC"-form Counter, the typed tags) until the object's own push is
// found. The same window trigmatch.ManaSpentForCast reads for the SpellCast-trigger
// ValidSA$ family, so the card-level CastSa spellings cannot disagree with
// the SA-level ones. A spell sitting on the stack mid-cast is exact (the
// payment ManaAdds sit directly above the push); the shared approximation
// with trigmatch.ManaSpentForCast applies: the window also counts the caster's own
// post-payment floating until the read point. Derived from the event log,
// so a replay derives the same answer.
func (e *Engine) castSpendWindow(obj state.ObjID) castSpendFacts {
	var acc [8]castSpendFacts
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		switch ev.Kind {
		case events.TurnChange, events.PlayerLost:
			return castSpendFacts{}
		case events.PutOnStack:
			if ev.Obj == obj && int(ev.Player) < len(acc) {
				f := acc[ev.Player]
				f.ok = true
				return f
			}
		case events.ManaAdd:
			if ev.Amount < 0 && int(ev.Player) < len(acc) {
				acc[ev.Player].spent += -ev.Amount
				if tag, _, ok := state.TypedManaCounter(ev.Counter); ok {
					base, artifact := state.ManaUnitTypes(tag)
					acc[ev.Player].tagged[base] += -ev.Amount
					if artifact && base != state.TypedArtifact {
						acc[ev.Player].tagged[state.TypedArtifact] += -ev.Amount
					}
				}
			}
		}
	}
	return castSpendFacts{}
}

// castSaAdmits evaluates the CastSa family against objID: every alternative
// carrying a token whose requirement the object's latest cast's spend window
// does not meet is dropped and the survivors rejoined (the shared
// admitProvenanceAlternatives split). ok is false when no alternative
// survives: the spec matches nothing. A spec without any CastSa token is
// returned unchanged, so every unrelated evaluation is byte-identical.
//
// There is no pre-push OFFER-window fallback here (unlike the hand
// families): the mana spend the properties read does not exist until the
// payment has run — and the cast flags a flag token reads ride the same
// pay-time CastInfo, after the push — so an AffectedZone$ Stack grant
// evaluated against the
// object being cast fails until CR 601.2f-h's payment is in the log — and
// the only first-cast gates that need the grant evaluate it AFTER payment
// (queueCascadeTriggers), where the window is exact.
func (e *Engine) castSaAdmits(spec string, objID state.ObjID) (string, bool) {
	if !strings.Contains(spec, "CastSa") {
		return spec, true
	}
	var facts castSpendFacts
	factsRead := false
	var flags uint64
	flagsRead := false
	for _, tok := range castSaTokens {
		if !strings.Contains(spec, tok.token) {
			continue
		}
		var holds bool
		if tok.flag != 0 {
			// A cast-flag token reads the object's LATEST cast's CastFlags
			// (each pay-time CastInfo REPLACES the set, events.Apply's
			// CastInfo case), so a re-cast object's older cast cannot
			// inherit the newer cast's flags — the same latest-cast read the
			// spend window takes. A copy was never cast and carries no
			// provenance bit (state.CastProvenanceFlags strips it), so it
			// reads false without a separate guard.
			if !flagsRead {
				if o := e.G.Obj(objID); o != nil {
					flags = o.CastFlags
				}
				flagsRead = true
			}
			holds = flags&tok.flag != 0
		} else {
			if !factsRead {
				facts = e.castSpendWindow(objID)
				factsRead = true
			}
			holds = castSaTokenHolds(tok, facts, 0)
		}
		var ok bool
		if spec, ok = admitProvenanceAlternatives(spec, tok.token, holds); !ok {
			return "", false
		}
	}
	return spec, true
}

// castOriginTokens is the origin-zone cast-provenance family (task
// wascastfrom): each token reads the object's LATEST PutOnStack cast (the
// zone the cast came from, optionally scoped to the evaluating you). ByYou
// MUST be listed before the plain YourGraveyard spelling — it is a
// superstring of it and the strip is exact-token, but the polarity must be
// accounted under the ByYou read, the castFromHandAnyAdmits order
// invariant.
type castOriginToken struct {
	token string
	zone  state.Zone
	byYou bool
}

var castOriginTokens = []castOriginToken{
	// "you cast a spell from your graveyard" — the caster casts from their
	// own graveyard; both Forge spellings collapse onto the same read (the
	// ByYou suffix is the player scope the plain spelling takes implicitly:
	// in this build a cast's From is always a zone its caster owns).
	{token: "wasCastFromYourGraveyardByYou", zone: state.ZGraveyard, byYou: true},
	{token: "wasCastFromYourGraveyard", zone: state.ZGraveyard, byYou: true},
	// "whenever you cast a spell from exile" — any caster (the bare family's
	// caster-agnostic reading; every player-scoped carrier supplies
	// ValidActivatingPlayer$ or a You qualifier alongside).
	{token: "wasCastFromExile", zone: state.ZExile},
	// "whenever a player casts a spell from their hand" — "their" is the
	// CASTER's own hand, so the predicate is caster-agnostic too: a cast from
	// a hand IS from the caster's hand in this build (a player never casts
	// from another seat's hand). Aether Revolt's Knowledge Pool, Wash Away's
	// "wasn't cast from its owner's hand" (owner == caster for a spell this
	// build can put on the stack) and Aerial Extortionist's negated form all
	// read it.
	{token: "wasCastFromTheirHand", zone: state.ZHand},
}

// specCarriesCastOrigin reports whether spec carries ANY origin-zone token
// the castOriginAdmits chain strips — the pending guard's cheap gate.
func specCarriesCastOrigin(spec string) bool {
	for _, tok := range castOriginTokens {
		if strings.Contains(spec, tok.token) {
			return true
		}
	}
	return false
}

// latestCastOrigin reports obj's LATEST PutOnStack cast: the zone it came
// from and the player who cast it. ok=false for a card never put on the
// stack (cheated into play) — the same read every existing provenance
// family takes. Derived from the event log like WasCastFromHandByYou, so a
// replay derives the same answer.
func (e *Engine) latestCastOrigin(obj state.ObjID) (state.Zone, state.PlayerID, bool) {
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.PutOnStack && ev.Obj == obj {
			return ev.From, ev.Player, true
		}
	}
	return 0, 0, false
}

// castOriginAdmits evaluates the origin-zone cast-provenance family
// (wasCastFromExile, wasCastFromYourGraveyard, wasCastFromYourGraveyardByYou,
// wasCastFromTheirHand) of a Forge filter spec against objID: every
// alternative carrying a token whose requirement is not met is dropped and
// the survivors rejoined, the admitProvenanceAlternatives split the hand
// families use. ok is false when no alternative survives. A spec without any
// of the tokens is returned unchanged, so every unrelated evaluation is
// byte-identical. The bare wasCastFromGraveyard spelling is deliberately NOT
// evaluated here (it stays the effects-side CastFlags predicate).
func (e *Engine) castOriginAdmits(spec string, objID state.ObjID, you state.PlayerID) (string, bool) {
	for _, tok := range castOriginTokens {
		if !strings.Contains(spec, tok.token) {
			continue
		}
		holds := false
		if o := e.G.Obj(objID); o != nil && !o.IsCopy {
			if from, by, ok := e.latestCastOrigin(objID); ok {
				holds = from == tok.zone && (!tok.byYou || by == you)
			}
		}
		var ok bool
		if spec, ok = admitProvenanceAlternatives(spec, tok.token, holds); !ok {
			return "", false
		}
	}
	return spec, true
}

// castOriginAdmitsAtZone evaluates the origin-zone family against the cast
// IN PROGRESS rather than the log: the cast a CantBeCast restriction gates
// has no PutOnStack yet, and the origin it will carry is the zone the
// restricted object is being cast FROM — its CURRENT zone at every cast
// evaluation site (the offer walks, beginCast's own re-check; the pending
// cast's from is always the object's zone at creation). A ByYou token holds
// unconditionally on the zone match: the caster attempting the cast IS the
// evaluating you. No log scan, so a prior completed cast's origin cannot
// leak into this cast's evaluation.
func (e *Engine) castOriginAdmitsAtZone(spec string, objID state.ObjID, from state.Zone) (string, bool) {
	for _, tok := range castOriginTokens {
		if !strings.Contains(spec, tok.token) {
			continue
		}
		var ok bool
		if spec, ok = admitProvenanceAlternatives(spec, tok.token, from == tok.zone); !ok {
			return "", false
		}
	}
	return spec, true
}

// webSlingedAdmits evaluates the Web-slinging cast-provenance predicate
// (kw:Web-slinging, CR 702.186a-family): the object's LATEST cast was paid
// for with its web-slinging cost -- state.FlagWebSlinged, stamped by the
// pay-time CastInfo (modeFlags). The flag is a CastProvenanceFlag
// (state/object.go), so a stack copy (put on the stack, never cast --
// CR 707.10) reads false without a separate guard, and a card never put on
// the stack (cheated into play) carries no bit either. There is no pre-push
// OFFER-window fallback here, exactly like the CastSa family: the flag does
// not exist until the payment has run, and the only corpus readers
// (Spiders-Man, Heroic Horde's ETB trigger; Scarlet Spider, Ben Reilly's
// Sensational Save replacement) evaluate it at battlefield entry, after the
// push.
func (e *Engine) webSlingedAdmits(spec string, objID state.ObjID) (string, bool) {
	if !strings.Contains(spec, "webSlinged") {
		return spec, true
	}
	holds := false
	if o := e.G.Obj(objID); o != nil {
		holds = o.CastFlags&state.FlagWebSlinged != 0
	}
	return admitProvenanceAlternatives(spec, "webSlinged", holds)
}

// castProvenanceAdmitsPending is castProvenanceAdmits with the pending-cast
// guard the two COST paths need (the ReduceCost/RaiseCost statics'
// ValidCard$, and the RestrictValid$ mana-restriction read): a
// provenance-keyed spec is UNRESOLVABLE while the priced object has no cast
// in the log yet. The offer-side walk (castable → costPayable) and the
// option-selection snapshot (costModifiers) both evaluate pre-push, where
// the log scan reads false and the NEGATED spellings would wrongly hold —
// a hand cast would be offered as payable on mana the payment then refuses,
// or priced at a reduction the payment must not take. Such a spec denies
// the whole match while the object is off the stack (the fail-closed
// direction both call sites document); continueCast re-prices the pending
// cast right after CR 601.2a's push, once the PutOnStack is in the log. A
// spec without a provenance token is returned unchanged, so every
// unrelated cost evaluation is byte-identical.
func (e *Engine) castProvenanceAdmitsPending(spec string, objID state.ObjID, you state.PlayerID) (string, bool) {
	if !strings.Contains(spec, "wasCastFromYourHand") && !strings.Contains(spec, "wasCastByYou") &&
		!specCarriesCastOrigin(spec) {
		return spec, true
	}
	o := e.G.Obj(objID)
	if o == nil {
		return "", false
	}
	if o.Zone != state.ZStack {
		// The origin-zone family is priceable PRE-push, unlike the hand
		// families: the origin a cast from here will carry is the object's
		// CURRENT zone (every cast evaluation site's pending origin — see
		// castOriginAdmitsAtZone), so the affordability walk can honestly
		// admit the restricted/reduced cost instead of the blanket deny that
		// made a RestrictValid$ Spell.wasCastFromYourGraveyard batch unable to
		// fund the very cast it names (Lord of the Forsaken's payment path).
		// The hand families keep the deny: their ByYou cast is not yet in the
		// log and the negated spellings would wrongly hold.
		if specCarriesCastOrigin(spec) {
			return e.castOriginAdmitsAtZone(spec, objID, o.Zone)
		}
		return "", false
	}
	return e.castProvenanceAdmits(spec, objID, you)
}

// specProvenanceGate is the chain's one-probe early-out. Every token any
// stage of castProvenanceAdmitsWindow strips carries the substring "Cast":
// the hand/any-origin/bare/origin-zone families are all "wasCast..." (the
// bare stage's specHasBareWasCast itself starts with an Index of "wasCast"),
// and the CastSa family's stage is gated on Contains "CastSa". A spec without
// "Cast" therefore passes every stage unchanged with ok true, so returning it
// directly is the chain's exact answer. Measured before this gate: ~95% of
// the chain's calls came from the layer walk's Affected$ match
// (matchesWithChars), almost none carrying a token, each paying ~10
// Contains/Index probes (the six stages' guards). The gate is a pure
// function of the spec, cached per spec string in a direct-mapped front keyed
// by the string's data pointer and length (effects' specFront pattern: spec
// text comes from the immutable card IR, so a hit is one atomic load and a
// pointer-equal string compare instead of a scan of the spec). Entries are
// immutable and published atomically, so concurrent engines only race to
// replace a slot; the entry holds the string, so the compare is exact.
// provenanceGateVerify (the rules test binary) re-runs every stage's own
// guard on a gated spec and panics if one would have fired.
//
// origin additionally records whether the spec carries an origin-zone token
// (specCarriesCastOrigin, four probes): the earlier stages only strip whole
// '+'-separated tokens, so a spec without one never gains one on the way
// down the chain, and castOriginAdmits is skipped for it.
func specProvenanceGate(spec string) provGate {
	slot := &provGateFront[provGateSlot(spec)]
	if ent := slot.Load(); ent != nil && ent.spec == spec {
		return ent.provGate
	}
	g := computeProvenanceGate(spec)
	slot.Store(&provGateEntry{spec: spec, provGate: g})
	return g
}

// computeProvenanceGate is specProvenanceGate's uncached body: every stage's
// own guard, evaluated on the unrewritten spec.
func computeProvenanceGate(spec string) provGate {
	var g provGate
	// webSlinged is the one token in this chain that does NOT carry the
	// substring "Cast" (kw:Web-slinging, the CastSa sibling without the word:
	// `Card.Self+wasCast+webSlinged`), so its probe sits before the early-out
	// below.
	if strings.Contains(spec, "webSlinged") {
		g.stages |= provWebSlinged
	}
	if !strings.Contains(spec, "Cast") {
		return g
	}
	handByYou := strings.Contains(spec, "wasCastFromYourHandByYou")
	if handByYou {
		g.stages |= provHandByYou
	}
	if strings.Contains(spec, "wasCastByYou") {
		g.stages |= provAtAllByYou
	}
	if specHasBareWasCast(spec) {
		g.stages |= provBare
	}
	if !handByYou && strings.Contains(spec, "wasCastFromYourHand") {
		g.stages |= provHandAny
	}
	if strings.Contains(spec, "CastSa") {
		g.stages |= provCastSa
	}
	g.origin = specCarriesCastOrigin(spec)
	g.may = g.stages != 0 || g.origin
	return g
}

// provGate stage bits: each is set iff that stage's own guard fires on the
// unrewritten spec.
const (
	provHandByYou uint8 = 1 << iota
	provAtAllByYou
	provBare
	provHandAny
	provCastSa
	provWebSlinged
)

type provGate struct {
	// may is false when no stage (nor the origin family) would fire: the
	// chain's answer is the spec itself, ok.
	may    bool
	origin bool
	stages uint8
}

type provGateEntry struct {
	spec string
	provGate
}

const provGateBits = 12

var provGateFront [1 << provGateBits]atomic.Pointer[provGateEntry]

func provGateSlot(spec string) uint {
	p := uintptr(unsafe.Pointer(unsafe.StringData(spec)))
	h := uint64(p>>3) ^ uint64(len(spec))*0x9e3779b97f4a7c15
	h ^= h >> 29
	h *= 0xbf58476d1ce4e5b9
	h ^= h >> 32
	return uint(h) & (1<<provGateBits - 1)
}

// provenanceGateVerify: see derivedMemoVerify. Set by the rules test binary.
var provenanceGateVerify = derivedMemoVerifyFlag != ""
