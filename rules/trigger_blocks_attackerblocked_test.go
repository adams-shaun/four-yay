package rules

// trigger_blocks_attackerblocked_test.go pins Forge Mode$ AttackerBlocked's
// ValidBlocker$ read (ticket agent-20261009T123507Z-34cad6da). The candidate
// walk used to read only ValidCard$ against each attacker, so a
// blocker-anchored line ("Whenever CARDNAME blocks a creature", Wall of
// Frost) fired for every block declaration whoever blocked, an
// attacker-anchored restricted line ("becomes blocked by an artifact
// creature", Tel-Jilad Wolf) fired for any blocker, and TriggerBlocker was
// never captured. The filed report called the blocker half inert; it was
// not -- it over-fired.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// abScenario builds a one-combat oracle scenario: p1 attacks p0 with
// attackers, p0 declares blocks, and the combat settles. preOps are raw step
// objects inserted before the attack (an attach); the block checkpoint is
// snapshot len(preOps)+2.
func abScenario(name string, p0, p1 []string, preOps []string, attackers []string, blocks [][2]string) string {
	q := func(xs []string) string {
		b, _ := json.Marshal(xs)
		return string(b)
	}
	bl, _ := json.Marshal(blocks)
	steps := append([]string{}, preOps...)
	steps = append(steps,
		`{"op":"attack","seat":1,"attackers":`+q(attackers)+`,"defender":"p0"}`,
		`{"op":"block","seat":0,"blocks":`+string(bl)+`}`,
	)
	return `{"name":"` + name + `","cr":["509.1a"],"why":"AttackerBlocked reads ValidBlocker$",` +
		`"setup":{"p0":{"battlefield":` + q(p0) + `},"p1":{"battlefield":` + q(p1) + `}},` +
		`"steps":[` + strings.Join(steps, ",") + `]}`
}

// abBlockCheckpoint runs sc and returns the declare-blockers snapshot's
// on-stack ability sources, failing unless every ref in blocking really is
// blocking there (so an empty stack means "did not fire", not "no block").
func abBlockCheckpoint(t *testing.T, sc string, block int, blocking ...string) (OracleResult, []string) {
	t.Helper()
	res, err := RunOracleScenarioJSON(testutil.CorpusRegistry(t), []byte(sc))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	if got := res.Snapshots[block].Step; got != "declare-blockers" {
		t.Fatalf("snapshot %d step = %q, want declare-blockers", block, got)
	}
	for _, ref := range blocking {
		if p, ok := findPerm(t, res.Snapshots, block, ref); !ok || !p.Blocking {
			t.Fatalf("%s is not blocking at the block checkpoint: %+v", ref, p)
		}
	}
	return res, stackAbilities(t, res.Snapshots, block)
}

func wantAbilities(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("block checkpoint abilities = %v, want %v", got, want)
	}
}

// TestAttackerBlockedBlockerHalfFiresForTheBlocker is the regression guard:
// Wall of Frost blocking is one instance, the Wall's own.
func TestAttackerBlockedBlockerHalfFiresForTheBlocker(t *testing.T) {
	sc := abScenario("wall-of-frost-blocker-half", []string{"Wall of Frost"}, []string{"Grizzly Bears"},
		nil, []string{"p1:Grizzly Bears"}, [][2]string{{"p0:Wall of Frost", "p1:Grizzly Bears"}})
	_, ab := abBlockCheckpoint(t, sc, 2, "p0:Wall of Frost")
	wantAbilities(t, ab, "p0:Wall of Frost")
}

// TestAttackerBlockedBlockerFilterIsRead is the failing-first case: another
// creature blocks, the Wall does not. "Whenever CARDNAME blocks a creature"
// must not fire (measured pre-fix: [p0:Wall of Frost]).
func TestAttackerBlockedBlockerFilterIsRead(t *testing.T) {
	sc := abScenario("wall-of-frost-other-blocks", []string{"Wall of Frost", "Grizzly Bears"}, []string{"Grizzly Bears"},
		nil, []string{"p1:Grizzly Bears"}, [][2]string{{"p0:Grizzly Bears", "p1:Grizzly Bears"}})
	res, ab := abBlockCheckpoint(t, sc, 2, "p0:Grizzly Bears")
	if w, ok := findPerm(t, res.Snapshots, 2, "p0:Wall of Frost"); !ok || w.Blocking {
		t.Fatalf("precondition: Wall of Frost must be on the battlefield and not blocking: %+v", w)
	}
	wantAbilities(t, ab)
}

// TestAttackerBlockedEquippedSpellingFires: Shield of the Righteous's
// ValidBlocker$ Card.EquippedBy names the bearer; the trigger is the
// equipment's own and fires when the bearer blocks.
func TestAttackerBlockedEquippedSpellingFires(t *testing.T) {
	pre := []string{`{"op":"attach","card":"p0:Shield of the Righteous","attached_to":"p0:Grizzly Bears"}`}
	sc := abScenario("shield-equipped-blocks", []string{"Shield of the Righteous", "Grizzly Bears"}, []string{"Barony Vampire"},
		pre, []string{"p1:Barony Vampire"}, [][2]string{{"p0:Grizzly Bears", "p1:Barony Vampire"}})
	res, ab := abBlockCheckpoint(t, sc, 3, "p0:Grizzly Bears")
	if s, ok := findPerm(t, res.Snapshots, 3, "p0:Shield of the Righteous"); !ok || s.AttachedTo != "p0:Grizzly Bears" {
		t.Fatalf("precondition: Shield must be attached to the blocker: %+v", s)
	}
	wantAbilities(t, ab, "p0:Shield of the Righteous")
}

// TestAttackerBlockedEquippedSpellingIgnoresUnequippedBlocker: the bearer
// stays home and another creature blocks, so the Shield does not fire.
func TestAttackerBlockedEquippedSpellingIgnoresUnequippedBlocker(t *testing.T) {
	pre := []string{`{"op":"attach","card":"p0:Shield of the Righteous","attached_to":"p0:Grizzly Bears"}`}
	sc := abScenario("shield-bearer-home", []string{"Shield of the Righteous", "Grizzly Bears", "Hill Giant"}, []string{"Barony Vampire"},
		pre, []string{"p1:Barony Vampire"}, [][2]string{{"p0:Hill Giant", "p1:Barony Vampire"}})
	res, ab := abBlockCheckpoint(t, sc, 3, "p0:Hill Giant")
	if s, ok := findPerm(t, res.Snapshots, 3, "p0:Shield of the Righteous"); !ok || s.AttachedTo != "p0:Grizzly Bears" {
		t.Fatalf("precondition: Shield must be attached to the idle bearer: %+v", s)
	}
	wantAbilities(t, ab)
}

// TestAttackerBlockedAttackerHalfReadsBlockerSpec: Tel-Jilad Wolf "becomes
// blocked by an artifact creature" fires for Phyrexian Walker and not for
// Grizzly Bears.
func TestAttackerBlockedAttackerHalfReadsBlockerSpec(t *testing.T) {
	plain := abScenario("tel-jilad-plain-blocker", []string{"Grizzly Bears"}, []string{"Tel-Jilad Wolf"},
		nil, []string{"p1:Tel-Jilad Wolf"}, [][2]string{{"p0:Grizzly Bears", "p1:Tel-Jilad Wolf"}})
	_, ab := abBlockCheckpoint(t, plain, 2, "p0:Grizzly Bears")
	wantAbilities(t, ab)

	art := abScenario("tel-jilad-artifact-blocker", []string{"Phyrexian Walker"}, []string{"Tel-Jilad Wolf"},
		nil, []string{"p1:Tel-Jilad Wolf"}, [][2]string{{"p0:Phyrexian Walker", "p1:Tel-Jilad Wolf"}})
	res, ab := abBlockCheckpoint(t, art, 2, "p0:Phyrexian Walker")
	if w, ok := findPerm(t, res.Snapshots, 2, "p0:Phyrexian Walker"); !ok || !strings.Contains(strings.Join(w.Types, ","), "Artifact") {
		t.Fatalf("precondition: the blocker must be an artifact creature: %+v", w)
	}
	wantAbilities(t, ab, "p1:Tel-Jilad Wolf")
}

// TestAttackerBlockedOrcSpelling: Dwarven Soldier's ValidBlocker$ Orc.
func TestAttackerBlockedOrcSpelling(t *testing.T) {
	plain := abScenario("dwarven-soldier-plain", []string{"Grizzly Bears"}, []string{"Dwarven Soldier"},
		nil, []string{"p1:Dwarven Soldier"}, [][2]string{{"p0:Grizzly Bears", "p1:Dwarven Soldier"}})
	_, ab := abBlockCheckpoint(t, plain, 2, "p0:Grizzly Bears")
	wantAbilities(t, ab)

	orc := abScenario("dwarven-soldier-orc", []string{"Krumar Bond-Kin"}, []string{"Dwarven Soldier"},
		nil, []string{"p1:Dwarven Soldier"}, [][2]string{{"p0:Krumar Bond-Kin", "p1:Dwarven Soldier"}})
	_, ab = abBlockCheckpoint(t, orc, 2, "p0:Krumar Bond-Kin")
	wantAbilities(t, ab, "p1:Dwarven Soldier")
}

// TestAttackerBlockedFlyingFilterOnBlockerHalf: Woolly Spider blocking a
// non-flyer fires nothing (ValidCard$ Creature.withFlying is read against the
// attacker); blocking a flyer fires once.
func TestAttackerBlockedFlyingFilterOnBlockerHalf(t *testing.T) {
	ground := abScenario("woolly-spider-ground", []string{"Woolly Spider"}, []string{"Grizzly Bears"},
		nil, []string{"p1:Grizzly Bears"}, [][2]string{{"p0:Woolly Spider", "p1:Grizzly Bears"}})
	_, ab := abBlockCheckpoint(t, ground, 2, "p0:Woolly Spider")
	wantAbilities(t, ab)

	flyer := abScenario("woolly-spider-flyer", []string{"Woolly Spider"}, []string{"Wind Drake"},
		nil, []string{"p1:Wind Drake"}, [][2]string{{"p0:Woolly Spider", "p1:Wind Drake"}})
	_, ab = abBlockCheckpoint(t, flyer, 2, "p0:Woolly Spider")
	wantAbilities(t, ab, "p0:Woolly Spider")
}

// TestAttackerBlockedCapturesTheBlockerRole: Righteous Indignation's
// Defined$ TriggeredBlockerLKICopy needs TriggerBlocker, which this mode
// never set. The blocking creature (not the attacker, not the enchantment)
// gets +1/+1.
func TestAttackerBlockedCapturesTheBlockerRole(t *testing.T) {
	sc := abScenario("righteous-indignation-pump", []string{"Righteous Indignation", "Grizzly Bears"}, []string{"Barony Vampire"},
		nil, []string{"p1:Barony Vampire"}, [][2]string{{"p0:Grizzly Bears", "p1:Barony Vampire"}})
	sc = strings.TrimSuffix(sc, "]}") + `,{"op":"resolve","seat":0}]}`
	res, ab := abBlockCheckpoint(t, sc, 2, "p0:Grizzly Bears")
	wantAbilities(t, ab, "p0:Righteous Indignation")
	if p, _ := findPerm(t, res.Snapshots, 2, "p0:Grizzly Bears"); p.PT != "2/2" {
		t.Fatalf("precondition: blocker PT before the trigger = %q, want 2/2", p.PT)
	}
	last := len(res.Snapshots) - 1
	bear, _ := findPerm(t, res.Snapshots, last, "p0:Grizzly Bears")
	vamp, _ := findPerm(t, res.Snapshots, last, "p1:Barony Vampire")
	if bear.PT != "3/3" {
		t.Fatalf("blocker PT after the trigger = %q, want 3/3 (vampire %q)", bear.PT, vamp.PT)
	}
	if vamp.PT != "3/2" {
		t.Fatalf("attacker PT after the trigger = %q, want unchanged 3/2", vamp.PT)
	}
}

// TestAttackerBlockedBlockerAmount: Seifer's ValidBlockerAmount$ GE2 fires
// when a creature attacking one of Seifer's controller's opponents is blocked
// by two creatures and not by one. Seifer sits on the ATTACKING side (p1), so
// the attacker "attacks one of your opponents".
func TestAttackerBlockedBlockerAmount(t *testing.T) {
	p0 := []string{"Grizzly Bears", "Hill Giant"}
	p1 := []string{"Seifer, Balamb Rival", "Barony Vampire"}
	one := abScenario("seifer-one-blocker", p0, p1,
		nil, []string{"p1:Barony Vampire"}, [][2]string{{"p0:Hill Giant", "p1:Barony Vampire"}})
	_, ab := abBlockCheckpoint(t, one, 2, "p0:Hill Giant")
	wantAbilities(t, ab)

	two := abScenario("seifer-two-blockers", p0, p1,
		nil, []string{"p1:Barony Vampire"},
		[][2]string{{"p0:Hill Giant", "p1:Barony Vampire"}, {"p0:Grizzly Bears", "p1:Barony Vampire"}})
	_, ab = abBlockCheckpoint(t, two, 2, "p0:Hill Giant", "p0:Grizzly Bears")
	wantAbilities(t, ab, "p1:Seifer, Balamb Rival")
}
