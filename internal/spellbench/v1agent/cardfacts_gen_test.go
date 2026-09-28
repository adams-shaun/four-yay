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
// IR for every card of the pauper-kernel catalog and fails when the
// committed file differs. SB_REGEN_CARDFACTS=1 rewrites the file instead.
//
// The file holds derived facts in this package's own vocabulary (types,
// mana value, power/toughness, keyword heads, effect API names, damage
// amounts, target classes, polarity) -- never script text.
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
	var sorted []string
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
	facts := map[string]*CardFact{}
	var missing []string
	for _, n := range sorted {
		c, ok := r.Lookup(n)
		if !ok || len(c.Faces) == 0 {
			missing = append(missing, n)
			continue
		}
		facts[n] = factFromFace(c.Faces[0])
	}
	if len(missing) > 0 {
		t.Logf("not in the gorge corpus (hand facts only): %v", missing)
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

func factFromFace(f *cards.Face) *CardFact {
	cf := &CardFact{CMC: int(f.Cmc())}
	for _, ty := range f.Types {
		switch ty {
		case "Land", "Creature", "Instant", "Sorcery", "Artifact", "Enchantment":
			cf.Types = append(cf.Types, strings.ToLower(ty))
		}
	}
	if f.PT != "" {
		cf.Power, cf.Toughness, cf.HasPT = f.Power(), f.Toughness(), true
	}
	for _, kw := range f.Keywords {
		head := kw
		if i := strings.IndexByte(head, ':'); i >= 0 {
			head = head[:i]
		}
		switch head {
		case "Flying", "Reach", "Haste", "Trample", "Flash", "Defender", "Lifelink", "Deathtouch",
			"First Strike", "Double Strike", "Vigilance", "Menace", "Hexproof", "Indestructible",
			"Kicker", "Madness", "Flashback", "Ninjutsu", "Affinity", "Bestow", "Cycling", "TypeCycling",
			"Embalm", "Plot", "Changeling", "Protection", "Equip", "Backup", "Devoid":
			cf.Keywords = append(cf.Keywords, head)
		}
	}
	for _, a := range f.Abilities {
		switch a.Kind {
		case "SP":
			cf.Spell = effectFact(a)
		case "AB":
			if a.API == "Mana" {
				cf.Mana = true
				continue
			}
			cf.Abilities = append(cf.Abilities, *effectFact(a))
		}
	}
	for _, tr := range f.Triggers {
		if tr.Effect == nil {
			continue
		}
		e := effectFact(tr.Effect)
		e.Trigger = tr.Mode
		if tr.Mode == "ChangesZone" && tr.Params["Destination"] == "Battlefield" {
			e.Trigger = "ETB"
		}
		cf.Triggers = append(cf.Triggers, *e)
	}
	return cf
}

// harmfulAPIs are effects that hurt whatever they target.
var harmfulAPIs = map[string]bool{
	"DealDamage": true, "Destroy": true, "Counter": true, "Tap": true, "Sacrifice": true,
	"DigUntil": true, "Mill": true, "Discard": true,
}

func effectFact(a *cards.SA) *EffectFact {
	e := &EffectFact{API: a.API}
	for s := a; s != nil; s = s.Sub {
		if s != a {
			e.Chain = append(e.Chain, s.API)
		}
		p := s.Params
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
	}
	return e
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
