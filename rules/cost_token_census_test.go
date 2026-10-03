package rules

// cost_token_census_test.go — the WHOLE-CORPUS census of the non-mana cost
// tokens whose TYPE SLOT can name something other than a card filter. The
// Land Grant bug (Reveal<1/Hand> read as "reveal one card matching the card
// filter Hand", a filter no card ever matches, so the cost was silently
// unpayable and the free cast was never offered) is one instance of a class:
// a cost token whose type slot is a ZONE or a relational word rather than a
// card filter. This census enumerates every distinct such token shape in the
// corpus, parses it through the REAL ParseCost, and holds it to one of two
// verdicts:
//
//   - the type slot's base word is a card-filter base the matcher reads
//     (a card type, supertype, creature type, `Card`, `Permanent`, `Any`,
//     `Spell`, a self-reference `CARDNAME`/`NICKNAME`, or the `Random`
//     discard draw), OR
//   - it is an explicit entry in costNonFilterSpecs with the reason it is
//     left (and, for `Hand`, the whole-zone reading the fix implements).
//
// A NEW non-filter word that would silently become an unmatched filter fails
// the census, naming the token and its carriers; an entry in the table the
// corpus no longer carries fails too (Ruling R-20's two-direction contract).
//
// It also censuses every `Mode$ AlternativeCost` static that carries a
// CheckSVar$ gate: the gate must be evaluable by effects.CheckSVarHolds, or
// the alternative is dead before its cost is ever priced.

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules/chars"
)

// costHeadRe matches the cost heads whose type slot can name a non-filter
// word. It mirrors the brief's census grep, plus RevealOrChoose.
var costHeadRe = map[string]bool{
	"Reveal": true, "RevealOrChoose": true, "Discard": true,
	"Exile": true, "ExileFromHand": true, "Return": true,
	"Sac": true, "Tap": true, "PutCardToLib": true, "Mill": true,
}

// specHead returns the cost token's head (the text before '<') when it is one
// the census reads, with the inside of the angle brackets; ok is false for
// every other token.
func specHead(tok string) (head, inside string, ok bool) {
	h, rest, found := strings.Cut(tok, "<")
	if !found || !strings.HasSuffix(rest, ">") || !costHeadRe[h] {
		return "", "", false
	}
	return h, strings.TrimSuffix(rest, ">"), true
}

// specBaseWord reduces a cost spec to its base filter word: the first
// comma-separated alternative, before any '.' predicate or '+'/'!' modifier
// (matching matchesBase's own normalisation), with a leading "non" stripped.
func specBaseWord(spec string) string {
	alt, _, _ := strings.Cut(spec, ",")
	alt, _, _ = strings.Cut(alt, ".")
	alt, _, _ = strings.Cut(alt, "+")
	alt, _, _ = strings.Cut(alt, "!")
	alt = strings.TrimSpace(alt)
	if nb, ok := strings.CutPrefix(alt, "non"); ok && nb != "" {
		alt = nb
	}
	return alt
}

// costVariableCount reports whether a token's count field is a variable form
// (X/Y/All) rather than a literal N. Those are a different class from the
// fixed-N type-slot bug this census targets: ParseCost does not model them
// (they fall into Cost.Unknown) and the raise/announce bridge owns part of
// the surface (Discard<X/Spec> on an announced cost). They are reported in
// the census log and NOT classified as fixed-N type slots. Declared here so
// the exemption is visible to a reviewer, not hidden in a boolean.
func costVariableCount(n string) bool {
	switch n {
	case "X", "Y", "All":
		return true
	}
	return false
}

// costNonFilterSpecs is the explicit verdict table for a type slot that is NOT
// a card-filter base word. Every entry must be measured by the corpus, or the
// census reports it stale. Keep each reason precise: a reviewer treats a new
// row as the wrong fix.
var costNonFilterSpecs = map[string]string{
	// Reveal<1/Hand>: "reveal your hand". The count is display noise; the
	// whole hand is the reveal, the same reading Discard<1/Hand> and
	// Discard<0/Hand> get. Payable with an empty hand (CR 701.20a). Read by
	// isWholeHandRevealSpec at the offer gate (nonManaCastable), the payment
	// (revealCostAsk) and the announcement (emitChoiceCosts).
	"Hand": "whole hand (Reveal<N/Hand>) -- isWholeHandRevealSpec; payable empty (CR 701.20a)",
	// Discard<1/Random>: a discard the OTHER effect/opponents choose, not a
	// hand filter. discardCandidates reads Random as "any hand card".
	"Random": "random discard (Discard<N/Random>) -- discardCandidates reads it as any hand card",
	// CARDNAME/NICKNAME: a self-reference, not a card type. The matcher owns
	// the object-ID semantics (matchesSpecFrom with sc.Source); a cast's own
	// card cannot pay it (it is on the stack) but an ability's source in the
	// hand can (the forecast family).
	"CARDNAME": "self-reference by object id (matchesSpecFrom) -- payable from the source's own zone",
	"NICKNAME": "self-reference by object id (normalised to CARDNAME by sacrificeMatchSpec)",
	// LEFT: genuinely relational type slots the build does not model. Both are
	// recorded here so a reviewer can see them; the census's job is to make
	// them visible, not to silently treat them as filters. (Filed per the
	// dispatch's new-ticket process.)
	// SameColor is no longer LEFT: Reveal<2/SameColor> (Illuminated Folio,
	// "reveal two cards from your hand that share a color") is read as a
	// RELATIONAL part by isSameColorRevealSpec at the offer gate
	// (nonManaCastable) and the payment (revealCostAsk). The ask carries
	// decision.SetPropShared over each candidate's DERIVED colour tokens
	// (setPropTokens "color"), so Decision.Validate and botpolicy's Clamp
	// enforce the same pair rule the offer gate measured.
	"SameColor": "relational same-colour reveal (Reveal<2/SameColor>, Illuminated Folio) -- isSameColorRevealSpec; SetPropShared over derived colours",
	"LastDrawn": "implemented (history-keyed): Discard<1/LastDrawn> (Jandor's Ring) reads the last events.Draw this turn through lastDrawnThisTurn; discardCandidates returns that one card only while it is still in hand, so a no-draw or left-hand cost is withheld",
	// ExileFromHand<1/All> (Herigast, Erupting Nullkite): "exile your whole
	// hand". The count is display noise; the whole zone is the payment, the
	// same reading Discard<1/Hand> and Reveal<1/Hand> get. Unlike the
	// whole-hand reveal it is NOT payable empty -- the token still demands
	// part.N (written 1) cards. Read by isWholeZoneExileSpec at the triggered
	// offer gate, the triggered settle walk, the cast/activation gate and
	// payment, and (task exil1) the unless-pay gate and continuation
	// (Grip of Amnesia's ExileFromGrave<1/All>, an UnlessCost$ the census's
	// own Cost$ walk does not visit). The census walks Cost$ params only, so
	// Herigast is its only measured carrier.
	"All": "whole zone (ExileFromHand<1/All>) -- isWholeZoneExileSpec; needs part.N cards (CR 118.8-family)",
}

// walkCostParams visits every Cost$ parameter a face carries: printed and
// granted abilities, triggers, replacements, SVar abilities, and statics.
func walkCostParams(f *cards.Face, visit func(raw string)) {
	seen := map[*cards.SA]bool{}
	var walk func(sa *cards.SA)
	walk = func(sa *cards.SA) {
		for ; sa != nil && !seen[sa]; sa = sa.Sub {
			seen[sa] = true
			if c, ok := sa.Params["Cost"]; ok {
				visit(c)
			}
		}
	}
	for _, a := range f.Abilities {
		walk(a)
	}
	for _, tr := range f.Triggers {
		walk(tr.Effect)
	}
	for _, r := range f.Repls {
		walk(r.With)
	}
	f.EachSVarAbility(walk)
	for _, st := range f.Statics {
		if c, ok := st.Params["Cost"]; ok {
			visit(c)
		}
	}
}

// TestCostTokenCensus enumerates every distinct cost token in the corpus whose
// head is one of costHeadRe, parses it, and holds its type slot to a filter
// base or an explicit table verdict.
func TestCostTokenCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)

	filterBases := map[string]bool{
		"Card": true, "Permanent": true, "Any": true, "Spell": true, "SpellAbility": true,
		"Affinity": true, "PermanentCard": true,
	}
	for _, w := range coreCardTypes {
		filterBases[w] = true
	}
	for _, w := range chars.SupertypeWords() {
		filterBases[w] = true
	}
	for _, w := range effects.CreatureTypeWordList() {
		filterBases[w] = true
	}
	// The base vocabulary is every SUBTYPE the corpus prints (artifact and
	// land subtypes -- Food, Treasure, Desert, Forest -- are filter bases too,
	// just as the creature types are): the matcher's hasTypeCtx reads any
	// subtype word off the object, so the census derives the same vocabulary
	// from the corpus rather than hard-coding half of it. Token subtypes
	// (Blood) are printed only on token scripts the registry may not carry, so
	// they are added explicitly.
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			for _, ty := range f.Types {
				filterBases[ty] = true
			}
		}
	}
	for _, w := range []string{"Blood", "Prism", "Junk", "Powerstone", "Shard", "Incubator", "Map", "Gold"} {
		filterBases[w] = true
	}

	bySpec := map[string]map[string]bool{}   // fixed-N spec -> token shapes
	carriers := map[string]map[string]bool{} // token -> card names
	allTokens := map[string]bool{}
	varCount := map[string]bool{} // variable-count (X/Y/All) tokens, reported only
	nTokens := 0

	for _, c := range reg.Cards {
		name := c.Faces[0].Name
		for _, f := range c.Faces {
			walkCostParams(f, func(raw string) {
				for _, tok := range strings.Fields(raw) {
					_, inside, ok := specHead(tok)
					if !ok {
						continue
					}
					nTokens++
					allTokens[tok] = true
					if carriers[tok] == nil {
						carriers[tok] = map[string]bool{}
					}
					carriers[tok][name] = true
					// The count and type slot are the first two fields of
					// N/Spec[/desc].
					fields := strings.Split(inside, "/")
					if len(fields) < 2 {
						continue
					}
					if costVariableCount(fields[0]) {
						varCount[tok] = true
						continue
					}
					spec := fields[1]
					if bySpec[spec] == nil {
						bySpec[spec] = map[string]bool{}
					}
					bySpec[spec][tok] = true
				}
			})
		}
	}
	if nTokens == 0 {
		t.Fatal("census found no cost tokens: the corpus walk is broken")
	}

	// Every FIXED-N token parses through the real ParseCost without being
	// reported Unknown (a head ParseCost does not recognise falls back to one
	// generic mana and lands in Cost.Unknown: a silently mispriced card). The
	// variable-count forms are exempted by costVariableCount and reported
	// below instead.
	tokens := make([]string, 0, len(allTokens))
	for tok := range allTokens {
		tokens = append(tokens, tok)
	}
	sort.Strings(tokens)
	var unknown, varToks []string
	for _, tok := range tokens {
		c := ParseCost(tok)
		for _, u := range c.Unknown {
			if strings.Contains(u, tok) || strings.Contains(tok, u) {
				if varCount[tok] {
					varToks = append(varToks, tok)
				} else {
					unknown = append(unknown, tok)
				}
			}
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		t.Errorf("fixed-count cost tokens ParseCost reports Unknown (priced as generic mana):\n  %s", strings.Join(unknown, "\n  "))
	}
	sort.Strings(varToks)
	t.Logf("variable-count (X/Y/All) tokens ParseCost leaves Unknown (announce/raise bridge class, not fixed-N): %d\n  %s",
		len(varToks), strings.Join(varToks, "\n  "))

	// Every Reveal/RevealOrChoose/Discard type slot must be a filter base, a
	// self-reference, or an explicit table entry.
	specs := make([]string, 0, len(bySpec))
	for s := range bySpec {
		specs = append(specs, s)
	}
	sort.Strings(specs)
	measured := map[string]bool{}
	var table strings.Builder
	for _, spec := range specs {
		base := specBaseWord(spec)
		verdict := ""
		switch {
		case filterBases[base]:
			verdict = "filter base " + base
		case costNonFilterSpecs[spec] != "":
			verdict = "table: " + costNonFilterSpecs[spec]
			measured[spec] = true
			// Couple the whole-hand verdict to the production predicate: a
			// revert or rename of isWholeHandRevealSpec (the fix) must fail
			// the census, not just the Land Grant behaviour test.
			if spec == "Hand" && !isWholeHandRevealSpec(spec) {
				t.Errorf("isWholeHandRevealSpec(%q) is false: the whole-hand reveal reading is gone", spec)
			}
			// SameColor's verdict is coupled to its production predicate the
			// same way: a revert or rename of isSameColorRevealSpec (the
			// Illuminated Folio fix) must fail the census, not just the
			// card's behaviour test.
			if spec == "SameColor" && !isSameColorRevealSpec(spec) {
				t.Errorf("isSameColorRevealSpec(%q) is false: the same-colour reveal reading is gone", spec)
			}
			// The whole-zone exile verdict is coupled the same way: a revert or
			// rename of isWholeZoneExileSpec must fail the census, not just the
			// Herigast behaviour test.
			if spec == "All" && !isWholeZoneExileSpec(spec) {
				t.Errorf("isWholeZoneExileSpec(%q) is false: the whole-zone exile reading is gone", spec)
			}
		case base != spec && costNonFilterSpecs[base] != "":
			// A predicate-carrying spec whose BASE is a table word (e.g.
			// CARDNAME/this card): the base's verdict applies.
			verdict = "table (base): " + costNonFilterSpecs[base]
		default:
			t.Errorf("cost type slot %q silently reads as an unmatched card filter (base %q): tokens %v",
				spec, base, sortedKeys(bySpec[spec]))
			verdict = "UNCLASSIFIED"
		}
		tokens := map[string]bool{}
		specCarriers := map[string]bool{}
		for tok := range bySpec[spec] {
			tokens[tok] = true
			for n := range carriers[tok] {
				specCarriers[n] = true
			}
		}
		var toks []string
		for tok := range tokens {
			toks = append(toks, tok)
		}
		sort.Strings(toks)
		cs := sortedKeys(specCarriers)
		if len(cs) > 6 {
			cs = append(cs[:6], "…")
		}
		table.WriteString("  " + spec + "  --  " + verdict + "  [" + strings.Join(toks, ", ") + "]  e.g. " + strings.Join(cs, ", ") + "\n")
	}
	t.Logf("cost-token census: %d cost tokens, %d distinct type slots\n%s", nTokens, len(specs), table.String())

	// Two-direction ratchet over the explicit table: every entry must be
	// measured by the corpus (a stale entry means the shape vanished -- or,
	// worse, was silently reclassified as a filter base).
	names := make([]string, 0, len(costNonFilterSpecs))
	for s := range costNonFilterSpecs {
		names = append(names, s)
	}
	sort.Strings(names)
	for _, s := range names {
		if !measured[s] {
			t.Errorf("costNonFilterSpecs entry %q is never measured by the corpus; delete it or fix the reading", s)
		}
	}
}

func sortedKeys(m map[string]bool) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// TestCostTokenCensusAlternativeCostStatic: every `Mode$ AlternativeCost` static in the
// corpus. It asserts the class contract the Land Grant bug violated -- every
// alternative's Cost$ parses through the real ParseCost without silently
// falling back to generic mana (Cost.Unknown) -- and logs the distinct Cost$
// shapes together with the verdict of each CheckSVar$ gate. The gate itself
// FAILS OPEN when the evaluator cannot read its body (sVarGateOK's documented
// contract), so an `unevaluable` verdict is reported, not failed; the Land
// Grant gate is asserted evaluable as the anchor that keeps the census from
// being vacuous.
func TestCostTokenCensusAlternativeCostStatic(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t)

	type altRow struct {
		card  string
		cost  string
		check string
		cmp   string
		eval  string
	}
	var rows []altRow
	shapes := map[string]map[string]bool{}
	otherUnknown := map[string]bool{}
	nStatics := 0
	landGrantEvaluated := false
	for _, c := range reg.Cards {
		name := c.Faces[0].Name
		for _, f := range c.Faces {
			for _, st := range f.Statics {
				if st.Mode != "AlternativeCost" {
					continue
				}
				nStatics++
				cost := strings.TrimSpace(st.Params["Cost"])
				check := strings.TrimSpace(st.Params["CheckSVar"])
				cmp := strings.TrimSpace(st.Params["SVarCompare"])
				if shapes[cost] == nil {
					shapes[cost] = map[string]bool{}
				}
				shapes[cost][name] = true

				// The cost must parse: an unparsed head is priced as one generic
				// mana and the real cost never happens. Enforced for the census
				// heads (costHeadRe); any OTHER unparsed head is logged below
				// and reported as an issue, since it is a different class from
				// the type-slot bug this ticket fixes (GainLife<N/Player...>).
				parsed := ParseCost(cost)
				for _, u := range parsed.Unknown {
					if costHeadRe[u] {
						t.Errorf("AlternativeCost Cost$ %q on %s parses Unknown %q (priced as generic mana): the alternative is mispriced",
							cost, name, u)
					} else {
						otherUnknown[u] = true
					}
				}

				verdict := "no gate"
				if check != "" {
					ctx := &effects.Ctx{Source: 0, Controller: 0, SVars: f.SVars}
					holds, evaluated := effects.CheckSVarHolds(e, ctx, check, cmp)
					if !evaluated {
						verdict = "unevaluable here (offer gate fails OPEN)"
					} else {
						verdict = "evaluable (fixture holds=" + boolStr(holds) + ")"
					}
				}
				if name == "Land Grant" && strings.Contains(verdict, "evaluable") {
					landGrantEvaluated = true
				}
				rows = append(rows, altRow{name, cost, check, cmp, verdict})
			}
		}
	}
	if nStatics == 0 {
		t.Fatal("no Mode$ AlternativeCost static found: the corpus walk is broken")
	}
	if !landGrantEvaluated {
		t.Fatal("anchor: Land Grant's CheckSVar$ gate (no land in hand) is not evaluable")
	}
	if len(otherUnknown) > 0 {
		t.Logf("other unparsed AlternativeCost heads (outside this census's heads; filed as issues): %v", sortedKeys(otherUnknown))
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].card != rows[j].card {
			return rows[i].card < rows[j].card
		}
		return rows[i].cost < rows[j].cost
	})
	var sb strings.Builder
	fmt.Fprintf(&sb, "alternative-cost census: %d statics, %d distinct Cost$ shapes\n", nStatics, len(shapes))
	sb.WriteString("shapes (Cost$ -> carriers):\n")
	var shapeNames []string
	for s := range shapes {
		shapeNames = append(shapeNames, s)
	}
	sort.Strings(shapeNames)
	for _, s := range shapeNames {
		cs := sortedKeys(shapes[s])
		if len(cs) > 4 {
			cs = append(cs[:4], "…")
		}
		sb.WriteString("  " + s + "  [" + strings.Join(cs, ", ") + "]\n")
	}
	sb.WriteString("gated statics:\n")
	for _, r := range rows {
		if r.check == "" {
			continue
		}
		sb.WriteString("  " + r.card + "  Cost$ " + r.cost + "  CheckSVar$ " + r.check + " " + r.cmp + "  -> " + r.eval + "\n")
	}
	t.Logf("%s", sb.String())
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
