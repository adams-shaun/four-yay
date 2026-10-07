package v1agent

import (
	"bytes"
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/spellbench"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestCardFactsMatchIR regenerates cardfacts.json from gorge's compiled card
// IR for every card of the pauper-kernel catalog and every row of the
// kernel card DB (kernelcards.json: sideboard cards and tokens included),
// and fails when the committed file differs. SB_REGEN_CARDFACTS=1 rewrites
// the file instead.
//
// The file holds derived facts in this package's own vocabulary (types,
// subtypes, mana value, power/toughness, keyword heads, effect API names,
// amounts, target and cost classes, polarity) -- never script text.
func TestCardFactsMatchIR(t *testing.T) {
	r := testutil.CorpusRegistry(t)
	ids, err := spellbench.CatalogIDs(spellbench.PauperKernel)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, id := range ids {
		f, err := spellbench.File(spellbench.PauperKernel, id)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range f.Cards {
			names[c.Name] = true
		}
	}
	folded := map[string]bool{}
	for n := range names {
		folded[normName(n)] = true
	}
	for _, k := range kernelCards {
		if !folded[normName(k.Name)] {
			names[k.Name] = true
			folded[normName(k.Name)] = true
		}
	}
	var sorted []string
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
	g := &factGen{r: r, tokenNames: map[string]string{}}
	byFold := map[string]*cards.Card{}
	for _, c := range r.AllCards() {
		if len(c.Faces) > 0 {
			byFold[normName(strings.ToLower(c.Faces[0].Name))] = c
		}
	}
	facts := map[string]*CardFact{}
	var missing []string
	for _, n := range sorted {
		if strings.HasSuffix(n, " Token") {
			if c := g.tokenByName(n); c != nil {
				facts[n] = g.card(c)
				continue
			}
		}
		c, ok := r.Lookup(n)
		if !ok {
			c, ok = byFold[normName(strings.ToLower(n))]
		}
		if !ok || len(c.Faces) == 0 {
			missing = append(missing, n)
			continue
		}
		facts[n] = g.card(c)
	}
	if len(missing) > 0 {
		t.Logf("not in the gorge corpus (kernel data only): %v", missing)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", " ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(facts); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("SB_REGEN_CARDFACTS") != "" {
		if err := os.WriteFile("cardfacts.json", buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	have, err := os.ReadFile("cardfacts.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(have, buf.Bytes()) {
		t.Fatalf("cardfacts.json is stale against the corpus; regenerate with SB_REGEN_CARDFACTS=1")
	}
}

type factGen struct {
	r          *cards.Registry
	tokenNames map[string]string // script stem -> token name
}

// tokenByName finds a token script by its card name; among several the
// lexically first stem wins (deterministic).
func (g *factGen) tokenByName(name string) *cards.Card {
	var stems []string
	for stem, c := range g.r.Tokens {
		if len(c.Faces) > 0 && (strings.EqualFold(c.Faces[0].Name, name) || strings.EqualFold(c.Faces[0].Name+" Token", name)) {
			stems = append(stems, stem)
		}
	}
	if len(stems) == 0 {
		return nil
	}
	sort.Strings(stems)
	return g.r.Tokens[stems[0]]
}

func (g *factGen) tokenName(stem string) string {
	if n, ok := g.tokenNames[stem]; ok {
		return n
	}
	n := ""
	if c, ok := g.r.Token(stem); ok && len(c.Faces) > 0 {
		n = c.Faces[0].Name
	}
	g.tokenNames[stem] = n
	return n
}

func (g *factGen) card(c *cards.Card) *CardFact {
	cf := g.face(c.Faces[0])
	for _, f := range c.Faces[1:] {
		cf.Faces = append(cf.Faces, g.face(f))
	}
	return cf
}

var cardTypeWords = map[string]bool{"Land": true, "Creature": true, "Instant": true, "Sorcery": true,
	"Artifact": true, "Enchantment": true, "Planeswalker": true, "Battle": true, "Kindred": true, "Tribal": true}
var superTypeWords = map[string]bool{"Basic": true, "Legendary": true, "Snow": true, "World": true, "Token": true}

func (g *factGen) face(f *cards.Face) *CardFact {
	cf := &CardFact{CMC: int(f.Cmc())}
	for _, ty := range f.Types {
		switch {
		case ty == "Land", ty == "Creature", ty == "Instant", ty == "Sorcery", ty == "Artifact", ty == "Enchantment":
			cf.Types = append(cf.Types, strings.ToLower(ty))
		case ty == "Legendary":
			cf.Legendary = true
		case cardTypeWords[ty], superTypeWords[ty]:
		default:
			cf.Subtypes = append(cf.Subtypes, strings.ToLower(ty))
		}
	}
	if f.PT != "" {
		cf.Power, cf.Toughness, cf.HasPT = f.Power(), f.Toughness(), true
	}
	for _, kw := range f.Keywords {
		head, rest := kw, ""
		if i := strings.IndexByte(head, ':'); i >= 0 {
			head, rest = head[:i], head[i+1:]
		}
		switch head {
		case "Flying", "Reach", "Haste", "Trample", "Flash", "Defender", "Lifelink", "Deathtouch",
			"First Strike", "Double Strike", "Vigilance", "Menace", "Hexproof", "Indestructible",
			"Kicker", "Madness", "Flashback", "Ninjutsu", "Affinity", "Bestow", "Cycling", "TypeCycling",
			"Embalm", "Plot", "Changeling", "Protection", "Equip", "Backup", "Devoid":
			cf.Keywords = append(cf.Keywords, head)
		case "Ward", "Storm", "Escape", "Bargain", "Shroud", "Prowess", "Landwalk", "Islandwalk":
			cf.Keywords = append(cf.Keywords, head)
		}
		switch head {
		case "Flashback":
			cf.Flashback = parseCost(rest)
		case "Kicker":
			cf.Kicker = parseCost(rest)
		case "Ninjutsu":
			cf.Ninjutsu = true
		case "Changeling":
			cf.Changeling = true
		}
	}
	if _, ok := f.SVars["ETBTapped"]; ok {
		cf.ETBTapped = true
	}
	for _, a := range f.Abilities {
		switch a.Kind {
		case "SP":
			cf.Spell = g.effect(a, f.SVars)
		case "AB":
			if a.API == "Mana" {
				cf.Mana = true
				cf.ManaAbs = append(cf.ManaAbs, *g.effect(a, f.SVars))
				continue
			}
			cf.Abilities = append(cf.Abilities, *g.effect(a, f.SVars))
		}
	}
	for _, tr := range f.Triggers {
		if tr.Effect == nil {
			continue
		}
		e := g.effect(tr.Effect, f.SVars)
		e.Trigger = tr.Mode
		if tr.Mode == "ChangesZone" && tr.Params["Destination"] == "Battlefield" {
			e.Trigger = "ETB"
		}
		if tr.Mode == "ChangesZone" && tr.Params["Origin"] == "Battlefield" {
			e.Trigger = "LTB"
			if tr.Params["Destination"] == "Graveyard" {
				e.Trigger = "Dies"
			}
		}
		if strings.Contains(tr.Params["ValidCard"], "+kicked") {
			e.Kicked = true
		}
		cf.Triggers = append(cf.Triggers, *e)
	}
	for _, st := range f.Statics {
		if s := staticFact(st.Mode, st.Params, f.SVars); s != nil {
			cf.Statics = append(cf.Statics, *s)
		}
	}
	return cf
}

func staticFact(mode string, p, svars map[string]string) *StaticFact {
	s := &StaticFact{}
	switch mode {
	case "Continuous":
		s.Mode = "continuous"
		switch a := p["Affected"]; {
		case a == "Card.Self" || a == "Creature.Self":
			s.Affects = "self"
		case strings.Contains(a, "EquippedBy"):
			s.Affects = "equipped"
		case strings.Contains(a, "EnchantedBy"):
			s.Affects = "enchanted"
		case strings.Contains(a, "YouCtrl"):
			s.Affects = "yours"
		default:
			s.Affects = "other"
		}
		s.Power, _ = strconv.Atoi(strings.TrimPrefix(p["AddPower"], "+"))
		s.Toughness, _ = strconv.Atoi(strings.TrimPrefix(p["AddToughness"], "+"))
		if kw := p["AddKeyword"]; kw != "" {
			for _, k := range strings.Split(kw, " & ") {
				s.Keywords = append(s.Keywords, strings.TrimSpace(k))
			}
		}
		if ip := p["IsPresent"]; ip != "" {
			s.Cond = "present"
		}
		if c := p["Condition"]; c != "" {
			s.Cond = strings.ToLower(c)
		}
	case "ReduceCost":
		s.Mode = "reduce_cost"
		s.Reduce = parseAmount(p["Amount"], svars)
	case "AlternativeCost":
		s.Mode = "alt_cost"
		s.Cost = parseCost(p["Cost"])
		if p["CheckSVar"] != "" {
			s.Cond = "conditional"
		}
	case "OptionalCost":
		s.Mode = "optional_cost"
		s.Cost = parseCost(p["Cost"])
	case "CantBlock", "CantBlockBy":
		s.Mode = "cant_block"
	case "CantAttack":
		s.Mode = "cant_attack"
	default:
		return nil
	}
	return s
}

// harmfulAPIs are effects that hurt whatever they target.
var harmfulAPIs = map[string]bool{
	"DealDamage": true, "Destroy": true, "Counter": true, "Tap": true, "Sacrifice": true,
	"DigUntil": true, "Mill": true, "Discard": true,
}

// parseSVarSA parses an SVar holding an ability ("DB$ API | Key$ Value |
// ...") into an unlinked SA; nil when it is not one.
func parseSVarSA(text string) *cards.SA {
	parts := strings.Split(text, " | ")
	head := strings.TrimSpace(parts[0])
	var kind string
	switch {
	case strings.HasPrefix(head, "DB$"):
		kind = "DB"
	case strings.HasPrefix(head, "AB$"):
		kind = "AB"
	case strings.HasPrefix(head, "SP$"):
		kind = "SP"
	default:
		return nil
	}
	sa := &cards.SA{Kind: kind, API: strings.TrimSpace(head[3:]), Params: map[string]string{}}
	for _, p := range parts[1:] {
		if i := strings.Index(p, "$"); i > 0 {
			sa.Params[strings.TrimSpace(p[:i])] = strings.TrimSpace(p[i+1:])
		}
	}
	return sa
}

func (g *factGen) effect(a *cards.SA, svars map[string]string) *EffectFact {
	e := &EffectFact{API: a.API}
	if c := a.Params["Cost"]; c != "" {
		e.Cost = parseCost(c)
	}
	if z := a.Params["ActivationZone"]; z != "" {
		e.Zone = strings.ToLower(z)
	}
	e.Sorcery = a.Params["SorcerySpeed"] == "True"
	if a.API == "Charm" || a.API == "GenericChoice" {
		for _, ch := range strings.Split(a.Params["Choices"], ",") {
			if sa := parseSVarSA(svars[strings.TrimSpace(ch)]); sa != nil {
				m := g.effect(sa, svars)
				if u := sa.Params["UnlessCost"]; u != "" && sa.Params["UnlessSwitched"] == "True" {
					m.Cost = parseCost(u) // "pay this to get the effect"
				}
				e.Modes = append(e.Modes, *m)
			}
		}
	}
	for s := a; s != nil; s = s.Sub {
		if s != a {
			e.Chain = append(e.Chain, s.API)
		}
		p := s.Params
		// legacy fields: computed exactly as before sb-generic
		if n, err := strconv.Atoi(p["NumDmg"]); err == nil && e.Damage == 0 {
			e.Damage = n
		}
		if n, err := strconv.Atoi(strings.TrimPrefix(p["NumAtt"], "+")); err == nil && e.PumpPower == 0 {
			e.PumpPower = n
		}
		if n, err := strconv.Atoi(strings.TrimPrefix(p["NumDef"], "+")); err == nil && e.PumpToughness == 0 {
			e.PumpToughness = n
		}
		if n, err := strconv.Atoi(p["NumCards"]); err == nil && e.Cards == 0 {
			e.Cards = n
		}
		if v := p["ValidTgts"]; v != "" && e.Target == "" {
			e.Target = targetClass(v)
		}
		if p["IsCurse"] == "True" {
			e.Harmful = true
		}
		if harmfulAPIs[s.API] && p["Defined"] != "You" {
			e.Harmful = true
		}
		if s.API == "ChangeZone" && p["Origin"] == "Battlefield" && (p["Destination"] == "Exile" || p["Destination"] == "Hand") && p["ValidTgts"] != "" {
			e.Harmful = true
		}
		if strings.HasPrefix(p["NumAtt"], "-") || strings.HasPrefix(p["NumDef"], "-") {
			e.Harmful = true
		}
		g.richer(e, s, svars)
	}
	return e
}

// richer fills the sb-generic fields from one link of the chain.
func (g *factGen) richer(e *EffectFact, s *cards.SA, svars map[string]string) {
	p := s.Params
	first := s.API == e.API && len(e.Chain) == 0
	if v := p["ValidTgts"]; v != "" && e.Tgt == nil {
		e.Tgt = parseFilter(v)
		if n, err := strconv.Atoi(p["TargetMax"]); err == nil && n > 1 {
			e.TgtMax = n
		}
		if strings.Contains(v, "cmcLEX") || strings.Contains(v, "cmcEQX") {
			e.TgtX = parseAmount("X", svars)
		}
	}
	if v := p["NumDmg"]; v != "" && e.Damage == 0 && e.DamageX == nil {
		if _, err := strconv.Atoi(v); err != nil {
			e.DamageX = parseAmount(v, svars)
		}
	}
	if v := p["NumAtt"]; v != "" && e.PumpX == nil {
		if _, err := strconv.Atoi(strings.TrimPrefix(v, "+")); err != nil && !strings.HasPrefix(v, "-") {
			e.PumpX = parseAmount(strings.TrimPrefix(v, "+"), svars)
		}
	}
	if s.API == "Mana" && e.ManaX == nil {
		if v := p["Amount"]; v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				e.ManaX = &Amount{N: n}
			} else {
				e.ManaX = parseAmount(v, svars)
			}
		}
	}
	if e.Defined == "" && p["Defined"] != "" {
		e.Defined = definedClass(p["Defined"])
	}
	switch s.API {
	case "DamageAll", "PumpAll", "DestroyAll", "ChangeZoneAll", "SacrificeAll", "TapAll", "UntapAll":
		if v := p["ValidCards"]; v != "" && e.Each == nil {
			e.Each = parseFilter(v)
		}
		if v := p["ValidPlayers"]; v != "" && e.EachPlayer == "" {
			if strings.Contains(v, "Opponent") {
				e.EachPlayer = "opp"
			} else {
				e.EachPlayer = "all"
			}
		}
	}
	if (s.API == "ChangeZone" || s.API == "ChangeZoneAll") && e.Origin == "" {
		e.Origin, e.Dest = strings.ToLower(p["Origin"]), strings.ToLower(p["Destination"])
		if v := p["ChangeType"]; v != "" {
			e.Search = parseFilter(v)
		}
	}
	if s.API == "Dig" && e.Search == nil {
		e.Origin = "library"
		e.Dest = strings.ToLower(p["DestinationZone"])
		if e.Dest == "" {
			e.Dest = "hand"
		}
		if v := p["ChangeValid"]; v != "" {
			e.Search = parseFilter(v)
		}
	}
	if s.API == "DigUntil" && e.Until == nil {
		e.Origin = "library"
		e.Until = parseFilter(p["Valid"])
		e.Dest = strings.ToLower(p["RevealedDestination"])
	}
	if s.API == "PutCounter" && e.Counter == "" {
		e.Counter = strings.ToLower(p["CounterType"])
		if e.Counter == "" {
			e.Counter = strings.ToLower(strings.Split(p["CounterTypes"], ",")[0])
		}
		e.CounterN, _ = strconv.Atoi(p["CounterNum"])
		if e.Counter == "stun" || e.Counter == "m1m1" {
			e.Harmful = true
		}
	}
	if s.API == "Counter" {
		if n, err := strconv.Atoi(p["UnlessCost"]); err == nil {
			e.Unless = n
		}
		if cp := p["ConditionPresent"]; cp != "" && p["ConditionDefined"] == "Targeted" {
			e.Cond, e.CondColor = "targeted_color", colorOf(cp)
		}
	}
	if s.API == "Destroy" || s.API == "ChangeZone" {
		if cp := p["ConditionPresent"]; cp != "" && p["ConditionDefined"] == "Targeted" {
			e.Cond, e.CondColor = "targeted_color", colorOf(cp)
		}
	}
	if s.API == "GainLife" && e.Life == 0 {
		e.Life, _ = strconv.Atoi(p["LifeAmount"])
	}
	if s.API == "Token" {
		n, err := strconv.Atoi(p["TokenAmount"])
		if err != nil || n == 0 {
			n = 1
		}
		e.Tokens += n
		if e.Token == "" {
			e.Token = g.tokenName(p["TokenScript"])
		}
	}
	if s.API == "Investigate" {
		e.Tokens++
		if e.Token == "" {
			e.Token = "Clue"
		}
	}
	if s.API == "Scry" || s.API == "Surveil" || s.API == "RearrangeTopOfLibrary" {
		e.Scry = true
	}
	if s.API == "Discard" {
		n, _ := strconv.Atoi(p["NumCards"])
		if n == 0 {
			n = 1
		}
		switch {
		case p["Defined"] == "You":
			e.Discard += n
		default:
			e.DiscardOpp = true
		}
	}
	if s.API == "Mill" {
		n, _ := strconv.Atoi(p["NumCards"])
		e.Mill += n
	}
	if s.API == "Effect" && p["ReplacementEffects"] != "" {
		for _, rn := range strings.Split(p["ReplacementEffects"], ",") {
			if strings.Contains(svars[rn], "Prevent$ True") {
				e.Prevent = true
			}
		}
	}
	if s.API == "Untap" {
		e.Untap = true
	}
	if s.API == "Tap" || s.API == "TapAll" {
		e.Tap = true
	}
	if kw := p["KW"]; kw != "" && (s.API == "Pump" || s.API == "PumpAll") {
		for _, k := range strings.Split(kw, " & ") {
			k = strings.TrimSpace(k)
			if !strings.HasPrefix(k, "HIDDEN") {
				e.Keywords = append(e.Keywords, k)
			}
		}
	}
	_ = first
}

func colorOf(v string) string {
	for _, c := range []string{"White", "Blue", "Black", "Red", "Green"} {
		if strings.Contains(v, c) {
			return strings.ToLower(c)
		}
	}
	return ""
}

func definedClass(v string) string {
	switch {
	case v == "You":
		return "you"
	case strings.Contains(v, "Opponent"):
		return "opp"
	case v == "Self":
		return "self"
	case strings.HasPrefix(v, "Targeted"), strings.HasPrefix(v, "Parent"):
		return "targeted"
	case v == "Enchanted":
		return "enchanted"
	case v == "Remembered":
		return "remembered"
	case strings.HasPrefix(v, "Triggered"):
		return "triggered"
	}
	return "other"
}

var colorWords = map[string]string{"White": "white", "Blue": "blue", "Black": "black", "Red": "red", "Green": "green"}

// parseFilter reduces an IR filter ("Creature.OppCtrl+nonLegendary",
// "Artifact.OppCtrl,Enchantment.OppCtrl", "Elf") to a Filter.
func parseFilter(v string) *Filter {
	f := &Filter{}
	add := func(list *[]string, s string) {
		for _, x := range *list {
			if x == s {
				return
			}
		}
		*list = append(*list, s)
	}
	for _, alt := range strings.Split(v, ",") {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		dot := strings.IndexByte(alt, '.')
		head, props := alt, ""
		if dot >= 0 {
			head, props = alt[:dot], alt[dot+1:]
		}
		// "Planeswalker" targets never occur for us; "Any" is creature or player
		switch head {
		case "Any":
			add(&f.Types, "creature")
			add(&f.Types, "player")
		case "Opponent":
			add(&f.Types, "player")
			f.Opp = true
		case "Spell":
			add(&f.Types, "card")
		default:
			if c, ok := colorWords[head]; ok {
				add(&f.Colors, c)
				add(&f.Types, "card")
			} else {
				add(&f.Types, strings.ToLower(head))
			}
		}
		if props == "" {
			continue
		}
		for _, pr := range strings.Split(props, "+") {
			switch {
			case pr == "OppCtrl" || pr == "OppOwn":
				f.Opp = true
			case pr == "YouCtrl" || pr == "YouOwn":
				f.You = true
			case pr == "Other":
				f.Other = true
			case pr == "tapped":
				f.Tapped = true
			case pr == "attacking":
				f.Attacker = true
			case pr == "cmcLEX":
				f.CMCLEX = true
			case strings.HasPrefix(pr, "named"):
				f.Named = true
			case strings.HasPrefix(pr, "non"):
				add(&f.Non, strings.ToLower(pr[3:]))
			case strings.HasPrefix(pr, "without"):
				add(&f.Without, strings.ToLower(pr[7:]))
			case strings.HasPrefix(pr, "with"):
				add(&f.With, strings.ToLower(pr[4:]))
			default:
				if c, ok := colorWords[pr]; ok {
					add(&f.Colors, c)
				}
			}
		}
	}
	return f
}

// parseAmount reads a number, an SVar naming one, or a Count$ expression.
func parseAmount(v string, svars map[string]string) *Amount {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	if n, err := strconv.Atoi(v); err == nil {
		return &Amount{N: n}
	}
	if sv, ok := svars[v]; ok {
		return parseAmount(sv, nil)
	}
	a := &Amount{Kind: "other"}
	expr, mods := v, ""
	if i := strings.IndexByte(v, '/'); i >= 0 && !strings.HasPrefix(v, "Discarded") {
		expr, mods = v[:i], v[i+1:]
	}
	switch {
	case strings.HasPrefix(expr, "Count$Valid "):
		a.Kind, a.Zone, a.Of = "count", "battlefield", parseFilter(strings.TrimPrefix(expr, "Count$Valid "))
	case strings.HasPrefix(expr, "Count$ValidGraveyard "):
		a.Kind, a.Zone, a.Of = "count", "graveyard", parseFilter(strings.TrimPrefix(expr, "Count$ValidGraveyard "))
	case strings.HasPrefix(expr, "Count$ValidHand "):
		a.Kind, a.Zone, a.Of = "count", "hand", parseFilter(strings.TrimPrefix(expr, "Count$ValidHand "))
	case strings.HasPrefix(expr, "Count$Metalcraft."), strings.HasPrefix(expr, "Count$Landfall."):
		parts := strings.Split(expr, ".")
		a.Kind = strings.ToLower(strings.TrimPrefix(parts[0], "Count$"))
		if len(parts) == 3 {
			a.Hi, _ = strconv.Atoi(parts[1])
			a.Lo, _ = strconv.Atoi(parts[2])
		}
	case expr == "Count$xPaid":
		a.Kind = "x"
	case strings.HasSuffix(expr, "$CardPower"):
		a.Kind = "power"
	}
	if strings.HasPrefix(mods, "Times.") {
		a.Mul, _ = strconv.Atoi(strings.TrimPrefix(mods, "Times."))
	}
	if strings.HasPrefix(mods, "Plus.") {
		a.Add, _ = strconv.Atoi(strings.TrimPrefix(mods, "Plus."))
	}
	if strings.HasPrefix(mods, "Minus") {
		n, _ := strconv.Atoi(strings.TrimLeft(strings.TrimPrefix(mods, "Minus"), "."))
		a.Add = -n
	}
	return a
}

// parseCost reads an IR cost ("3 B T Sac<1/CARDNAME>").
func parseCost(s string) *CostFact {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	c := &CostFact{}
	depth, start := 0, 0
	for i := 0; i <= len(s); i++ {
		switch {
		case i == len(s) || (s[i] == ' ' && depth == 0):
			parseCostToken(c, strings.TrimSpace(s[start:i]))
			start = i + 1
		case s[i] == '<':
			depth++
		case s[i] == '>':
			depth--
		}
	}
	return c
}

func costArg(tok string) (n int, what string) {
	i, j := strings.IndexByte(tok, '<'), strings.LastIndexByte(tok, '>')
	if i < 0 || j < i {
		return 1, ""
	}
	args := strings.Split(tok[i+1:j], "/")
	n, err := strconv.Atoi(args[0])
	if err != nil {
		n = 1
	}
	if len(args) > 1 {
		what = args[1]
	}
	return n, what
}

func parseCostToken(c *CostFact, tok string) {
	if tok == "" {
		return
	}
	name := tok
	if i := strings.IndexByte(tok, '<'); i >= 0 {
		name = tok[:i]
	}
	n, what := costArg(tok)
	self := what == "CARDNAME" || strings.HasPrefix(what, "CARDNAME")
	switch name {
	case "T":
		c.Tap = true
	case "Q":
		c.Other = true
	case "X":
		c.X = true
	case "Sac":
		if self {
			c.SacSelf = true
		} else {
			c.Sac, c.SacN = parseFilter(strings.ReplaceAll(what, ";", ",")), n
		}
	case "Discard":
		if self {
			c.DiscardSelf = true
		} else {
			c.Discard += n
		}
	case "Exile":
		if self {
			c.ExileSelf = true
		} else {
			c.Other = true
		}
	case "ExileFromGrave":
		if self {
			c.ExileSelf = true
		} else {
			c.ExileGrave += n
		}
	case "Return":
		c.Return = parseFilter(what)
	case "tapXType":
		c.TapOthers += n
	case "PayLife":
		c.PayLife += n
	case "Reveal", "RevealOrChoose":
		c.Reveal = true
	case "AddCounter":
		c.AddCounter = true
	default:
		if strings.ContainsRune(tok, '<') {
			c.Other = true
			return
		}
		if v, err := strconv.Atoi(tok); err == nil {
			c.Mana += v
			return
		}
		// coloured, hybrid or phyrexian symbol
		c.Mana++
	}
}

// targetClass reduces a target filter to this package's vocabulary.
func targetClass(v string) string {
	switch {
	case v == "Any":
		return "any"
	case strings.HasPrefix(v, "Creature.OppCtrl"):
		return "opp_creature"
	case strings.HasPrefix(v, "Creature"):
		return "creature"
	case v == "Player":
		return "player"
	case v == "Opponent":
		return "opponent"
	case strings.HasPrefix(v, "Permanent"):
		return "permanent"
	case strings.HasPrefix(v, "Artifact"), strings.HasPrefix(v, "Enchantment"):
		return "artifact_or_enchantment"
	case strings.HasPrefix(v, "Card"), strings.HasPrefix(v, "Spell"):
		return "spell"
	}
	return "other"
}
