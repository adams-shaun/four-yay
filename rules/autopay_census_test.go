package rules

// Auto-pay mana census (auto-pay audit, goal 4a).
//
// TestAutopayManaCensus enumerates every mana-producing ability shape in the
// compiled corpus -- cards AND token scripts -- and asks the engine's own code
// about each one on a synthetic board:
//
//   - V1 planner eligibility is ground truth from the real planner: membership
//     of the ability in paymentPlanUnitAlternatives over paymentPlanManaUnits,
//     an end-to-end PlanCastPayment for a {1} probe instant with the candidate
//     as the only seat-0 source, and paymentPlanManaInterference. The reason
//     column names the first planner predicate (in the planner's own order)
//     that withholds the ability, computed by calling those same predicates.
//   - Manual support is ground truth from the ordinary priority path: the
//     "activate for mana" option is submitted through Submit, any follow-up
//     decision is answered, and the log is read for the ManaAdd it produced.
//   - Playability is the `make report` registry: Registry.Unsupported against
//     effects.Supported().
//
// It never runs in the ordinary suite: set GORGE_AUTOPAY_CENSUS to an output
// directory. It writes census.csv (one row per ability), classes.md (class
// table), staples.md, findings.md (admissions the V1 contract excludes),
// killswitch.csv/.md (cards whose presence anywhere declines every plan),
// deckimpact.md (per repo deck) and summary.txt there. No card script text
// is written anywhere -- rows carry card names and shape classes only.
//
//	GORGE_AUTOPAY_CENSUS=/some/dir go test ./rules -run TestAutopayManaCensus -v

import (
	"encoding/csv"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/rules/pay"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules/chars"
	"github.com/adams-shaun/gorge/state"
)

const censusEnv = "GORGE_AUTOPAY_CENSUS"

// censusBenignParams are mana-ability parameters that carry presentation or
// AI hints only. Every other key on a mana ability is reported in the
// params_extra column so riders the planner does not read become visible.
var censusBenignParams = map[string]bool{
	"Cost": true, "Produced": true, "Amount": true, "SpellDescription": true,
	"StackDescription": true, "SubAbility": true, "AILogic": true, "PrecostDesc": true,
	"CostDesc": true, "AmountDesc": true, "ActivationZone": true, "AIPreference": true,
	"AINoRecursiveCheck": true, "Planeswalker": true, "Ultimate": true, "AnnounceType": true,
}

// censusSpecialProduction are the effMana parameters that change what the
// produced mana does or who gets it (effects/misc.go effMana).
var censusSpecialProduction = []string{"RestrictValid", "AddsNoCounter", "PersistentMana",
	"TriggersWhenSpent", "AddsCounters", "Defined", "UnlessCost"}

var censusBasicTypes = []string{"Plains", "Island", "Swamp", "Mountain", "Forest"}

type censusItem struct {
	kind   string // card | token
	key    string // card name, or token script stem
	card   *cards.Card
	face   int
	family string // activated | intrinsic | loyalty | spell | embedded | triggered | replacement | static_grant | land_type_grant | svar_other
	ident  string
	sa     *cards.SA
	trig   *cards.Trigger
	repl   *cards.Repl
	st     *cards.Static
	via    string
}

type censusRow struct {
	item                                                  censusItem
	api, zone, costClass, costParts, prodClass, amount    string
	restricted, riders, paramsExtra, conditions, selfIntf string
	class                                                 string
	v1Eligible, v1Reason, v1Window, v1E2E, v1Sick         string
	v1Structural                                          string
	presenceIntf                                          string
	manualOffered, manualOutcome, manualDetail            string
	playable, missing                                     string
	decks                                                 []string
	creators                                              int
	creatorDecks                                          []string
	creature                                              bool
	noUntap                                               bool
}

type autopayCensus struct {
	t         *testing.T
	r         *cards.Registry
	supported map[string]bool
	base      *Engine
	probe     state.ObjID
	imprint   state.ObjID
	legend    state.ObjID
	decks     map[*cards.Card][]string
	tokenRefs map[string]map[*cards.Card]bool
	support   map[string]*cards.Card
	// fodder, set only by TestCreatureManaAbilityAudit, arms every audit
	// board with the cost fodder the creature mana rows' costs name (token
	// permanents, a tapped source for a {Q} cost). The corpus-wide env-gated
	// census (TestAutopayManaCensus) leaves it false and its boards are
	// untouched.
	fodder bool
}

func censusChainHasMana(sa *cards.SA) bool {
	for d := 0; sa != nil && d < 32; d, sa = d+1, sa.Sub {
		if cards.IsManaAbilityAPI(sa.API) {
			return true
		}
	}
	return false
}

func censusRepoRoot(t *testing.T) string {
	cmd := exec.Command("git", "-C", ".", "rev-parse", "--show-toplevel")
	cmd.Env = cards.GitEnv()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("census: resolve repo root: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func TestAutopayManaCensus(t *testing.T) {
	outDir := os.Getenv(censusEnv)
	if outDir == "" {
		t.Skip("set " + censusEnv + "=<dir> to run the auto-pay mana census")
	}
	// A census that silently skips is worse than none: with the env set, a
	// missing corpus is a failure, not testutil.CorpusRegistry's Skip.
	if _, err := os.Stat(filepath.Join(censusRepoRoot(t), ".cards")); err != nil {
		t.Fatalf("census: no .cards corpus in this checkout (%v); run scripts/agent-worktree.sh or make fetch-cards", err)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cz := newAutopayCensus(t)
	var rows []censusRow
	for _, c := range cz.r.Cards {
		if !censusNamed(c) {
			continue
		}
		rows = append(rows, cz.evalCard("card", c.Faces[0].Name, c)...)
	}
	stems := make([]string, 0, len(cz.r.Tokens))
	for stem := range cz.r.Tokens {
		stems = append(stems, stem)
	}
	sort.Strings(stems)
	for _, stem := range stems {
		rows = append(rows, cz.evalCard("token", stem, cz.r.Tokens[stem])...)
	}
	if len(rows) < 1000 {
		t.Fatalf("census: only %d mana-producing abilities found; corpus not loaded?", len(rows))
	}
	cz.writeCSV(filepath.Join(outDir, "census.csv"), rows)
	cz.writeClasses(filepath.Join(outDir, "classes.md"), rows)
	cz.writeStaples(filepath.Join(outDir, "staples.md"), rows)
	cz.writeFindings(filepath.Join(outDir, "findings.md"), rows)
	cz.writeSummary(filepath.Join(outDir, "summary.txt"), rows)
	cz.writeKillSwitches(filepath.Join(outDir, "killswitch.csv"), filepath.Join(outDir, "killswitch.md"))
	cz.writeDeckImpact(filepath.Join(outDir, "deckimpact.md"), rows)
	t.Logf("census: %d ability rows written to %s", len(rows), outDir)
}

func censusNamed(c *cards.Card) bool {
	for _, f := range c.Faces {
		if f != nil && f.Name != "" {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// setup

func newAutopayCensus(t *testing.T) *autopayCensus {
	r := testutil.CorpusRegistry(t)
	cz := &autopayCensus{t: t, r: r, supported: effects.Supported(),
		decks: map[*cards.Card][]string{}, tokenRefs: map[string]map[*cards.Card]bool{},
		support: map[string]*cards.Card{}}
	cz.loadDecks()
	cz.loadTokenRefs()
	// Synthetic support permanents: none of them has a mana ability, so the
	// candidate stays the only seat-0 mana source on the V1 board.
	for name, src := range map[string]string{
		"relic":  "Name:Census Relic\nManaCost:1\nTypes:Artifact\nOracle:x\n",
		"charm":  "Name:Census Charm\nManaCost:1\nTypes:Enchantment\nOracle:x\n",
		"elf":    "Name:Census Elf\nManaCost:G G\nTypes:Creature Elf Druid Warrior\nPT:1/1\nOracle:x\n",
		"legend": "Name:Census Legend\nManaCost:W U B R G\nTypes:Legendary Creature Human\nPT:2/2\nOracle:x\n",
		"land":   "Name:Census Land\nTypes:Land\nOracle:x\n",
		"bear":   "Name:Census Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n",
		"inst":   "Name:Census Instant\nManaCost:U\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n",
		"sorc":   "Name:Census Sorcery\nManaCost:B\nTypes:Sorcery\nA:SP$ Draw | Num$ 1\nOracle:x\n",
		"imprt":  "Name:Census Imprint\nManaCost:R\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n",
		"forest": "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n",
		"island": "Name:Island\nTypes:Basic Land Island\nOracle:x\n",
		"plains": "Name:Plains\nTypes:Basic Land Plains\nOracle:x\n",
		"swamp":  "Name:Swamp\nTypes:Basic Land Swamp\nOracle:x\n",
		"mount":  "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n",
		"dork":   "Name:Census Dork\nManaCost:G\nTypes:Creature Elf\nPT:1/1\nA:AB$ Mana | Cost$ T | Produced$ G | SpellDescription$ Add {G}.\nOracle:x\n",
		"rock":   "Name:Census Rock\nManaCost:1\nTypes:Artifact\nA:AB$ Mana | Cost$ T | Produced$ C | SpellDescription$ Add {C}.\nOracle:x\n",
		"probe":  "Name:Census Probe\nManaCost:1\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n",
		// Fodder supports: placed only by the audit's fodder hook, never on the
		// base board. None of them has a mana ability, a trigger or a static.
		"food":      "Name:Census Food\nTypes:Artifact Food\nOracle:x\n",
		"goblin":    "Name:Census Goblin\nManaCost:R\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n",
		"spirit":    "Name:Census Spirit\nManaCost:W\nTypes:Creature Spirit\nPT:1/1\nOracle:x\n",
		"saproling": "Name:Census Saproling\nManaCost:G\nTypes:Creature Saproling\nPT:1/1\nOracle:x\n",
		"wood":      "Name:Wood\nTypes:Creature Wall\nPT:0/1\nOracle:x\n",
	} {
		cz.support[name] = card(t, src)
	}
	cz.buildBase()
	return cz
}

func (cz *autopayCensus) loadDecks() {
	for _, name := range testutil.RepoDeckNames() {
		f := testutil.RepoDeckFile(cz.t, name)
		seen := map[*cards.Card]bool{}
		add := func(cs []*cards.Card) {
			for _, c := range cs {
				if c != nil && !seen[c] {
					seen[c] = true
					cz.decks[c] = append(cz.decks[c], name)
				}
			}
		}
		main, err := f.Resolve(cz.r)
		if err != nil {
			cz.t.Fatalf("census: deck %s: %v", name, err)
		}
		add(main)
		side, err := f.ResolveSideboard(cz.r)
		if err != nil {
			cz.t.Fatalf("census: deck %s sideboard: %v", name, err)
		}
		add(side)
		for _, cn := range f.CommanderNames() {
			if c, ok := cz.r.Lookup(cn); ok {
				add([]*cards.Card{c})
			}
		}
	}
}

// loadTokenRefs records, per token script stem, the corpus cards whose face
// parameters or SVars name it -- the token's creators.
func (cz *autopayCensus) loadTokenRefs() {
	split := func(r rune) bool {
		return !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	}
	for _, c := range cz.r.Cards {
		note := func(s string) {
			for _, w := range strings.FieldsFunc(s, split) {
				if _, ok := cz.r.Tokens[w]; ok {
					if cz.tokenRefs[w] == nil {
						cz.tokenRefs[w] = map[*cards.Card]bool{}
					}
					cz.tokenRefs[w][c] = true
				}
			}
		}
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			for _, a := range f.Abilities {
				for sa, d := a, 0; sa != nil && d < 32; sa, d = sa.Sub, d+1 {
					for _, v := range sa.Params {
						note(v)
					}
				}
			}
			for _, v := range f.SVars {
				note(v)
			}
			for _, tr := range f.Triggers {
				for _, v := range tr.Params {
					note(v)
				}
			}
			for _, st := range f.Statics {
				for _, v := range st.Params {
					note(v)
				}
			}
			for _, rp := range f.Repls {
				for _, v := range rp.Params {
					note(v)
				}
			}
		}
	}
}

// buildBase is the shared starting board every census board clones: seat 0
// holds priority in turn 1's precombat main phase with the {1} probe instant
// in hand, three
// vanilla artifacts, two vanilla enchantments and three vanilla creatures on
// the battlefield (metalcraft, devotion, Elf and legendary counts), eight
// assorted cards in the graveyard (threshold, delirium), a red creature card
// in exile for imprint, and seat 1 controls one of each basic land (reflected
// production). No seat-0 support permanent has a mana ability.
func (cz *autopayCensus) buildBase() {
	t := cz.t
	probe := cz.support["probe"]
	deck0 := []*cards.Card{probe}
	for len(deck0) < 40 {
		deck0 = append(deck0, cz.support["mount"])
	}
	deck1 := make([]*cards.Card, 40)
	for i := range deck1 {
		deck1[i] = cz.support["mount"]
	}
	cfg := seatZeroStart(Config{Seed: 4242, Names: []string{"a", "b"}, Decks: [][]*cards.Card{deck0, deck1},
		Tokens: cz.r.Tokens})
	e := New(cfg)
	e.Advance()
	var probeID state.ObjID
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 0) {
			if e.G.Obj(id).Face().Name == "Census Probe" {
				probeID = id
				if z == state.ZLibrary {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
					e.pending = nil
					e.Advance()
				}
				break
			}
		}
		if probeID != 0 {
			break
		}
	}
	if probeID == 0 {
		t.Fatal("census: probe instant not found")
	}
	// Sorcery-speed casts (rituals) need seat 0's precombat main phase.
	driveToStep(t, e, 1, 0, state.StepMain1)
	// The layer-4 land-type grant path is armed by the corpus vocabulary, the
	// way a universe-backed match arms it.
	e.landTypeWords = chars.CorpusLandTypeWords(cz.r.Cards)
	for _, k := range []string{"relic", "relic", "relic", "charm", "charm", "elf", "elf", "legend"} {
		id := censusPlace(e, cz.support[k], 0, state.ZBattlefield, 0)
		if k == "legend" {
			cz.legend = id
		}
	}
	for _, k := range []string{"land", "elf", "inst", "sorc", "relic", "charm", "bear", "inst"} {
		censusPlace(e, cz.support[k], 0, state.ZGraveyard, 0)
	}
	cz.imprint = censusPlace(e, cz.support["imprt"], 0, state.ZExile, 0)
	for _, k := range []string{"plains", "island", "swamp", "mount", "forest", "bear"} {
		censusPlace(e, cz.support[k], 1, state.ZBattlefield, 0)
	}
	cz.base = e
	cz.probe = probeID
}

// censusPlace puts a fresh object for c into zone z eventlessly, untapped and
// not summoning sick, and stales every derived memo the way onBoard does.
func censusPlace(e *Engine, c *cards.Card, p state.PlayerID, z state.Zone, face int) state.ObjID {
	o := e.G.AddObject(c, p)
	id := o.ID
	o.Zone = z
	o.SetFaceIdx(uint8(face))
	o.SummonSick = false
	e.G.Clock++
	o.Timestamp = e.G.Clock
	e.G.SetZone(z, p, append(e.G.Zone(z, p), id))
	censusStale(e)
	return id
}

func censusStale(e *Engine) {
	e.staticEpoch = -1
	e.activeEpoch = -1
	e.typesEpoch = -1
	if e.layer4InPool {
		e.refreshDerivedTypes()
	}
}

// board clones the base and arms the pool-level probes New would have armed
// had the candidate been in a deck.
func (cz *autopayCensus) board(c *cards.Card) *Engine {
	e := cz.base.Clone()
	pool := Config{Decks: [][]*cards.Card{{c}}}
	if poolHasLayer4Static(pool) {
		e.layer4InPool = true
	}
	if poolHasSetNameStatic(pool) {
		e.setNameInPool = true
	}
	return e
}

// addFodder arms the manual board with the cost fodder the creature mana
// audit's rows commonly name (the audit's own cz.fodder opt-in): token
// permanents -- two Food, a Goblin, a Spirit, a Saproling and the token named
// Wood, exactly the sacrifice/tap filters those costs read -- and, when the
// probed ability's cost carries an untap component, the source itself tapped
// (a {Q} cost is unpayable on an untapped source). Everything is in place
// BEFORE askPriority offers the activation. Every object is written
// eventlessly under the census's own censusStale precedent -- the fodder
// board is a synthetic per-row clone that is never replayed, and an eventless
// write keeps every logged event out of the tally.
func (cz *autopayCensus) addFodder(e *Engine, id state.ObjID, ma *cards.SA) {
	if ma == nil {
		return
	}
	c := e.parseCost(ma.Params["Cost"])
	need := map[string]bool{}
	for _, part := range c.Sac {
		if part.Announced {
			continue
		}
		fodderNeed(part.Spec, need)
	}
	for _, part := range c.TapPermanent {
		if part.Dyn != "" {
			continue
		}
		fodderNeed(part.Spec, need)
	}
	for _, stem := range fodderStems {
		if !need[stem.key] {
			continue
		}
		// Two of each: a tapXType<2/...> part taps two, a Sac<1/...> part
		// sacrifices one -- two covers every literal count the audit's rows
		// name.
		for i := 0; i < 2; i++ {
			fid := censusPlace(e, cz.support[stem.key], 0, state.ZBattlefield, 0)
			e.G.Obj(fid).IsToken = stem.tok
		}
	}
	// {Q}: the untap cost needs the source already tapped at offer time
	// (activationTapCostUnavailable refuses an untapped {Q} source).
	if c.Untap {
		if o := e.G.Obj(id); o != nil {
			o.Tapped = true
		}
	}
	// untapYType<N/Spec> (Benthic Explorers): the cost untaps a TAPPED
	// matching permanent, so the board needs one. A Land.OppCtrl spec needs
	// it on an opponent's battlefield; any other spec gets it for the payer.
	// A basic Island matches the corpus's Land specs and produces every
	// colour, so a ManaReflected read of it is non-empty.
	for _, part := range c.UntapPermanent {
		seat := state.PlayerID(0)
		if strings.Contains(part.Spec, "OppCtrl") {
			seat = 1
		}
		fid := censusPlace(e, cz.support["island"], seat, state.ZBattlefield, 0)
		if o := e.G.Obj(fid); o != nil {
			o.Tapped = true
		}
	}
	censusStale(e)
}

// fodderNeed marks which fodder a cost spec needs. Only exact, well-known
// fragments are matched ("namedWood" first, so a Wood filter is not mistaken
// for a generic token filter). A spec that names none of the fragments gets
// no fodder at all -- fail-closed, like every other filter read.
func fodderNeed(spec string, need map[string]bool) {
	switch {
	case strings.Contains(spec, "namedWood"):
		need["wood"] = true
	case strings.Contains(spec, "Saproling"):
		need["saproling"] = true
	case strings.Contains(spec, "Spirit"):
		need["spirit"] = true
	case strings.Contains(spec, "Goblin"):
		need["goblin"] = true
	case strings.Contains(spec, "Food"):
		need["food"] = true
	case strings.Contains(spec, ".token"):
		need["food"] = true // a token is a token: two Foods satisfy tapXType<2/Permanent.token>
	}
}

// fodderStems is the audit's fodder table: which synthetic support card each
// cost spec needs, and whether that support card is marked as a token.
var fodderStems = []struct {
	key  string
	frag string
	tok  bool
}{
	{"food", "Food", true},
	{"goblin", "Goblin", false},
	{"spirit", "Spirit", false},
	{"saproling", "Saproling", false},
	{"wood", "Wood", true},
}

// enrich turns a V1 board into the manual board: floating mana of every type,
// counters for every counter-removal cost, a recorded colour/type choice and
// an imprinted red card.
func (cz *autopayCensus) enrich(e *Engine, id state.ObjID) {
	pl := &e.G.Players[0]
	for i := range pl.Pool {
		pl.Pool[i] += 3
	}
	pl.AddCounter("ENERGY", 6)
	// A WUBRG commander designation, so a colour-identity producer (Command
	// Tower, Arcane Signet) has an identity to read (CR 903.4).
	pl.Commanders = []state.ObjID{cz.legend}
	// One of each basic land for seat 0: the land counts, reflected-land
	// candidates and IsPresent gates a manual activation commonly reads.
	for _, k := range []string{"plains", "island", "swamp", "mount", "forest"} {
		censusPlace(e, cz.support[k], 0, state.ZBattlefield, 0)
	}
	o := e.G.Obj(id)
	if o == nil {
		return
	}
	o.ChosenColor = "G"
	o.ChosenType = "Elf"
	o.Imprinted = []state.ObjID{cz.imprint}
	o.AddCounter("CHARGE", 3)
	if f := o.Face(); f != nil {
		for _, a := range f.Abilities {
			for _, part := range e.parseCost(a.Params["Cost"]).SubCounter {
				if part.Spec != "" && o.Counter(part.Spec) < part.N*2+2 {
					o.AddCounter(part.Spec, part.N*2+2)
				}
			}
		}
	}
	censusStale(e)
}

// ---------------------------------------------------------------------------
// enumeration

func censusItems(kind, key string, c *cards.Card) []censusItem {
	var out []censusItem
	for fi, f := range c.Faces {
		if f == nil {
			continue
		}
		reached := map[string]bool{}
		markChain := func(sa *cards.SA) {
			for d := 0; sa != nil && d < 32; d, sa = d+1, sa.Sub {
				if n := strings.TrimSpace(sa.Params["SubAbility"]); n != "" {
					reached[n] = true
				}
			}
		}
		base := censusItem{kind: kind, key: key, card: c, face: fi}
		for ai, a := range f.Abilities {
			it := base
			it.ident, it.sa = fmt.Sprintf("A%d", ai), a
			switch {
			case a.Kind == "AB" && cards.IsManaAbilityAPI(a.API):
				it.family = "activated"
				if strings.HasPrefix(a.Line, "intrinsic:") {
					it.family = "intrinsic"
				} else if isLoyaltyAbilityCost(a, ParseCost(a.Params["Cost"])) {
					it.family = "loyalty"
				}
			case a.Kind == "SP" && censusChainHasMana(a):
				it.family = "spell"
			case censusChainHasMana(a):
				it.family = "embedded"
			default:
				continue
			}
			markChain(a)
			out = append(out, it)
		}
		for ti := range f.Triggers {
			tr := &f.Triggers[ti]
			if n := strings.TrimSpace(tr.Params["Execute"]); n != "" && censusChainHasMana(tr.Effect) {
				reached[n] = true
				markChain(tr.Effect)
				it := base
				it.ident, it.family, it.trig, it.sa = fmt.Sprintf("T%d", ti), "triggered", tr, tr.Effect
				out = append(out, it)
			}
		}
		for ri := range f.Repls {
			rp := &f.Repls[ri]
			if rp.Event == "ProduceMana" || censusChainHasMana(rp.With) {
				if n := strings.TrimSpace(rp.Params["ReplaceWith"]); n != "" {
					reached[n] = true
				}
				markChain(rp.With)
				it := base
				it.ident, it.family, it.repl, it.sa = fmt.Sprintf("R%d", ri), "replacement", rp, rp.With
				out = append(out, it)
			}
		}
		for si := range f.Statics {
			st := &f.Statics[si]
			if st.Mode != "Continuous" {
				continue
			}
			for _, key := range []string{"AddAbility", "AddAbilities"} {
				for _, n := range strings.Split(st.Params[key], "&") {
					n = strings.TrimSpace(n)
					if n == "" {
						continue
					}
					if sa := cards.ResolveSVar(f.SVars, n); sa != nil && censusChainHasMana(sa) {
						reached[n] = true
						markChain(sa)
						it := base
						it.ident, it.family, it.st, it.sa, it.via = fmt.Sprintf("S%d:%s", si, n), "static_grant", st, sa, key
						out = append(out, it)
					}
				}
			}
			if censusGrantsBasicType(st) {
				it := base
				it.ident, it.family, it.st = fmt.Sprintf("S%d", si), "land_type_grant", st
				out = append(out, it)
			}
		}
		// Residual: a Mana-API SVar no enumerated chain reaches (a Charm
		// choice, an Animate grant, a delayed trigger's Execute, ...).
		names := make([]string, 0, len(f.SVars))
		for n := range f.SVars {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			if reached[n] {
				continue
			}
			sa := cards.ResolveSVar(f.SVars, n)
			if sa == nil || !cards.IsManaAbilityAPI(sa.API) {
				continue
			}
			it := base
			it.ident, it.family, it.sa, it.via = "SV:"+n, "svar_other", sa, censusSVarVia(f, n)
			out = append(out, it)
		}
	}
	return out
}

// censusGrantsBasicType reports a Continuous static that adds a basic land
// type (and with it the CR 305.6 intrinsic mana ability) to permanents.
func censusGrantsBasicType(st *cards.Static) bool {
	for _, key := range []string{"AddType", "AddTypes"} {
		for _, w := range strings.FieldsFunc(st.Params[key], func(r rune) bool { return r == '&' || r == ',' || r == ' ' }) {
			if strings.EqualFold(w, "AllBasicLandType") {
				return true
			}
			for _, b := range censusBasicTypes {
				if strings.EqualFold(w, b) {
					return true
				}
			}
		}
	}
	return false
}

// censusSVarVia names the parameter keys that reference SVar n on face f.
func censusSVarVia(f *cards.Face, n string) string {
	keys := map[string]bool{}
	scan := func(params map[string]string) {
		for k, v := range params {
			for _, w := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == '&' || r == ' ' }) {
				if w == n {
					keys[k] = true
				}
			}
		}
	}
	for _, a := range f.Abilities {
		for sa, d := a, 0; sa != nil && d < 32; sa, d = sa.Sub, d+1 {
			scan(sa.Params)
		}
	}
	for _, tr := range f.Triggers {
		scan(tr.Params)
	}
	for _, rp := range f.Repls {
		scan(rp.Params)
	}
	for _, st := range f.Statics {
		scan(st.Params)
	}
	for m, body := range f.SVars {
		if m == n {
			continue
		}
		// An SVar may hold a trigger or static line (an AddTrigger$ /
		// Triggers$ body) that is not an SA; read its keys textually.
		for _, part := range strings.Split(body, "|") {
			k, v, ok := strings.Cut(strings.TrimSpace(part), "$")
			if !ok {
				continue
			}
			for _, w := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == '&' || r == ' ' }) {
				if w == n {
					keys["SVar:"+strings.TrimSpace(k)] = true
				}
			}
		}
	}
	for _, k := range f.Keywords {
		if strings.Contains(k, n) {
			keys["K"] = true
		}
	}
	out := make([]string, 0, len(keys))
	for k := range keys {
		out = append(out, k)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return "unreferenced"
	}
	return strings.Join(out, "+")
}

// ---------------------------------------------------------------------------
// shape classification

func censusCostParts(e *Engine, raw string) (tap bool, parts []string) {
	c := e.parseCost(raw)
	for _, tok := range strings.Fields(raw) {
		if tok == "Q" {
			parts = append(parts, "untap_Q")
		}
	}
	if c.Generic > 0 || c.Colored != (state.Mana{}) || c.X > 0 || c.Snow > 0 || len(c.Hybrid) > 0 ||
		len(c.Phyrexian) > 0 || len(c.Twobrid) > 0 || len(c.HybridPhyrexian) > 0 {
		parts = append(parts, "mana")
	}
	if c.Life > 0 || len(c.LifeX) > 0 || c.LifeHalfUp {
		parts = append(parts, "life")
	}
	for _, s := range c.Sac {
		if strings.EqualFold(pay.SacrificeMatchSpec(s.Spec), "CARDNAME") || strings.EqualFold(s.Spec, "Self") {
			parts = append(parts, "sac_self")
		} else {
			parts = append(parts, "sac_other")
		}
	}
	if len(c.Discard) > 0 {
		parts = append(parts, "discard")
	}
	for _, x := range c.Exile {
		switch x.Zone {
		case state.ZGraveyard:
			parts = append(parts, "exile_grave")
		case state.ZHand:
			parts = append(parts, "exile_hand")
		default:
			parts = append(parts, "exile_other")
		}
	}
	if len(c.SubCounter) > 0 {
		parts = append(parts, "subcounter")
	}
	if len(c.AddCounter) > 0 {
		parts = append(parts, "addcounter")
	}
	if len(c.TapPermanent) > 0 {
		parts = append(parts, "tap_other")
	}
	if len(c.Return) > 0 {
		parts = append(parts, "return")
	}
	if len(c.Mill) > 0 {
		parts = append(parts, "mill")
	}
	if len(c.Energy) > 0 {
		parts = append(parts, "energy")
	}
	if len(c.Exert) > 0 {
		parts = append(parts, "exert")
	}
	if len(c.DamageYou) > 0 {
		parts = append(parts, "damage")
	}
	if len(c.Reveal) > 0 || len(c.RevealOrChoose) > 0 || len(c.RevealChosen) > 0 || len(c.Behold) > 0 {
		parts = append(parts, "reveal")
	}
	if len(c.Blight) > 0 || c.Forage || len(c.Draw) > 0 || len(c.PutToLib) > 0 || len(c.MoveToGrave) > 0 ||
		len(c.Evidence) > 0 || len(c.RollDice) > 0 || len(c.ExileFromTop) > 0 {
		parts = append(parts, "other")
	}
	for _, u := range c.Unknown {
		if u != "Q" {
			parts = append(parts, "unknown:"+u)
		}
	}
	return c.Tap, parts
}

var censusCostPriority = []string{"sac_self", "sac_other", "discard", "exile_hand", "exile_grave", "exile_other",
	"life", "subcounter", "addcounter", "mana", "tap_other", "untap_Q", "return", "mill", "energy", "exert",
	"damage", "reveal", "other"}

func censusCostClass(tap bool, parts []string) string {
	if len(parts) == 0 {
		if tap {
			return "T"
		}
		return "none"
	}
	primary := parts[0]
	for _, p := range censusCostPriority {
		if censusHas(parts, p) {
			primary = p
			break
		}
	}
	if tap {
		return "T+" + primary
	}
	return primary
}

func censusHas(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func censusProdClass(sa *cards.SA) (class, amount string) {
	amount = strings.TrimSpace(sa.Params["Amount"])
	if amount == "" {
		amount = "1"
	}
	if _, err := strconv.Atoi(amount); err != nil {
		amount = "dynamic"
	}
	if sa.API == "ManaReflected" {
		v := sa.Params["Valid"]
		switch {
		case strings.Contains(v, "Imprinted"):
			return "reflected_imprint", amount
		case strings.Contains(v, "Remembered") || strings.Contains(v, "Sacrificed") || strings.Contains(v, "Exiled"):
			return "reflected_remembered", amount
		}
		return "reflected", amount
	}
	p := strings.TrimSpace(sa.Params["Produced"])
	switch {
	case p == "":
		return "blank", amount
	case p == "Any":
		return "any", amount
	case p == "Combo Any":
		return "combo_any", amount
	case strings.Contains(p, "ColorIdentity"):
		return "colour_identity", amount
	case strings.Contains(p, "Chosen"):
		return "chosen", amount
	case strings.HasPrefix(p, "Special"):
		return "special", amount
	case strings.HasPrefix(p, "Combo "):
		return "choice_combo", amount
	}
	counts, any := cards.ProducedCounts(p)
	if any {
		return "unparsed", amount
	}
	total := int32(0)
	for _, n := range counts {
		total += n
	}
	n, _ := strconv.Atoi(amount)
	if total == 1 && (amount == "dynamic" || n <= 1) {
		return "fixed_single", amount
	}
	return "fixed_multi", amount
}

func censusRiders(sa *cards.SA) (riders, extra, conds []string) {
	for s, d := sa.Sub, 0; s != nil && d < 32; s, d = s.Sub, d+1 {
		riders = append(riders, "sub:"+s.API)
	}
	for k := range sa.Params {
		if censusBenignParams[k] {
			continue
		}
		extra = append(extra, k)
		switch k {
		case "Activation", "IsPresent", "PresentCompare", "ActivationPhases", "PlayerTurn", "OpponentTurn",
			"ActivationLimit", "GameActivationLimit", "Activator", "InstantSpeed", "SorcerySpeed", "CheckSVar",
			"SVarCompare", "ClassLevel", "Condition":
			conds = append(conds, k)
		}
	}
	for _, k := range censusSpecialProduction {
		if _, ok := sa.Params[k]; ok {
			riders = append(riders, k)
		}
	}
	sort.Strings(extra)
	sort.Strings(conds)
	return riders, extra, conds
}

func censusSelfInterference(f *cards.Face) bool {
	for _, t := range f.Triggers {
		if t.Mode == "Taps" || t.Mode == "TapsForMana" {
			return true
		}
	}
	for _, r := range f.Repls {
		ev := strings.ToLower(r.Event)
		if strings.Contains(ev, "mana") || strings.Contains(ev, "tap") {
			return true
		}
	}
	return false
}

// censusNoUntap reports a self "doesn't untap during your untap step"
// drawback (Forge writes it as an Untap replacement on the card itself).
func censusNoUntap(f *cards.Face) bool {
	for _, r := range f.Repls {
		if r.Event == "Untap" && strings.Contains(r.Params["ValidCard"], "Self") {
			return true
		}
	}
	return false
}

func (cz *autopayCensus) classify(row *censusRow) {
	it := row.item
	switch it.family {
	case "intrinsic":
		row.class = "intrinsic:basic_land_type"
	case "loyalty":
		row.class = "loyalty (not a mana ability)"
	case "spell":
		if it.sa.API == "Mana" || it.sa.API == "ManaReflected" {
			row.class = "spell:ritual"
		} else {
			row.class = "spell:mana_rider(" + it.sa.API + ")"
		}
	case "embedded":
		row.class = "embedded:" + it.sa.Kind + "(" + it.sa.API + ")"
	case "triggered":
		row.class = "trigger:" + it.trig.Mode
	case "replacement":
		row.class = "replacement:" + it.repl.Event
	case "static_grant":
		row.class = "grant:AddAbility(" + row.prodClass + ")"
	case "land_type_grant":
		row.class = "grant:basic_land_type"
	case "svar_other":
		row.class = "svar:" + it.via
	case "activated":
		switch {
		case row.zone != "Battlefield":
			row.class = "zone:" + row.zone + "/" + row.costClass
		case row.costClass != "T":
			row.class = "cost:" + row.costClass
		case row.restricted == "Y":
			row.class = "prod:restricted"
		case strings.HasPrefix(row.prodClass, "reflected"):
			row.class = "prod:" + row.prodClass
		case row.amount == "dynamic":
			row.class = "prod:dynamic_amount"
		default:
			row.class = "prod:" + row.prodClass
		}
	}
}

// ---------------------------------------------------------------------------
// evaluation

func (cz *autopayCensus) evalCard(kind, key string, c *cards.Card) (rows []censusRow) {
	items := censusItems(kind, key, c)
	if len(items) == 0 {
		return nil
	}
	var missing []string
	if kind == "card" || kind == "token" {
		missing = cz.r.Unsupported(c, cz.supported)
	}
	var creators int
	var creatorDecks []string
	if kind == "token" {
		seen := map[string]bool{}
		for cc := range cz.tokenRefs[key] {
			creators++
			for _, d := range cz.decks[cc] {
				seen[d] = true
			}
		}
		for d := range seen {
			creatorDecks = append(creatorDecks, d)
		}
		sort.Strings(creatorDecks)
	}
	for _, it := range items {
		row := censusRow{item: it, decks: cz.decks[c], creators: creators, creatorDecks: creatorDecks}
		row.playable = "Y"
		if len(missing) > 0 {
			row.playable = "N"
			row.missing = strings.Join(missing, " ")
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					row.manualOutcome = "panic"
					row.manualDetail = strings.ReplaceAll(fmt.Sprint(r), ",", ";")
					if len(row.manualDetail) > 160 {
						row.manualDetail = row.manualDetail[:160]
					}
				}
			}()
			cz.evalItem(&row)
		}()
		cz.classify(&row)
		rows = append(rows, row)
	}
	return rows
}

func (cz *autopayCensus) evalItem(row *censusRow) {
	it := row.item
	f := it.card.Faces[it.face]
	row.selfIntf = yn(censusSelfInterference(f))
	row.noUntap = censusNoUntap(f)
	// Printed-type creature flag, a fallback for every family; evalActivated
	// overwrites it with the layer-4-derived types on the probe board.
	row.creature = f != nil && censusHas(f.Types, "Creature")
	if it.sa != nil {
		row.api = it.sa.API
		row.prodClass, row.amount = censusProdClass(it.sa)
		riders, extra, conds := censusRiders(it.sa)
		row.riders = strings.Join(riders, " ")
		row.paramsExtra = strings.Join(extra, " ")
		row.conditions = strings.Join(conds, " ")
		row.restricted = yn(strings.TrimSpace(it.sa.Params["RestrictValid"]) != "")
		tap, parts := censusCostParts(cz.base, it.sa.Params["Cost"])
		row.costClass, row.costParts = censusCostClass(tap, parts), strings.Join(parts, " ")
		row.zone = "Battlefield"
		if z := strings.TrimSpace(it.sa.Params["ActivationZone"]); z != "" {
			row.zone = z
		}
	}
	switch it.family {
	case "activated", "intrinsic":
		cz.evalActivated(row)
	case "spell":
		row.zone = "cast"
		row.v1Eligible, row.v1Reason = "n/a", "not a source: a spell (V1 plans permanents' mana abilities only)"
		cz.evalSpell(row)
	case "triggered", "replacement":
		row.zone = "Battlefield"
		row.v1Eligible, row.v1Reason = "n/a", "modifies other sources"
		cz.evalModifier(row)
	case "static_grant", "land_type_grant":
		row.zone = "Battlefield"
		cz.evalGrant(row)
	case "loyalty":
		row.v1Eligible, row.v1Reason = "N", "loyalty ability: not on the mana path (CR 605.1b)"
		row.manualOutcome = "n/a (ability path)"
	default:
		row.v1Eligible, row.v1Reason = "n/a", "not a mana ability head"
		row.manualOutcome = "n/a"
	}
}

func yn(b bool) string {
	if b {
		return "Y"
	}
	return "N"
}

func censusZone(z string) (state.Zone, bool) {
	switch z {
	case "Battlefield":
		return state.ZBattlefield, true
	case "Hand":
		return state.ZHand, true
	case "Graveyard":
		return state.ZGraveyard, true
	case "Exile":
		return state.ZExile, true
	}
	return 0, false
}

// v1Member is the ground truth: the ability is one of the exact one-tap
// outcomes the real planner would search over for this source.
func (cz *autopayCensus) v1Member(e *Engine, id state.ObjID, ma *cards.SA) bool {
	ab, ok := pay.PaymentAbility(e.G, id, ma)
	if !ok {
		return false
	}
	var want decision.ManaAmount
	intrinsic := ab.Kind == decision.PaymentAbilityIntrinsic
	if intrinsic {
		counts, _ := cards.ProducedCounts(ma.Params["Produced"])
		var m state.Mana
		for i, n := range counts {
			m[state.ManaIndex(cards.ManaSymbol(i))] += n * availableAmount(ma)
		}
		want = pay.ManaAmount(m)
	}
	for _, u := range e.paymentPlanManaUnits(0) {
		if u.ID != id {
			continue
		}
		for _, a := range pay.PaymentPlanUnitAlternatives(asPayer(e), u) {
			if a.Activation.Ability == ab && (!intrinsic || a.Activation.Produces == want) {
				return true
			}
		}
	}
	return false
}

// v1Structural walks the planner's own predicates, in the planner's order,
// for the board-independent part of the V1 contract and names the first one
// that withholds the ability.
func (cz *autopayCensus) v1Structural(e *Engine, id state.ObjID, ma *cards.SA) string {
	if tier, _, detail := pay.PaymentPlanAbilityTier(asPayer(e), 0, id, ma); tier != pay.TierNormal {
		return "paymentPlanAbilityTier (" + detail + ")"
	}
	if strings.TrimSpace(ma.Params["RestrictValid"]) != "" {
		return "RestrictValid (windowManaUnits)"
	}
	cost := e.parseCost(ma.Params["Cost"])
	if !pay.ManaFreeCost(cost) {
		return "manaFreeCost (cost beyond tap)"
	}
	amt := availableAmount(ma)
	if amt <= 0 {
		return "availableAmount (non-literal Amount$)"
	}
	produced := strings.TrimSpace(ma.Params["Produced"])
	counts, any := cards.ProducedCounts(ma.Params["Produced"])
	total := int32(0)
	for _, n := range counts {
		total += n
	}
	viaWindow := (any && total == 1 && counts[5] == 1) || (!any && total > 0)
	viaAny := produced == "Any" && any
	// aph-combo-chosen-identity: the finite choice shapes are planned too --
	// an amount-1 Combo/Chosen/ColorIdentity (one alternative per resolvable
	// colour) alongside Produced$ Any. Combo Any and an allocation (amount >
	// 1) stay deferred. The mirror is the SHAPE gate only; whether the choice
	// resolves to a colour on a given board (recorded Chosen, commander
	// identity) is the planner's board-dependent answer, not this column's.
	viaChoice := any && produced != "Any" && pay.PaymentPlanChoiceShape(produced) && amt == 1
	if !viaWindow && !viaAny && !viaChoice {
		return "ProducedCounts (open production: only fixed or a finite choice)"
	}
	if !pay.PaymentPlanTapOnlyCost(cost) {
		return "paymentPlanTapOnlyCost (no {T} or extra part)"
	}
	if _, ok := pay.PaymentAbility(e.G, id, ma); !ok {
		return "paymentAbility (granted/merged/foreign)"
	}
	altOK := !any && ma.API == "Mana" && total > 0
	if !altOK && !viaAny && !viaChoice {
		if ma.API != "Mana" {
			return "paymentPlanAltOK (API " + ma.API + ")"
		}
		return "paymentPlanAltOK (open choice)"
	}
	return "ok"
}

// v1WindowGate names the board-dependent availableManaAbilitiesForWindow
// gate that withholds a structurally admissible ability on this board.
func (cz *autopayCensus) v1WindowGate(e *Engine, id state.ObjID, ma *cards.SA) string {
	o := e.G.Obj(id)
	switch {
	case o == nil:
		return "no object"
	case o.Tapped:
		return "tapped"
	case !abilityZoneOK(ma, o.Zone):
		return "zone"
	case e.isLoyaltyAbility(ma):
		return "loyalty"
	case !e.activatorAllows(0, id, ma):
		return "Activator$"
	case !e.activationConditionOK(0, ma):
		return "Activation$ " + ma.Params["Activation"]
	case ma.API == "Mana" && !pay.ManaActivationGateHolds(asPayer(e), 0, id, ma):
		return "IsPresent$/ActivationPhases$"
	case ma.API == "ManaReflected" && !e.manaReflectedPresentHolds(0, id, ma):
		return "IsPresent$ (reflected)"
	case e.abilityRestricted(0, id, ma):
		return "CantBeActivated"
	case !e.manaAbilityPayable(0, id, ma):
		return "manaAbilityPayable (cost unpayable on census board)"
	case pay.InstantSpeedOnly(ma):
		return "InstantSpeed$ (payment window withholds)"
	}
	return "other (activation limit / granted walk)"
}

func (cz *autopayCensus) evalActivated(row *censusRow) {
	it := row.item
	zone, ok := censusZone(row.zone)
	if !ok {
		row.v1Eligible, row.v1Reason, row.manualOutcome = "N", "zone "+row.zone+" (planner walks the battlefield only)", "n/a (zone)"
		return
	}
	// V1 board.
	e := cz.board(it.card)
	id := censusPlace(e, it.card, 0, zone, it.face)
	e.G.Obj(id).IsToken = it.kind == "token"
	ma := censusFaceSA(e, id, it)
	if ma == nil {
		row.v1Eligible, row.v1Reason = "N", "ability not on the placed face"
		return
	}
	row.creature = censusHas(e.Derived(id).Types, "Creature")
	member := cz.v1Member(e, id, ma)
	structural := cz.v1Structural(e, id, ma)
	if zone != state.ZBattlefield {
		structural = "zone " + row.zone + " (planner walks the battlefield only)"
	}
	row.v1Reason = structural
	row.v1Window = "ok"
	inWindow := censusContains(e.availableManaAbilitiesForWindow(0, id, false), ma)
	if !inWindow {
		row.v1Window = cz.v1WindowGate(e, id, ma)
	}
	intf := e.paymentPlanManaInterference()
	// Structural: the planner's own predicates admit the shape, so a board
	// where its activation gate holds (Activation$, IsPresent$, ...) and no
	// interference is live makes it a V1 source.
	row.v1Structural = yn(structural == "ok" && !intf && zone == state.ZBattlefield && !pay.InstantSpeedOnly(ma))
	switch {
	case member && intf:
		row.v1Eligible, row.v1Reason = "N", "paymentPlanManaInterference (a global mana effect reaches the payer)"
	case member:
		row.v1Eligible = "Y"
		if structural != "ok" {
			row.v1Reason = "MISMATCH admitted but mirror says: " + structural
		}
	default:
		row.v1Eligible = "N"
		if structural == "ok" {
			row.v1Reason = "window: " + row.v1Window
		}
	}
	if zone == state.ZBattlefield {
		got := e.PlanCastPayment(0, decision.PlannedCast{Object: cz.probe, Face: 0, Origin: "hand"})
		uses := false
		if got.Plan != nil {
			for _, a := range got.Plan.Activations {
				if a.Source == id {
					uses = true
				}
			}
		}
		switch {
		case uses:
			row.v1E2E = "plan uses source"
		case got.Plan != nil:
			row.v1E2E = "plan without source"
		default:
			row.v1E2E = "no plan: " + got.Reason
		}
		if row.creature && member && !intf {
			e.G.Obj(id).SummonSick = true
			row.v1Sick = yn(cz.v1Member(e, id, ma))
			e.G.Obj(id).SummonSick = false
		}
	}
	// Manual board.
	m := cz.board(it.card)
	mid := censusPlace(m, it.card, 0, zone, it.face)
	m.G.Obj(mid).IsToken = it.kind == "token"
	cz.enrich(m, mid)
	mma := censusFaceSA(m, mid, it)
	if cz.fodder {
		cz.addFodder(m, mid, mma)
	}
	row.manualOffered = yn(censusContains(m.availableManaAbilitiesForWindow(0, mid, true), mma))
	if row.manualOffered == "N" {
		row.manualOutcome = "not_offered"
		row.manualDetail = cz.v1WindowGate(m, mid, mma)
		return
	}
	row.manualOutcome, row.manualDetail = cz.manualActivate(m, mid, mma)
}

func censusContains(xs []*cards.SA, x *cards.SA) bool {
	for _, v := range xs {
		if censusSameSA(v, x) {
			return true
		}
	}
	return false
}

// censusSameSA is pointer identity, falling back to line/API/params equality
// for the abilities the engine builds fresh per walk (a CR 305.6 intrinsic,
// an SVar-resolved grant).
func censusSameSA(a, b *cards.SA) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil || a.Line != b.Line || a.API != b.API || a.Kind != b.Kind || len(a.Params) != len(b.Params) {
		return false
	}
	for k, v := range a.Params {
		if w, ok := b.Params[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// censusFaceSA returns the placed object's own pointer for the enumerated
// ability (the registry face and the object's face are the same compiled
// face, so the pointer is shared).
func censusFaceSA(e *Engine, id state.ObjID, it censusItem) *cards.SA {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return nil
	}
	for _, a := range o.Face().Abilities {
		if a == it.sa {
			return a
		}
	}
	return nil
}

// manualActivate drives one activation through the ordinary priority offer:
// the "activate for mana" option, the ability pick when the source has
// several, and any follow-up decision (colour, sacrifice, discard) answered
// with its first option(s). The outcome is read from the log.
func (cz *autopayCensus) manualActivate(e *Engine, id state.ObjID, ma *cards.SA) (string, string) {
	e.pending = nil
	e.askPriority(0)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		return "no_priority", ""
	}
	pick := -1
	for _, opt := range d.Options {
		if opt.Kind == "activate" && opt.Obj == id {
			pick = opt.Index
			break
		}
	}
	if pick < 0 {
		return "not_offered", "no activate option at priority"
	}
	start := len(e.L.Events)
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
		return "submit_error", err.Error()
	}
	if out, detail := cz.drive(e, id, ma); out != "" {
		return out, detail
	}
	return censusTally(e.L.Events[start:])
}

// drive answers pending non-priority decisions until priority returns.
func (cz *autopayCensus) drive(e *Engine, id state.ObjID, ma *cards.SA) (string, string) {
	for step := 0; step < 24; step++ {
		d := e.Pending()
		if d == nil || d.Kind == decision.KPriority || e.G.Over {
			return "", ""
		}
		var choices []int
		if e.choosing == chooseMana && e.manaActivation != nil && e.manaActivation.source == id && ma != nil {
			want := -1
			for i, a := range e.manaActivation.abilities {
				if want < 0 && censusSameSA(a, ma) {
					want = i
				}
			}
			for _, opt := range d.Options {
				if opt.Ability == want {
					choices = []int{opt.Index}
					break
				}
			}
			if choices == nil {
				return "ability_not_in_choice", ""
			}
		} else {
			n := d.Min
			if n < 1 {
				n = 1
			}
			if d.Max > 0 && n > d.Max {
				n = d.Max
			}
			if n > len(d.Options) {
				n = len(d.Options)
			}
			for i := 0; i < n; i++ {
				choices = append(choices, d.Options[i].Index)
			}
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
			return "submit_error", string(d.Kind) + ": " + err.Error()
		}
	}
	return "decision_loop", ""
}

func censusTally(evs []events.Event) (string, string) {
	var total int32
	var cols []string
	var notes []string
	for _, ev := range evs {
		switch ev.Kind {
		case events.ManaAdd:
			if ev.Player == 0 && ev.Amount > 0 {
				total += ev.Amount
				sym := ev.Counter
				if sym != "" {
					sym = sym[len(sym)-1:]
				}
				cols = append(cols, fmt.Sprintf("%d%s", ev.Amount, sym))
			}
		case events.Note:
			low := strings.ToLower(ev.Text)
			if strings.Contains(low, "unhandled") || strings.Contains(low, "unimplemented") ||
				strings.Contains(low, "not implemented") || strings.Contains(low, "no mana to reflect") {
				notes = append(notes, ev.Text)
			}
		}
	}
	detail := strings.Join(cols, "+")
	if len(notes) > 0 {
		n := strings.ReplaceAll(notes[0], ",", ";")
		if len(n) > 120 {
			n = n[:120]
		}
		return "unhandled_note", n
	}
	if total == 0 {
		return "no_mana", ""
	}
	return "mana_added", detail
}

// evalSpell casts the spell from hand on the manual board and resolves it.
func (cz *autopayCensus) evalSpell(row *censusRow) {
	it := row.item
	e := cz.board(it.card)
	id := censusPlace(e, it.card, 0, state.ZHand, it.face)
	cz.enrich(e, id)
	e.pending = nil
	e.askPriority(0)
	d := e.Pending()
	pick := -1
	if d != nil {
		for _, opt := range d.Options {
			if opt.Kind == "cast" && opt.Obj == id && opt.Mode == "" && opt.AltCostIndex == 0 {
				pick = opt.Index
				break
			}
		}
	}
	if pick < 0 {
		row.manualOffered, row.manualOutcome = "N", "not_castable"
		return
	}
	row.manualOffered = "Y"
	start := len(e.L.Events)
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
		row.manualOutcome, row.manualDetail = "submit_error", err.Error()
		return
	}
	if out, detail := cz.drive(e, id, nil); out != "" {
		row.manualOutcome, row.manualDetail = out, detail
		return
	}
	for guard := 0; guard < 4 && len(e.G.Stack) > 0; guard++ {
		onStack := false
		for _, sid := range e.G.Stack {
			if sid == id {
				onStack = true
			}
		}
		if !onStack {
			break
		}
		e.pending = nil
		e.resolveTop()
		if out, detail := cz.drive(e, id, nil); out != "" {
			row.manualOutcome, row.manualDetail = out, detail
			return
		}
	}
	row.manualOutcome, row.manualDetail = censusTally(e.L.Events[start:])
}

// evalModifier measures a triggered or replacement mana shape: whether its
// presence makes the planner decline (the V1 contract), and -- for the
// tap-driven modes -- what tapping a Forest, a {G} dork and a {C} rock
// actually produces with it on the battlefield.
func (cz *autopayCensus) evalModifier(row *censusRow) {
	it := row.item
	e := cz.board(it.card)
	id := censusPlace(e, it.card, 0, state.ZBattlefield, it.face)
	e.G.Obj(id).IsToken = it.kind == "token"
	island := censusPlace(e, cz.support["island"], 0, state.ZBattlefield, 0)
	cz.attach(e, id, island)
	row.presenceIntf = yn(e.paymentPlanManaInterference())
	got := e.PlanCastPayment(0, decision.PlannedCast{Object: cz.probe, Face: 0, Origin: "hand"})
	if got.Plan != nil {
		row.v1E2E = "plan with modifier present"
	} else {
		row.v1E2E = "no plan: " + got.Reason
	}
	mode := ""
	if it.trig != nil {
		mode = it.trig.Mode
	}
	if it.repl != nil {
		mode = it.repl.Event
	}
	switch mode {
	case "TapsForMana", "Taps", "ManaAdded", "ProduceMana":
	default:
		row.manualOutcome = "n/a (not tap-driven; registry only)"
		return
	}
	var results []string
	for _, k := range []string{"forest", "dork", "rock"} {
		m := cz.board(it.card)
		mid := censusPlace(m, it.card, 0, state.ZBattlefield, it.face)
		src := censusPlace(m, cz.support[k], 0, state.ZBattlefield, 0)
		o := m.G.Obj(mid)
		o.ChosenColor = "G"
		o.ChosenType = "Forest"
		cz.attach(m, mid, src)
		mas := m.availableManaAbilitiesForWindow(0, src, true)
		if len(mas) == 0 {
			results = append(results, k+"=not_offered")
			continue
		}
		out, detail := cz.manualActivate(m, src, mas[0])
		results = append(results, k+"="+out+":"+detail)
	}
	row.manualOffered = "Y"
	row.manualDetail = strings.Join(results, " ")
	switch {
	case strings.Contains(row.manualDetail, "panic"):
		row.manualOutcome = "panic"
	case censusModifierChanged(results):
		row.manualOutcome = "modifies_production"
	case strings.Contains(row.manualDetail, "unhandled_note"):
		row.manualOutcome = "unhandled_note"
	default:
		row.manualOutcome = "no_change_observed"
	}
}

// censusModifierChanged reports whether any source produced something other
// than its printed single unit (Forest 1G, dork 1G, rock 1C).
func censusModifierChanged(results []string) bool {
	want := map[string]string{"forest": "mana_added:1G", "dork": "mana_added:1G", "rock": "mana_added:1C"}
	for _, r := range results {
		k, v, _ := strings.Cut(r, "=")
		if strings.HasPrefix(v, "mana_added:") && v != want[k] {
			return true
		}
	}
	return false
}

// attach hangs an Aura or Equipment candidate on the recipient so an
// "enchanted land"/"equipped creature" shape has something to read.
func (cz *autopayCensus) attach(e *Engine, id, to state.ObjID) {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return
	}
	for _, typ := range o.Face().Types {
		if typ == "Aura" || typ == "Equipment" {
			o.AttachedTo = to
			censusStale(e)
			return
		}
	}
}

// evalGrant places the grantor with a land, a creature and an artifact
// recipient, and classifies whatever mana ability a recipient gains.
func (cz *autopayCensus) evalGrant(row *censusRow) {
	it := row.item
	e := cz.board(it.card)
	id := censusPlace(e, it.card, 0, state.ZBattlefield, it.face)
	o := e.G.Obj(id)
	o.ChosenColor = "G"
	o.ChosenType = "Forest"
	e.G.Obj(id).IsToken = it.kind == "token"
	type recipient struct {
		key string
		id  state.ObjID
	}
	land := censusPlace(e, cz.support["land"], 0, state.ZBattlefield, 0)
	forest := censusPlace(e, cz.support["forest"], 0, state.ZBattlefield, 0)
	bear := censusPlace(e, cz.support["bear"], 0, state.ZBattlefield, 0)
	var relic state.ObjID
	for _, bid := range e.G.Zone(state.ZBattlefield, 0) {
		if e.G.Obj(bid).Face().Name == "Census Relic" {
			relic = bid
			break
		}
	}
	recips := []recipient{{"land", land}, {"forest", forest}, {"bear", bear}, {"relic", relic}, {"legend", cz.legend}, {"self", id}}
	// Attach to the recipient an Enchant/Equip shape most plausibly names.
	target := forest
	for _, k := range o.Face().Keywords {
		if strings.HasPrefix(k, "Enchant:") && strings.Contains(k, "Creature") {
			target = bear
		}
		if strings.HasPrefix(k, "Enchant:") && strings.Contains(k, "Artifact") {
			target = relic
		}
	}
	for _, typ := range o.Face().Types {
		if typ == "Equipment" {
			target = bear
		}
	}
	cz.attach(e, id, target)
	for _, rc := range recips {
		if rc.id == 0 {
			continue
		}
		ro := e.G.Obj(rc.id)
		for _, ma := range e.availableManaAbilitiesForWindow(0, rc.id, true) {
			if _, _, printed := pileAbilityRefOf(ro, ma); printed && !strings.HasPrefix(ma.Line, "intrinsic:") {
				continue
			}
			if strings.HasPrefix(ma.Line, "intrinsic:") && rc.key == "forest" && ma.Params["Produced"] == "G" {
				continue // the Forest's own intrinsic
			}
			// A land-type grant shows up as an intrinsic; an AddAbility grant as
			// a non-intrinsic ability the recipient does not print.
			if (it.family == "land_type_grant") != strings.HasPrefix(ma.Line, "intrinsic:") {
				continue
			}
			row.api = ma.API
			if it.family == "land_type_grant" {
				row.prodClass, row.amount = censusProdClass(ma)
				tap, parts := censusCostParts(e, ma.Params["Cost"])
				row.costClass, row.costParts = censusCostClass(tap, parts), strings.Join(parts, " ")
			}
			member := cz.v1Member(e, rc.id, ma)
			row.v1Eligible = yn(member && !e.paymentPlanManaInterference())
			row.v1Structural = row.v1Eligible
			row.v1Reason = cz.v1Structural(e, rc.id, ma)
			if member && row.v1Reason == "ok" {
				row.v1Reason = "ok (granted to " + rc.key + ")"
			} else if member {
				row.v1Reason = "MISMATCH admitted but mirror says: " + row.v1Reason
			}
			m := e.Clone()
			pl := &m.G.Players[0]
			for i := range pl.Pool {
				pl.Pool[i] += 3
			}
			censusStale(m)
			row.manualOffered = "Y"
			out, detail := cz.manualActivate(m, rc.id, ma)
			row.manualOutcome, row.manualDetail = out, rc.key+":"+detail
			return
		}
	}
	row.v1Eligible, row.v1Reason = "N", "no recipient gained a mana ability on the census board"
	row.manualOffered, row.manualOutcome = "N", "grant_not_observed"
}

// ---------------------------------------------------------------------------
// output

func (cz *autopayCensus) writeCSV(path string, rows []censusRow) {
	f, err := os.Create(path)
	if err != nil {
		cz.t.Fatal(err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"kind", "card", "face", "ability", "family", "api", "zone", "cost_class", "cost_parts",
		"prod_class", "amount", "restricted", "riders", "params_extra", "conditions", "self_interference", "no_untap",
		"class", "v1_eligible", "v1_structural", "v1_reason", "v1_window", "v1_e2e_probe", "v1_admitted_while_sick", "presence_interference",
		"creature", "manual_offered", "manual_outcome", "manual_detail", "playable", "missing_primitives", "repo_decks",
		"token_creators", "token_creator_decks"})
	for _, r := range rows {
		_ = w.Write([]string{r.item.kind, r.item.key, strconv.Itoa(r.item.face), r.item.ident, r.item.family, r.api,
			r.zone, r.costClass, r.costParts, r.prodClass, r.amount, r.restricted, r.riders, r.paramsExtra, r.conditions,
			r.selfIntf, yn(r.noUntap), r.class, r.v1Eligible, r.v1Structural, r.v1Reason, r.v1Window, r.v1E2E, r.v1Sick, r.presenceIntf,
			yn(r.creature), r.manualOffered, r.manualOutcome, r.manualDetail, r.playable, r.missing, strings.Join(r.decks, ";"),
			strconv.Itoa(r.creators), strings.Join(r.creatorDecks, ";")})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		cz.t.Fatal(err)
	}
}

type censusAgg struct {
	class                                      string
	abilities, v1, v1s, manualOK, manualTried  int
	cards, tokens, deckCards, playable, played map[string]bool
	examples                                   []string
	exampleDecked                              []string
}

func (cz *autopayCensus) aggregate(rows []censusRow) []*censusAgg {
	by := map[string]*censusAgg{}
	for _, r := range rows {
		a := by[r.class]
		if a == nil {
			a = &censusAgg{class: r.class, cards: map[string]bool{}, tokens: map[string]bool{},
				deckCards: map[string]bool{}, playable: map[string]bool{}, played: map[string]bool{}}
			by[r.class] = a
		}
		a.abilities++
		if r.v1Eligible == "Y" {
			a.v1++
		}
		if r.v1Structural == "Y" {
			a.v1s++
		}
		if r.manualOutcome != "" && !strings.HasPrefix(r.manualOutcome, "n/a") {
			a.manualTried++
			if r.manualOutcome == "mana_added" || r.manualOutcome == "modifies_production" {
				a.manualOK++
			}
		}
		name := r.item.key
		if r.item.kind == "token" {
			a.tokens[name] = true
		} else {
			a.cards[name] = true
		}
		if len(r.decks) > 0 || len(r.creatorDecks) > 0 {
			if !a.deckCards[name] {
				a.exampleDecked = append(a.exampleDecked, name)
			}
			a.deckCards[name] = true
		}
		if r.playable == "Y" {
			a.playable[name] = true
		}
		if len(a.examples) < 4 && !censusHas(a.examples, name) {
			a.examples = append(a.examples, name)
		}
	}
	out := make([]*censusAgg, 0, len(by))
	for _, a := range by {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].cards)+len(out[i].tokens) != len(out[j].cards)+len(out[j].tokens) {
			return len(out[i].cards)+len(out[i].tokens) > len(out[j].cards)+len(out[j].tokens)
		}
		return out[i].class < out[j].class
	})
	return out
}

func (cz *autopayCensus) writeClasses(path string, rows []censusRow) {
	var b strings.Builder
	b.WriteString("| class | abilities | cards | tokens | repo-deck cards/tokens | V1 admitted (census board) | V1 structural | manual OK / tried | playable cards | examples (repo-deck first) |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
	for _, a := range cz.aggregate(rows) {
		ex := append([]string(nil), a.exampleDecked...)
		sort.Strings(ex)
		if len(ex) > 4 {
			ex = ex[:4]
		}
		for _, e := range a.examples {
			if len(ex) >= 4 {
				break
			}
			if !censusHas(ex, e) {
				ex = append(ex, e)
			}
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d | %d | %d / %d | %d | %s |\n", a.class, a.abilities, len(a.cards),
			len(a.tokens), len(a.deckCards), a.v1, a.v1s, a.manualOK, a.manualTried, len(a.playable), strings.Join(ex, "; "))
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		cz.t.Fatal(err)
	}
}

// censusStaples is the audit brief's explicit staples list. Tokens are named
// by script stem.
var censusStaples = []string{
	"Chrome Mox", "Mox Diamond", "Mox Opal", "Mox Amber", "Lotus Petal", "Black Lotus", "Lion's Eye Diamond",
	"c_a_treasure_sac", "c_a_gold_sac", "c_a_powerstone", "c_0_1_eldrazi_spawn_sac", "c_1_1_eldrazi_scion_sac",
	"Azorius Signet", "Arcane Signet", "Talisman of Dominance", "Adarkar Wastes", "Sulfurous Springs",
	"Horizon Canopy", "Sunbaked Canyon", "City of Brass", "Mana Confluence", "Ancient Tomb", "Gemstone Mine",
	"Tendo Ice Bridge", "Springleaf Drum", "Mana Vault", "Grim Monolith", "Basalt Monolith", "Sol Ring",
	"Birds of Paradise", "Llanowar Elves", "Elvish Archdruid", "Priest of Titania", "Gaea's Cradle",
	"Serra's Sanctum", "Tolarian Academy", "Cabal Coffers", "Nykthos, Shrine to Nyx", "Urza's Tower",
	"Azorius Chancery", "Simic Growth Chamber", "Mystic Gate", "Cascade Bluffs", "Darkwater Catacombs",
	"Cavern of Souls", "Eldrazi Temple", "Mishra's Workshop", "Ancient Ziggurat", "Simian Spirit Guide",
	"Elvish Spirit Guide", "Utopia Sprawl", "Wild Growth", "Mana Flare", "Mana Reflection", "Fellwar Stone",
	"Exotic Orchard", "Reflecting Pool", "Pentad Prism", "Coalition Relic", "Everflowing Chalice",
	"Gilded Lotus", "Doubling Cube", "Dark Ritual", "Command Tower", "Selesnya Guildgate", "Glacial Fortress",
	"Wastes", "Forest", "Tundra", "Snow-Covered Forest", "Jungle Hollow", "Chromatic Lantern",
	"Heartbeat of Spring", "Caged Sun", "Tarnished Citadel", "Mind Stone", "Arcane Signet",
}

func (cz *autopayCensus) writeStaples(path string, rows []censusRow) {
	var b strings.Builder
	b.WriteString("| staple | ability | class | V1 | V1 reason | e2e {1} probe | manual | manual detail | playable | repo decks |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
	seen := map[string]bool{}
	for _, s := range censusStaples {
		if seen[s] {
			continue
		}
		seen[s] = true
		found := false
		for _, r := range rows {
			if r.item.key != s {
				continue
			}
			found = true
			decks := len(r.decks)
			if r.item.kind == "token" {
				decks = len(r.creatorDecks)
			}
			fmt.Fprintf(&b, "| %s | %s %s | %s | %s | %s | %s | %s | %s | %s | %d |\n", s, r.item.ident, r.item.family,
				r.class, r.v1Eligible, r.v1Reason, r.v1E2E, r.manualOutcome, r.manualDetail, r.playable, decks)
		}
		if !found {
			fmt.Fprintf(&b, "| %s | (no mana-producing ability row) | | | | | | | | |\n", s)
		}
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		cz.t.Fatal(err)
	}
}

// writeFindings lists every ability the planner ADMITS that carries a shape
// the V1 contract excludes, and every presence shape the interference gate
// does not see.
func (cz *autopayCensus) writeFindings(path string, rows []censusRow) {
	var b strings.Builder
	b.WriteString("# V1 admission findings\n\n")
	section := func(title string, keep func(r censusRow) bool) {
		fmt.Fprintf(&b, "## %s\n\n", title)
		n := 0
		for _, r := range rows {
			if !keep(r) {
				continue
			}
			n++
			fmt.Fprintf(&b, "- %s [%s %s] class=%s riders=%q extra=%q e2e=%q decks=%d\n", r.item.key, r.item.kind,
				r.item.ident, r.class, r.riders, r.paramsExtra, r.v1E2E, len(r.decks)+len(r.creatorDecks))
		}
		fmt.Fprintf(&b, "\ncount: %d\n\n", n)
	}
	section("Admitted with a SubAbility rider", func(r censusRow) bool {
		return r.v1Eligible == "Y" && strings.Contains(r.riders, "sub:")
	})
	section("Admitted with special-production parameters", func(r censusRow) bool {
		if r.v1Eligible != "Y" {
			return false
		}
		for _, k := range censusSpecialProduction {
			if censusHas(strings.Fields(r.paramsExtra), k) {
				return true
			}
		}
		return false
	})
	section("Admitted while summoning sick (creature source)", func(r censusRow) bool { return r.v1Sick == "Y" })
	section("Admitted with a doesn't-untap drawback (policy hazard)", func(r censusRow) bool {
		return r.v1Eligible == "Y" && r.noUntap
	})
	section("Admitted with a non-gate extra parameter", func(r censusRow) bool {
		if r.v1Eligible != "Y" {
			return false
		}
		conds := strings.Fields(r.conditions)
		for _, k := range strings.Fields(r.paramsExtra) {
			if !censusHas(conds, k) {
				return true
			}
		}
		return false
	})
	section("Tap/mana-event modifier the planner does NOT decline (plan offered with it present)", func(r censusRow) bool {
		if r.item.family != "triggered" && r.item.family != "replacement" {
			return false
		}
		mode := ""
		if r.item.trig != nil {
			mode = r.item.trig.Mode
		} else if r.item.repl != nil {
			mode = r.item.repl.Event
		}
		switch mode {
		case "TapsForMana", "Taps", "TapAll", "ManaAdded", "ProduceMana":
		default:
			return false
		}
		return r.presenceIntf == "N" && strings.HasPrefix(r.v1E2E, "plan with")
	})
	section("Mirror disagreements (census bug if non-empty)", func(r censusRow) bool {
		return strings.HasPrefix(r.v1Reason, "MISMATCH")
	})
	section("Manual path failures on a playable card", func(r censusRow) bool {
		if r.playable != "Y" {
			return false
		}
		switch r.manualOutcome {
		case "not_offered", "no_mana", "unhandled_note", "panic", "submit_error", "decision_loop", "ability_not_in_choice", "not_castable", "grant_not_observed":
			return true
		}
		return false
	})
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		cz.t.Fatal(err)
	}
}

func (cz *autopayCensus) writeSummary(path string, rows []censusRow) {
	var b strings.Builder
	cardsAll, tokensAll := map[string]bool{}, map[string]bool{}
	fam := map[string]int{}
	var v1, bf int
	for _, r := range rows {
		if r.item.kind == "token" {
			tokensAll[r.item.key] = true
		} else {
			cardsAll[r.item.key] = true
		}
		fam[r.item.family]++
		if (r.item.family == "activated" || r.item.family == "intrinsic") && r.zone == "Battlefield" {
			bf++
			if r.v1Eligible == "Y" {
				v1++
			}
		}
	}
	fmt.Fprintf(&b, "ability rows: %d  cards: %d  tokens: %d\n", len(rows), len(cardsAll), len(tokensAll))
	fams := make([]string, 0, len(fam))
	for k := range fam {
		fams = append(fams, k)
	}
	sort.Strings(fams)
	for _, k := range fams {
		fmt.Fprintf(&b, "family %-16s %d\n", k, fam[k])
	}
	fmt.Fprintf(&b, "battlefield activated/intrinsic abilities: %d, V1 admitted on the census board: %d\n", bf, v1)
	var structural, sick, creatureAdmitted int
	srcCards, srcV1 := map[string]bool{}, map[string]bool{}
	for _, r := range rows {
		if (r.item.family == "activated" || r.item.family == "intrinsic") && r.zone == "Battlefield" {
			srcCards[r.item.kind+":"+r.item.key] = true
			if r.v1Structural == "Y" {
				structural++
				srcV1[r.item.kind+":"+r.item.key] = true
			}
			if r.creature && r.v1Eligible == "Y" {
				creatureAdmitted++
			}
			if r.v1Sick == "Y" {
				sick++
			}
		}
	}
	fmt.Fprintf(&b, "V1 structurally admissible (gate-dependent shapes counted): %d\n", structural)
	fmt.Fprintf(&b, "battlefield source cards/tokens: %d, with at least one V1-admissible ability: %d\n", len(srcCards), len(srcV1))
	fmt.Fprintf(&b, "creature-source abilities admitted: %d, of which still admitted while summoning sick: %d\n", creatureAdmitted, sick)
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		cz.t.Fatal(err)
	}
}

// censusInterferenceReasons names one face's tap/mana text: every
// Taps/TapsForMana trigger mode and every replacement event naming mana or a
// tap (the pre-aph-interference-scope whole-board scan, kept so a killswitch
// row still says which line a decline would come from), plus a ManaConvert
// static -- the one printed shape paymentPlanGlobalManaEffect treats as
// global.
func censusInterferenceReasons(f *cards.Face) []string {
	var out []string
	for _, st := range f.Statics {
		if st.Mode == "ManaConvert" {
			out = append(out, "static:ManaConvert")
		}
	}
	for _, t := range f.Triggers {
		if t.Mode == "Taps" || t.Mode == "TapsForMana" {
			out = append(out, "trigger:"+t.Mode)
		}
	}
	for _, r := range f.Repls {
		ev := strings.ToLower(r.Event)
		if strings.Contains(ev, "mana") || strings.Contains(ev, "tap") {
			out = append(out, "replacement:"+r.Event)
		}
	}
	return out
}

// writeKillSwitches lists every corpus card and token whose presence on the
// OPPONENT's battlefield makes the planner decline a plan it otherwise makes
// (seat 0: an Island and the {1} probe instant), by running PlanCastPayment
// end to end for every face in the corpus. A decline is attributed to the
// mana-interference scan (paymentPlanManaInterference's printed-face mirror),
// to the cast-shape gate (paymentPlanCastShapeOK: sunburst/converge/cast-spend
// readers, target-dependent cost statics), or to neither. A face that makes
// the probe uncastable, or raises its cost past one Island ("insufficient"),
// is a legitimate answer and not listed.
func (cz *autopayCensus) writeKillSwitches(csvPath, mdPath string) {
	ctl := cz.base.Clone()
	censusPlace(ctl, cz.support["island"], 0, state.ZBattlefield, 0)
	if got := ctl.PlanCastPayment(0, decision.PlannedCast{Object: cz.probe, Face: 0, Origin: "hand"}); got.Plan == nil {
		cz.t.Fatalf("census: control board has no plan: %s", got.Reason)
	}
	f, err := os.Create(csvPath)
	if err != nil {
		cz.t.Fatal(err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"kind", "card", "face", "reasons", "mirror_predicts", "planner_reason", "repo_decks"})
	type agg struct {
		cards, decked int
		ex            []string
	}
	byReason := map[string]*agg{}
	var total, decked, mirrored, faces int
	var deckedNames []string
	visit := func(kind, key string, c *cards.Card) {
		for fi, face := range c.Faces {
			if face == nil {
				continue
			}
			faces++
			var reason string
			func() {
				defer func() {
					if r := recover(); r != nil {
						reason = "panic"
					}
				}()
				e := cz.board(c)
				censusPlace(e, cz.support["island"], 0, state.ZBattlefield, 0)
				id := censusPlace(e, c, 1, state.ZBattlefield, fi)
				e.G.Obj(id).IsToken = kind == "token"
				got := e.PlanCastPayment(0, decision.PlannedCast{Object: cz.probe, Face: 0, Origin: "hand"})
				if got.Plan != nil || got.Reason != "unsupported" || !e.paymentPlanCastCandidate(0, cz.probe) {
					return
				}
				switch {
				case e.paymentPlanManaInterference():
					reason = "mana-interference"
				case e.paymentPlanSunburstGrantOut():
					reason = "cast-shape gate: Sunburst grant (paymentPlanSunburstGrantOut)"
				case e.triggeredConvergeReaderOut():
					reason = "cast-shape gate: converge reader (triggeredConvergeReaderOut)"
				case e.triggeredCastSpendReaderOut():
					reason = "cast-shape gate: cast-spend reader (triggeredCastSpendReaderOut)"
				case e.paymentPlanHasTargetDependentModifier(0, cz.probe):
					reason = "cast-shape gate: ValidTarget cost static (paymentPlanHasTargetDependentModifier)"
				case !e.paymentPlanCastShapeOK(0, cz.probe):
					reason = "cast-shape gate: other"
				default:
					reason = "other"
				}
			}()
			if reason == "" {
				continue
			}
			mirror := censusInterferenceReasons(face)
			detail := reason
			if reason == "mana-interference" && len(mirror) > 0 {
				mirrored++
				detail = strings.Join(mirror, " ")
			}
			decks := cz.decks[c]
			_ = w.Write([]string{kind, key, strconv.Itoa(fi), detail, yn(len(mirror) > 0), reason, strings.Join(decks, ";")})
			total++
			if len(decks) > 0 {
				decked++
				deckedNames = append(deckedNames, key)
			}
			keys := []string{reason}
			if reason == "mana-interference" && len(mirror) > 0 {
				keys = nil
				seen := map[string]bool{}
				for _, m := range mirror {
					if !seen[m] {
						seen[m] = true
						keys = append(keys, m)
					}
				}
			}
			for _, k := range keys {
				a := byReason[k]
				if a == nil {
					a = &agg{}
					byReason[k] = a
				}
				a.cards++
				if len(decks) > 0 {
					a.decked++
				}
				if len(a.ex) < 6 {
					a.ex = append(a.ex, key)
				}
			}
			break
		}
	}
	for _, c := range cz.r.Cards {
		if censusNamed(c) {
			visit("card", c.Faces[0].Name, c)
		}
	}
	stems := make([]string, 0, len(cz.r.Tokens))
	for stem := range cz.r.Tokens {
		stems = append(stems, stem)
	}
	sort.Strings(stems)
	for _, stem := range stems {
		visit("token", stem, cz.r.Tokens[stem])
	}
	w.Flush()
	var b strings.Builder
	fmt.Fprintf(&b, "faces probed end to end: %d. Kill switches (on the OPPONENT's battlefield, the probe stays castable but every plan is declined): %d; of which predicted by the printed-face interference mirror: %d; in repo decks: %d (%s)\n\n",
		faces, total, mirrored, decked, strings.Join(deckedNames, "; "))
	b.WriteString("| reason | cards/tokens | in repo decks | examples |\n|---|---|---|---|\n")
	keys := make([]string, 0, len(byReason))
	for k := range byReason {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if byReason[keys[i]].cards != byReason[keys[j]].cards {
			return byReason[keys[i]].cards > byReason[keys[j]].cards
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		a := byReason[k]
		fmt.Fprintf(&b, "| %s | %d | %d | %s |\n", k, a.cards, a.decked, strings.Join(a.ex, "; "))
	}
	if err := os.WriteFile(mdPath, []byte(b.String()), 0o644); err != nil {
		cz.t.Fatal(err)
	}
}

// writeDeckImpact reports, per repo deck, which of its mana-producing cards
// the V1 planner can use and which it cannot (by class), plus kill switches.
func (cz *autopayCensus) writeDeckImpact(path string, rows []censusRow) {
	type status struct {
		v1     bool
		class  string
		family string
	}
	perCard := map[string]*status{}
	var order []string
	other := map[string]string{}
	for _, r := range rows {
		if r.item.kind != "card" || len(r.decks) == 0 {
			continue
		}
		if !((r.item.family == "activated" || r.item.family == "intrinsic") && r.zone == "Battlefield") {
			if _, ok := other[r.item.key]; !ok {
				other[r.item.key] = r.class
			}
			continue
		}
		st := perCard[r.item.key]
		if st == nil {
			st = &status{class: r.class, family: r.item.family}
			perCard[r.item.key] = st
			order = append(order, r.item.key)
		}
		if r.v1Structural == "Y" || r.v1Eligible == "Y" {
			st.v1 = true
		}
	}
	var b strings.Builder
	b.WriteString("| deck | battlefield mana sources | V1-usable | not V1-usable (class) | other mana cards (class) | kill switches |\n|---|---|---|---|---|---|\n")
	for _, name := range testutil.RepoDeckNames() {
		var total, usable int
		var not, kills []string
		for _, key := range order {
			if !censusHas(cz.decksOf(key), name) {
				continue
			}
			st := perCard[key]
			total++
			if st.v1 {
				usable++
			} else {
				not = append(not, key+" ("+st.class+")")
			}
		}
		for _, c := range cz.r.Cards {
			if !censusNamed(c) || !censusHas(cz.decks[c], name) {
				continue
			}
			for _, face := range c.Faces {
				if face != nil && len(censusInterferenceReasons(face)) > 0 {
					kills = append(kills, c.Faces[0].Name)
					break
				}
			}
		}
		var others []string
		keys := make([]string, 0, len(other))
		for k := range other {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if _, isSource := perCard[k]; !isSource && censusHas(cz.decksOf(k), name) {
				others = append(others, k+" ("+other[k]+")")
			}
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %s | %s | %s |\n", name, total, usable, strings.Join(not, "; "),
			strings.Join(others, "; "), strings.Join(kills, "; "))
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		cz.t.Fatal(err)
	}
}

func (cz *autopayCensus) decksOf(name string) []string {
	if c, ok := cz.r.Lookup(name); ok {
		return cz.decks[c]
	}
	return nil
}
