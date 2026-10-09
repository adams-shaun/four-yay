package templates_test

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// permNamed returns one battlefield permanent by name in the last snapshot.
func permNamed(t *testing.T, res rules.OracleResult, name string) rules.OracleSnapPerm {
	t.Helper()
	s := res.Snapshots[len(res.Snapshots)-1]
	for _, p := range s.Permanents {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("precondition: %s is not on the battlefield in the final snapshot", name)
	return rules.OracleSnapPerm{}
}

// countPermNamed counts the last snapshot's battlefield permanents named name.
func countPermNamed(res rules.OracleResult, name string) int {
	n := 0
	for _, p := range res.Snapshots[len(res.Snapshots)-1].Permanents {
		if p.Name == name {
			n++
		}
	}
	return n
}

// offeredHas reports whether the last snapshot offers kind for the source.
func offeredHas(res rules.OracleResult, kind, source string, labelContains string) bool {
	for _, o := range res.Snapshots[len(res.Snapshots)-1].Offered {
		if o.Kind == kind && (source == "" || o.Source == source) && strings.Contains(o.Label, labelContains) {
			return true
		}
	}
	return false
}

// stackSourceCount counts the last snapshot's stack entries whose source ref
// names ref exactly.
func stackSourceCount(res rules.OracleResult, ref string) int {
	n := 0
	for _, e := range res.Snapshots[len(res.Snapshots)-1].Stack {
		if e.Source == ref {
			n++
		}
	}
	return n
}

// kwHas reports whether the permanent carries the keyword.
func kwHas(p rules.OracleSnapPerm, kw string) bool {
	for _, k := range p.Keywords {
		if strings.EqualFold(k, kw) {
			return true
		}
	}
	return false
}

// TestRemainingStaticModeTemplates generates and replays every served
// remaining-static-modes item gorge-side: modeItem already asserts the
// classification precondition and the generation-time control (a control
// that fails to show the ordinary behaviour returns a skip, which modeItem
// fatals on); replay runs the observation scenario and its embedded
// Expectations. Each row then re-derives the observation's claim from the
// final snapshot, so a weakened template cannot pass the test silently.
func TestRemainingStaticModeTemplates(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		card, key, sub string
		check          func(*testing.T, rules.OracleResult)
	}{
		// Activations (Wonder Man): the power-up probe's second activation
		// happened -- four +1/+1 counters, twice the control's two.
		{"Wonder Man, Hollywood Hero", "static#0.0", "static.activations-powerup", func(t *testing.T, res rules.OracleResult) {
			if got := permNamed(t, res, "Brave Brawler").Counters["P1P1"]; got != 4 {
				t.Fatalf("Brave Brawler counters %d, want 4 (two activations)", got)
			}
		}},
		// ManaConvert over Case spells (Case File Auditor): the Case probe
		// cast from red mana entered the battlefield.
		{"Case File Auditor", "static#0.0", "static.mana-convert-case-spells", func(t *testing.T, res rules.OracleResult) {
			permNamed(t, res, "Case of the Gorgon's Kiss")
		}},
		// ManaConvert over activated abilities (Agatha's Soul Cauldron): the
		// activation's lifelink grant reached its target.
		{"Agatha's Soul Cauldron", "static#0.0", "static.mana-convert-abilities", func(t *testing.T, res rules.OracleResult) {
			if !kwHas(permNamed(t, res, "Alabaster Mage"), "Lifelink") {
				t.Fatal("Alabaster Mage does not show the activation's lifelink grant")
			}
		}},
		// CantPreventDamage unconditional (Sunspine Lynx): the Shock the
		// prevention source covers landed on the victim.
		{"Sunspine Lynx", "static#0.1", "static.cant-prevent-damage", func(t *testing.T, res rules.OracleResult) {
			if got := permNamed(t, res, "Giant Spider").Damage; got != 2 {
				t.Fatalf("Giant Spider damage %d, want 2 (the prevented damage landed)", got)
			}
		}},
		// CantPreventDamage combat-only (Frenzied Baloth): p1 took the
		// unblocked attacker's 2 combat damage.
		{"Frenzied Baloth", "static#0.0", "static.cant-prevent-damage-combat", func(t *testing.T, res rules.OracleResult) {
			if got := res.Snapshots[len(res.Snapshots)-1].Players[1].Life; got != 18 {
				t.Fatalf("p1 life %d, want 18 (the combat damage landed)", got)
			}
		}},
		// CantAttackUnless (Archangel of Tithes): the taxed attacker's
		// CanAttack=false is asserted by the scenario's own Expectation; the
		// card must be on p1's battlefield at the checkpoint.
		{"Archangel of Tithes", "static#0.0", "static.cant-attack-unless-tax", func(t *testing.T, res rules.OracleResult) {
			permNamed(t, res, "Archangel of Tithes")
		}},
		// IgnoreHexproof (Nowhere to Run): the hexproof probe took the burn
		// and died.
		{"Nowhere to Run", "static#0.0", "static.ignore-hexproof", func(t *testing.T, res rules.OracleResult) {
			if got := countPermNamed(res, "Witchstalker"); got != 0 {
				t.Fatalf("Witchstalker on the battlefield %d, want 0 (the burn targeted it)", got)
			}
		}},
		// CantPutCounter (Blossombind): the counter spell resolved but the
		// enchanted host carries no +1/+1 counter.
		{"Blossombind", "static#0.0", "static.cant-put-counter", func(t *testing.T, res rules.OracleResult) {
			if got := permNamed(t, res, "Giant Spider").Counters["P1P1"]; got != 0 {
				t.Fatalf("Giant Spider +1/+1 counters %d, want 0 (the host is enchanted)", got)
			}
		}},
		// NoCleanupDamage (Ancient Adamantoise): the combat damage it took is
		// still marked in turn 2's main phase.
		{"Ancient Adamantoise", "static#0.0", "static.no-cleanup-damage", func(t *testing.T, res rules.OracleResult) {
			if got := permNamed(t, res, "Ancient Adamantoise").Damage; got != 2 {
				t.Fatalf("Ancient Adamantoise damage %d, want 2 (survives cleanup)", got)
			}
		}},
		// CantBeSuspected (Airtight Alibi): the host shows the Aura's hexproof
		// grant and never the suspect designation's menace.
		{"Airtight Alibi", "static#0.1", "static.cant-be-suspected", func(t *testing.T, res rules.OracleResult) {
			host := permNamed(t, res, "Giant Spider")
			if kwHas(host, "Menace") {
				t.Fatal("the host still shows the suspect designation's menace")
			}
			if !kwHas(host, "Hexproof") {
				t.Fatal("the host does not show the attached card's hexproof grant")
			}
		}},
		// ActivateAbilityAsIfHaste (Shang-Chi): the summoning-sick probe's
		// activation is offered at the priority checkpoint.
		{"Shang-Chi, Master of Kung Fu", "static#0.0", "static.activate-as-if-haste", func(t *testing.T, res rules.OracleResult) {
			if !offeredHas(res, "activate", "p0:Prodigal Sorcerer", "") {
				t.Fatal("the summoning-sick probe's activation is not offered")
			}
		}},
		// PlotZone (Fblthp): a plot cast for the top card is offered.
		{"Fblthp, Lost on the Range", "static#0.2", "static.plot-zone", func(t *testing.T, res rules.OracleResult) {
			if !offeredHas(res, "cast", "", "Plot Memnite") {
				t.Fatal("no plot offer for the top card of the library")
			}
		}},
		// CantBeCopied (Choreographed Sparks): the copy spell resolved with
		// only the original on the stack -- no copy minted.
		{"Choreographed Sparks", "static#0.0", "static.cant-be-copied", func(t *testing.T, res rules.OracleResult) {
			if got := stackSourceCount(res, "p0:Choreographed Sparks"); got != 1 {
				t.Fatalf("Choreographed Sparks on the stack %d, want 1 (no copy minted)", got)
			}
		}},
		// UnspentMana (Electro): the red mana added mid-step survives the
		// step end in the pool.
		{"Electro, Assaulting Battery", "static#0.0", "static.unspent-mana", func(t *testing.T, res rules.OracleResult) {
			if got := res.Snapshots[len(res.Snapshots)-1].Players[0].Pool; !strings.Contains(got, "R") {
				t.Fatalf("p0 pool %q, want it to still hold red mana after the step end", got)
			}
		}},
		// IgnoreLegendRule (Spider-Verse): both legendary Spiders stayed.
		{"Spider-Verse", "static#0.0", "static.ignore-legend-rule", func(t *testing.T, res rules.OracleResult) {
			if got := countPermNamed(res, "Lady Spider, Maybelle Reilly"); got != 2 {
				t.Fatalf("legendary Spider copies on the battlefield %d, want 2", got)
			}
		}},
	} {
		it := modeItem(t, tc.card, tc.key, tc.sub)
		res := replay(t, reg, it)
		tc.check(t, res)
	}
}

// TestCantBlockUnlessTaxStaysNamedSkip pins the one served-shape row whose
// scenario grammar reaches no checkpoint: the {1} block tax leaves no
// affordable block, so the blockers decision is never posed and the template
// keeps the requirement a named skip rather than pinning a vacuous item.
func TestCantBlockUnlessTaxStaysNamedSkip(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("Archangel of Tithes")
	if !ok {
		t.Fatal("precondition: Archangel of Tithes absent from corpus")
	}
	for _, req := range levelb.Requirements(c) {
		if req.Key != "static#0.1" {
			continue
		}
		if req.Sub != "static.cant-block-unless-tax" {
			t.Fatalf("precondition: static#0.1 classifies as %q, want static.cant-block-unless-tax", req.Sub)
		}
		f := c.Faces[req.Face]
		if len(f.Statics) == 0 {
			t.Fatal("precondition: face 0 carries no static abilities")
		}
		_, sk := templates.GenerateB(reg, "Archangel of Tithes", req)
		if sk == nil {
			t.Fatal("static#0.1 generated an item; want the named paid-block-checkpoint skip")
		}
		if !strings.Contains(sk.Reason, "blockers decision is not posed") {
			t.Fatalf("skip reason %q does not name the missing checkpoint", sk.Reason)
		}
		return
	}
	t.Fatal("precondition: Archangel of Tithes has no requirement static#0.1")
}
