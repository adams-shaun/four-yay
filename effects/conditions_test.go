package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// conditionBoard builds a 2-seat game with an instant, a sorcery and a land
// on it, and returns the host plus their ids (bolt, ritual, mountain, then
// the source creature).
func conditionBoard(t *testing.T) (*fakeHost, []state.ObjID) {
	t.Helper()
	h := newHost(t, 2)
	bolt := mkCard(t, "Name:Bolt\nTypes:Instant\nOracle:3 damage\n")
	ritual := mkCard(t, "Name:Ritual\nTypes:Sorcery\nOracle:add RRR\n")
	mtn := mkCard(t, "Name:Mountain\nTypes:Land\nOracle:x\n")
	fix := mkCard(t, "Name:Fixture\nTypes:Creature\nPT:1/1\nOracle:x\n")
	var ids []state.ObjID
	ids = append(ids, h.g.AddObject(bolt, 0).ID)
	ids = append(ids, h.g.AddObject(ritual, 0).ID)
	ids = append(ids, h.g.AddObject(mtn, 0).ID)
	ids = append(ids, h.g.AddObject(fix, 0).ID)
	return h, ids
}

// TestConditionGateDelverShape pins the exact shape Delver of Secrets
// transforms on: ConditionDefined$ Remembered + ConditionPresent$
// Card.Instant,Card.Sorcery + ConditionCompare$ EQ1 is met exactly when the
// remembered set holds exactly one instant/sorcery (task
// fb-20260914T033246Z-3f1cc033, defect 2).
func TestConditionGateDelverShape(t *testing.T) {
	h, ids := conditionBoard(t)
	sa := sa(t, "DB$ SetState | Defined$ Self | Mode$ Transform | ConditionDefined$ Remembered | ConditionPresent$ Card.Instant,Card.Sorcery | ConditionCompare$ EQ1")
	ctx := &Ctx{Controller: 0, Source: ids[3], Remembered: []state.Target{{Obj: ids[0]}}}
	if met, resolved := conditionMet(h, ctx, sa); !met || !resolved {
		t.Fatalf("one remembered instant: met=%v resolved=%v, want true true", met, resolved)
	}
	ctx.Remembered = []state.Target{{Obj: ids[2]}} // a land
	if met, resolved := conditionMet(h, ctx, sa); met || !resolved {
		t.Fatalf("one remembered land: met=%v resolved=%v, want false true", met, resolved)
	}
	ctx.Remembered = []state.Target{{Obj: ids[0]}, {Obj: ids[1]}} // two spells
	if met, resolved := conditionMet(h, ctx, sa); met || !resolved {
		t.Fatalf("two remembered spells vs EQ1: met=%v resolved=%v, want false true", met, resolved)
	}
	ctx.Remembered = nil
	if met, resolved := conditionMet(h, ctx, sa); met || !resolved {
		t.Fatalf("nothing remembered: met=%v resolved=%v, want false true", met, resolved)
	}
	// A player entry in Remembered never matches a Card spec.
	ctx.Remembered = []state.Target{{Player: 1, IsPlayer: true}}
	if met, resolved := conditionMet(h, ctx, sa); met || !resolved {
		t.Fatalf("only a player remembered: met=%v resolved=%v, want false true", met, resolved)
	}
}

// TestConditionGateCompareOperators pins the literal operator set and the
// no-Compare default (presence: count >= 1).
func TestConditionGateCompareOperators(t *testing.T) {
	h, ids := conditionBoard(t)
	base := "DB$ Pump | ConditionDefined$ Remembered | ConditionPresent$ Card | ConditionCompare$ "
	ctx := &Ctx{Controller: 0, Source: ids[3], Remembered: []state.Target{{Obj: ids[0]}, {Obj: ids[1]}}}
	for _, tc := range []struct {
		cmp  string
		want bool
	}{
		{"EQ2", true}, {"EQ1", false}, {"NE2", false}, {"NE1", true},
		{"GE2", true}, {"GE3", false}, {"GT1", true}, {"GT2", false},
		{"LT3", true}, {"LT2", false}, {"LE2", true}, {"LE1", false},
	} {
		gate := sa(t, base+tc.cmp)
		if met, resolved := conditionMet(h, ctx, gate); !resolved || met != tc.want {
			t.Errorf("Compare %s over 2 remembered: met=%v resolved=%v, want met=%v", tc.cmp, met, resolved, tc.want)
		}
	}
	// Forge spells the compare without a space; the spaced spelling parses too.
	gate := sa(t, "DB$ Pump | ConditionDefined$ Remembered | ConditionPresent$ Card | ConditionCompare$ EQ 2")
	if met, resolved := conditionMet(h, ctx, gate); !met || !resolved {
		t.Fatalf("spaced EQ 2: met=%v resolved=%v, want true true", met, resolved)
	}
	// No Compare key: presence (>= 1 of the Present spec).
	gate = sa(t, "DB$ Pump | ConditionDefined$ Remembered | ConditionPresent$ Card.Instant")
	if met, resolved := conditionMet(h, ctx, gate); !met || !resolved {
		t.Fatalf("Present with no Compare: met=%v resolved=%v, want true true", met, resolved)
	}
	// Neither Present nor Compare: the defined group is non-empty.
	gate = sa(t, "DB$ Pump | ConditionDefined$ Remembered")
	if met, resolved := conditionMet(h, ctx, gate); !met || !resolved {
		t.Fatalf("Defined only: met=%v resolved=%v, want true true", met, resolved)
	}
}

// TestConditionGateUnresolvedShapesReportFalse pins the conditionMet-level
// boundary: a gate this build cannot evaluate reports resolved=false, and a
// sub with NO Condition* key at all reports met=true, resolved=false (not
// gated). What Resolve's walk does with each is the census classifier's call
// (effects/conditions.go's scope comment): a classifier-named shape is skipped
// with a Note, the residual unclassified shape still runs.
func TestConditionGateUnresolvedShapesReportFalse(t *testing.T) {
	h, _ := conditionBoard(t)
	other := "DB$ Pump | ConditionDefined$ ChosenCard | ConditionPresent$ Creature | ConditionCompare$ EQ1"
	if _, resolved := conditionMet(h, &Ctx{Controller: 0}, sa(t, other)); resolved {
		t.Fatal("ConditionDefined$ ChosenCard resolved — out of the scoped shape")
	}
	checkSVar := "DB$ Pump | ConditionDefined$ Remembered | ConditionCheckSVar$ X | ConditionSVarCompare$ EQ1"
	if _, resolved := conditionMet(h, &Ctx{Controller: 0}, sa(t, checkSVar)); resolved {
		t.Fatal("ConditionCheckSVar$ resolved — out of the scoped shape")
	}
	// ConditionNotPresent$ over the REMEMBERED group is resolved as of the
	// Rakdos-params task (the Ajani/Ravenous/Fallaji "the remembered card is
	// gone" shapes); Targeted is now resolved too (conditionMet's Targeted
	// group). The still-unresolved NOT-PRESENT shape is a defined group this
	// build cannot enumerate (ChosenCard, lodestone_bauble's).
	notPresent := "DB$ Pump | ConditionDefined$ ChosenCard | ConditionNotPresent$ Card"
	if _, resolved := conditionMet(h, &Ctx{Controller: 0}, sa(t, notPresent)); resolved {
		t.Fatal("ConditionNotPresent$ over ChosenCard resolved — out of the scoped shape")
	}
	unknownPred := "DB$ Pump | ConditionDefined$ Remembered | ConditionPresent$ Card.DefinedTargeted"
	if _, resolved := conditionMet(h, &Ctx{Controller: 0}, sa(t, unknownPred)); resolved {
		t.Fatal("an unknown predicate in Present resolved — would count a false zero")
	}
	nonLiteral := "DB$ Pump | ConditionDefined$ Remembered | ConditionPresent$ Card | ConditionCompare$ GTX"
	if _, resolved := conditionMet(h, &Ctx{Controller: 0}, sa(t, nonLiteral)); resolved {
		t.Fatal("a non-literal Compare resolved — out of the scoped shape")
	}
	// No Condition* key at all: not gated, run.
	if met, resolved := conditionMet(h, &Ctx{Controller: 0}, sa(t, "DB$ Pump")); resolved || !met {
		t.Fatalf("ungated sub: met=%v resolved=%v, want true false", met, resolved)
	}
}

// TestConditionGateTargetedGroup pins the ConditionDefined$ Targeted group
// (Stalking Leonin's `ConditionDefined$ Targeted | ConditionPresent$
// Card.ChosenCtrl`): the group is the resolving ability's own answered
// targets -- Ctx.PickedTargets while a pre-asked body dispatches, else
// Ctx.Targets -- exactly the channels Defined$ Targeted reads. A chosen
// player that controls the target admits; one that does not denies; an empty
// target list is a resolved zero, never the fail-open an absent binding gets.
func TestConditionGateTargetedGroup(t *testing.T) {
	h, ids := conditionBoard(t)
	// ids[3] is the Fixture, controlled by seat 0. Record seat 0 as the
	// source's secretly chosen player.
	h.g.Obj(ids[3]).Chosen = []state.Target{{Player: 0, IsPlayer: true}}
	gate := sa(t, "DB$ Pump | ConditionDefined$ Targeted | ConditionPresent$ Card.ChosenCtrl")

	// The target is controlled by the chosen player: met, resolved.
	ctx := &Ctx{Controller: 0, Source: ids[3], Targets: []state.Target{{Obj: ids[3]}}}
	if met, resolved := conditionMet(h, ctx, gate); !met || !resolved {
		t.Fatalf("target controlled by the chosen player: met=%v resolved=%v, want true true", met, resolved)
	}

	// The target is controlled by another seat: resolved false, not fail-open.
	h.g.Obj(ids[3]).Chosen = []state.Target{{Player: 1, IsPlayer: true}}
	if met, resolved := conditionMet(h, ctx, gate); met || !resolved {
		t.Fatalf("target not controlled by the chosen player: met=%v resolved=%v, want false true", met, resolved)
	}

	// No chosen player at all: the predicate fails closed, resolved false.
	h.g.Obj(ids[3]).Chosen = nil
	if met, resolved := conditionMet(h, ctx, gate); met || !resolved {
		t.Fatalf("no chosen player: met=%v resolved=%v, want false true", met, resolved)
	}

	// No targets chosen: a resolved zero, never fail-open.
	h.g.Obj(ids[3]).Chosen = []state.Target{{Player: 0, IsPlayer: true}}
	if met, resolved := conditionMet(h, &Ctx{Controller: 0, Source: ids[3]}, gate); met || !resolved {
		t.Fatalf("no targets: met=%v resolved=%v, want false true (resolved zero)", met, resolved)
	}

	// Ctx.PickedTargets (the pre-asked body channel) outranks Ctx.Targets.
	// The Mountain (ids[2]) is moved to seat 1 so it is NOT controlled by the
	// chosen player (seat 0), while the fixture (ids[3]) is.
	h.g.Obj(ids[2]).Controller = 1
	picked := &Ctx{Controller: 0, Source: ids[3], Targets: []state.Target{{Obj: ids[3]}}, PickedTargets: []state.Target{{Obj: ids[2]}}}
	if met, resolved := conditionMet(h, picked, gate); met || !resolved {
		t.Fatalf("PickedTargets outrank: met=%v resolved=%v, want false true", met, resolved)
	}
}

// TestConditionGatePresentWithoutDefinedResolvesBattlefield pins the
// task fb-9d2338cc scope: a ConditionPresent$ WITHOUT a ConditionDefined$
// now resolves against Forge's default group, the whole battlefield. The
// entering/replaced object (c.Replaced) is excluded, so a fast land's
// GT2-on-Land.YouCtrl counts OTHER lands — the oracle's "two or fewer other
// lands" — not the land itself (which for the Updated replacement shape is
// already on the battlefield when the gate runs). Out-of-scope shapes stay
// unresolved: a bare ConditionCompare$ with no Present/no Defined names no
// count group, and an unknown predicate still fails closed rather than
// counting a false zero.
func TestConditionGatePresentWithoutDefinedResolvesBattlefield(t *testing.T) {
	h, ids := conditionBoard(t)
	// conditionBoard lands all four objects in the library (AddObject's
	// default zone); move them to the battlefield so the group counts them.
	for _, id := range ids {
		h.g.Obj(id).Zone = state.ZBattlefield
	}

	// ids[2] is the Mountain (a Land), ids[3] the Fixture (a Creature).
	// No replaced object: one Land on the battlefield, EQ1 -> met.
	gate := sa(t, "DB$ Tap | Defined$ Self | ETB$ True | ConditionPresent$ Land | ConditionCompare$ EQ1")
	if met, resolved := conditionMet(h, &Ctx{Controller: 0, Source: ids[3]}, gate); !met || !resolved {
		t.Fatalf("Land EQ1, one Land on battlefield: met=%v resolved=%v, want true true", met, resolved)
	}

	// The same gate with the Mountain as the replaced (entering) object: the
	// exclusion drops it from the count, so EQ1 is unmet but RESOLVED.
	if met, resolved := conditionMet(h, &Ctx{Controller: 0, Source: ids[3], Repl: ReplacementInputs{Replaced: ids[2]}}, gate); met || !resolved {
		t.Fatalf("Land EQ1 with the Land itself replaced: met=%v resolved=%v, want false true (self-exclusion)", met, resolved)
	}

	// Presence (no Compare key): at least one Creature matches.
	gate = sa(t, "DB$ Tap | Defined$ Self | ETB$ True | ConditionPresent$ Creature")
	if met, resolved := conditionMet(h, &Ctx{Controller: 0, Source: ids[3]}, gate); !met || !resolved {
		t.Fatalf("Creature presence, one Creature on battlefield: met=%v resolved=%v, want true true", met, resolved)
	}

	// YouCtrl scopes to the gate's controller, not some other seat's lands.
	cond := h.g.Obj(ids[2]).Controller
	if cond != 0 {
		t.Fatalf("Mountain controller = %d, want 0", cond)
	}

	// Out of scope: bare Compare with no Present / no Defined names no group.
	bareCmp := sa(t, "DB$ Pump | ConditionCompare$ GE1")
	if _, resolved := conditionMet(h, &Ctx{Controller: 0, Source: 4}, bareCmp); resolved {
		t.Fatal("a bare ConditionCompare$ (no Present, no Defined) resolved — it names no count group")
	}

	// Out of scope: an unknown predicate in Present fails closed (would count
	// a false zero). DefinedTargeted is the positive twin of the implemented
	// NotDefinedTargeted and remains unknown here.
	unknown := sa(t, "DB$ Pump | ConditionPresent$ Card.DefinedTargeted")
	if _, resolved := conditionMet(h, &Ctx{Controller: 0, Source: 4}, unknown); resolved {
		t.Fatal("an unknown predicate in Present resolved — would count a false zero")
	}
}

// TestResolveSkipsAnUnmetConditionGate pins the chain-walk: a gated sub
// whose condition is not met is SKIPPED and the chain continues past it —
// Delver's DBTransform must not flip when the gate fails — while a met gate
// runs the sub.
func TestResolveSkipsAnUnmetConditionGate(t *testing.T) {
	h, ids := conditionBoard(t)
	// A two-faced source so SetState has something to flip.
	src := mkCard(t, "Name:Front\nTypes:Creature\nPT:1/1\nOracle:x\n\nALTERNATE\n\nName:Back\nTypes:Creature\nPT:3/3\nOracle:x\n")
	srcID := h.g.AddObject(src, 0).ID
	chain := "DB$ SetState | Defined$ Self | Mode$ Transform | ConditionDefined$ Remembered | ConditionPresent$ Card.Instant,Card.Sorcery | ConditionCompare$ EQ1 | SubAbility$ DBCleanup\nSVar:DBCleanup:DB$ Cleanup | ClearRemembered$ True"
	sa := sa(t, chain)

	// Land remembered: no FlipFace, chain continues (the Cleanup note runs).
	ctx := &Ctx{Controller: 0, Source: srcID, Remembered: []state.Target{{Obj: ids[2]}}}
	Resolve(h, ctx, sa)
	flips := 0
	for _, e := range h.log {
		if e.Kind == events.FlipFace {
			flips++
		}
	}
	if flips != 0 {
		t.Fatalf("the unmet gate still flipped: %d FlipFace events", flips)
	}

	// Instant remembered: exactly one flip.
	h2, ids2 := conditionBoard(t)
	srcID2 := h2.g.AddObject(src, 0).ID
	ctx2 := &Ctx{Controller: 0, Source: srcID2, Remembered: []state.Target{{Obj: ids2[0]}}}
	Resolve(h2, ctx2, sa)
	flips = 0
	for _, e := range h2.log {
		if e.Kind == events.FlipFace {
			flips++
		}
	}
	if flips != 1 {
		t.Fatalf("the met gate did not flip exactly once: %d FlipFace events", flips)
	}
}

// TestConditionBareDeliriumReportsTheCensus pins the bare-Condition$
// Delirium arm against the deck card that needed it (Descend upon the
// Sinful's DB$ Token): the gate is met exactly when the resolving
// controller's graveyard census holds, and it RESOLVES (so an unmet gate
// SKIPS the sub) rather than falling through unresolved to the run-anyway
// default the unresolved bare values take. The census itself (four or more
// distinct core card types) is pinned end to end in rules against the real
// engine; this double-backed pin proves the gate's wiring, not the census.
func TestConditionBareDeliriumReportsTheCensus(t *testing.T) {
	h, ids := conditionBoard(t)
	gate := sa(t, "DB$ Draw | Condition$ Delirium")
	ctx := &Ctx{Controller: 0, Source: ids[3]}

	// Under the census: resolved and unmet, so the sub is skipped.
	if met, resolved := conditionMet(h, ctx, gate); !resolved || met {
		t.Fatalf("no delirium: met=%v resolved=%v, want false true", met, resolved)
	}

	// Over the census: resolved and met.
	h.delirium = true
	if met, resolved := conditionMet(h, ctx, gate); !resolved || !met {
		t.Fatalf("delirium: met=%v resolved=%v, want true true", met, resolved)
	}
}

// TestConditionBareBlessingReadsTheLatch pins the bare-Condition$ Blessing
// arm against the three corpus carriers the brief names (ocelot_pride /
// a-ocelot_pride's DB$ CopyPermanent, the_golden_city_of_orazca's DB$ Draw):
// the gate is met exactly when the resolving controller's seat holds the
// city's blessing -- the one-way latch state.Player.Blessing that
// events.Apply's BlessingChange fold writes -- and, crucially, it RESOLVES
// (so an unmet gate SKIPS the sub) rather than falling through unresolved to
// the run-anyway default.
func TestConditionBareBlessingReadsTheLatch(t *testing.T) {
	h, ids := conditionBoard(t)
	gate := sa(t, "DB$ Draw | Condition$ Blessing")
	ctx := &Ctx{Controller: 0, Source: ids[3]}

	// Unblessed: resolved and unmet, so the sub is skipped.
	if met, resolved := conditionMet(h, ctx, gate); !resolved || met {
		t.Fatalf("unblessed seat: met=%v resolved=%v, want false true", met, resolved)
	}

	// Blessed (the latch the fold sets): resolved and met.
	h.g.Players[0].Blessing = true
	if met, resolved := conditionMet(h, ctx, gate); !resolved || !met {
		t.Fatalf("blessed seat: met=%v resolved=%v, want true true", met, resolved)
	}

	// The latch is per-seat: the other controller stays unmet.
	ctx.Controller = 1
	if met, resolved := conditionMet(h, ctx, gate); !resolved || met {
		t.Fatalf("unblessed other seat: met=%v resolved=%v, want false true", met, resolved)
	}
}
