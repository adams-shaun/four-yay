package rules

// trigger_blocks_blocker_test.go pins the blocker half of Forge Mode$
// AttackerBlockedByCreature (level-B census ticket cli-20261009T105445Z:
// D8b stack-content class, Skewer Slinger's combat#0.block divergence row and
// its trigger#0.0 generation skip). "Whenever CARDNAME blocks or becomes
// blocked by a creature" used to fire only the become-blocked half: the
// queue's hard pr[0]==source gate made every blocker-side line inert, so
// Skewer Slinger's damage, Witherscale Wurm's wither and Wooden Stake's
// destroy never happened. The walk now selects pairs by the trigger's own
// specs (ValidCard$ against the attacker, ValidBlocker$ against the blocker)
// and the queue captures role-correct referents (triggerAnchoredAtBlocker):
// a blocker-side trigger remembers the ATTACKER, so Defined$
// TriggeredAttackerLKICopy names the creature it blocked.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// blockerCombatScenario is the shared combat shape of the generated
// combat#0.block probes: the named blocker blocks the attacking Grizzly
// Bears, then the combat resolves.
func blockerCombatScenario(name, blocker string) string {
	return `{"name":"` + name + `","cr":["509.1a"],"why":"blocker half fires","` +
		`setup":{"p0":{"battlefield":["` + blocker + `"]},"p1":{"battlefield":["Grizzly Bears"]}},` +
		`"steps":[` +
		`{"op":"attack","seat":1,"attackers":["p1:Grizzly Bears"],"defender":"p0"},` +
		`{"op":"block","seat":0,"blocks":[["p0:` + blocker + `","p1:Grizzly Bears"]]},` +
		`{"op":"pass_to","seat":0,"step":"main2","active":"p1"},` +
		`{"op":"resolve","seat":0}]}`
}

// stackAbilities lists a snapshot's on-stack ability source refs.
func stackAbilities(t *testing.T, snaps []OracleSnapshot, i int) []string {
	t.Helper()
	if i < 0 || i >= len(snaps) {
		t.Fatalf("no snapshot %d (have %d)", i, len(snaps))
	}
	var out []string
	for _, s := range snaps[i].Stack {
		if s.Kind == "ability" {
			out = append(out, s.Source)
		}
	}
	return out
}

func findPerm(t *testing.T, snaps []OracleSnapshot, i int, ref string) (OracleSnapPerm, bool) {
	t.Helper()
	if i < 0 || i >= len(snaps) {
		t.Fatalf("no snapshot %d (have %d)", i, len(snaps))
	}
	for _, p := range snaps[i].Permanents {
		if p.Ref == ref {
			return p, true
		}
	}
	return OracleSnapPerm{}, false
}

// TestSkewerSlingerBlockTriggerDealsDamage pins the row's own card: when
// Skewer Slinger blocks, its "blocks or becomes blocked" ability is on the
// stack at the declare-blockers priority ask (what the XMage side records)
// and the attacker dies to the trigger's damage plus combat damage.
func TestSkewerSlingerBlockTriggerDealsDamage(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(blockerCombatScenario("skewer-blocker-half", "Skewer Slinger")))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	// Precondition: at the block checkpoint the pair is really formed and
	// the blocker's trigger is on the stack.
	block := 2
	if got := res.Snapshots[block].Step; got != "declare-blockers" {
		t.Fatalf("snapshot %d step = %q, want declare-blockers", block, got)
	}
	if _, ok := findPerm(t, res.Snapshots, block, "p1:Grizzly Bears"); !ok {
		t.Fatal("Grizzly Bears not on the battlefield at the block checkpoint")
	}
	ab := stackAbilities(t, res.Snapshots, block)
	if len(ab) != 1 || ab[0] != "p0:Skewer Slinger" {
		t.Fatalf("block checkpoint abilities = %v, want [p0:Skewer Slinger]", ab)
	}
	// The trigger resolves in the post-block priority round; the 1 damage
	// plus the combat damage kills the 2/2 attacker.
	last := len(res.Snapshots) - 1
	if _, ok := findPerm(t, res.Snapshots, last, "p1:Grizzly Bears"); ok {
		t.Fatalf("attacker survived: trigger damage never landed (snapshots %v)", res.Snapshots[last].Permanents)
	}
	if _, ok := findPerm(t, res.Snapshots, last, "p0:Skewer Slinger"); !ok {
		t.Fatal("blocker lost the block it should survive")
	}
}

// TestWitherscaleWurmBlockerHalfGrantsWither pins the wither spelling of the
// same half: "Whenever CARDNAME blocks or becomes blocked by a creature, that
// creature gains wither" pumps the ATTACKER, whose combat damage then lands
// on the blocker as -1/-1 counters instead of damage.
func TestWitherscaleWurmBlockerHalfGrantsWither(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(blockerCombatScenario("witherscale-blocker-half", "Witherscale Wurm")))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	block := 2
	ab := stackAbilities(t, res.Snapshots, block)
	if len(ab) != 1 || ab[0] != "p0:Witherscale Wurm" {
		t.Fatalf("block checkpoint abilities = %v, want [p0:Witherscale Wurm]", ab)
	}
	last := len(res.Snapshots) - 1
	wurm, ok := findPerm(t, res.Snapshots, last, "p0:Witherscale Wurm")
	if !ok {
		t.Fatal("Witherscale Wurm missing after combat")
	}
	if got := wurm.Counters["M1M1"]; got != 2 {
		t.Fatalf("Witherscale Wurm -1/-1 counters = %v (%d), want 2 from the attacker's withered damage", wurm.Counters, got)
	}
	if wurm.Damage != 0 {
		t.Fatalf("Witherscale Wurm damage = %d, want 0 (wither routes it to counters)", wurm.Damage)
	}
}

// TestWoodenStakeEquippedBlockerHalfDestroysAttacker pins the attachment
// spelling of the same half: the trigger lives on the EQUIPMENT and its
// ValidBlocker$ Card.AttachedBy anchor names the bearer, so the destroy
// referent is the blocked Vampire attacker. Barony Vampire has no evasion,
// so the block is legal.
func TestWoodenStakeEquippedBlockerHalfDestroysAttacker(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sc := `{"name":"wooden-stake-blocker-half","cr":["509.1a"],"why":"equipped blocker half fires",` +
		`"setup":{"p0":{"battlefield":["Wooden Stake","Grizzly Bears"]},"p1":{"battlefield":["Barony Vampire"]}},` +
		`"steps":[` +
		`{"op":"attach","card":"p0:Wooden Stake","attached_to":"p0:Grizzly Bears"},` +
		`{"op":"attack","seat":1,"attackers":["p1:Barony Vampire"],"defender":"p0"},` +
		`{"op":"block","seat":0,"blocks":[["p0:Grizzly Bears","p1:Barony Vampire"]]},` +
		`{"op":"pass_to","seat":0,"step":"main2","active":"p1"},` +
		`{"op":"resolve","seat":0}]}`
	res, err := RunOracleScenarioJSON(reg, []byte(sc))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	block := 3
	if got := res.Snapshots[block].Step; got != "declare-blockers" {
		t.Fatalf("snapshot %d step = %q, want declare-blockers", block, got)
	}
	// Precondition: the pair is formed, so the missing trigger below is the
	// blocker half and not a block that never happened.
	if p, ok := findPerm(t, res.Snapshots, block, "p0:Grizzly Bears"); !ok || !p.Blocking {
		t.Fatalf("bearer not blocking at the block checkpoint: %+v", p)
	}
	ab := stackAbilities(t, res.Snapshots, block)
	if len(ab) != 1 || ab[0] != "p0:Wooden Stake" {
		t.Fatalf("block checkpoint abilities = %v, want [p0:Wooden Stake]", ab)
	}
	// The trigger resolves in the post-block priority round, so the Vampire
	// dies to the destroy before combat damage and the blocker survives.
	last := len(res.Snapshots) - 1
	if _, ok := findPerm(t, res.Snapshots, last, "p1:Barony Vampire"); ok {
		t.Fatal("Barony Vampire survived: the equipped blocker half never destroyed it")
	}
	if g := res.Snapshots[last].Players[1].Graveyard; len(g) == 0 || g[0] != "Barony Vampire" {
		t.Fatalf("p1 graveyard = %v, want the destroyed Barony Vampire", g)
	}
	if _, ok := findPerm(t, res.Snapshots, last, "p0:Grizzly Bears"); !ok {
		t.Fatal("bearer died before the trigger could resolve (destroy must precede combat damage)")
	}
}
