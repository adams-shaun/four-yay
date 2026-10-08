package mzenc

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// hasAll fails unless every id of want is in got.
func hasAll(t *testing.T, what string, got, want map[int32]struct{}) {
	t.Helper()
	for id := range want {
		if _, ok := got[id]; !ok {
			t.Fatalf("%s: expected id %d missing", what, id)
		}
	}
}

// absent fails if got contains any id that build adds only when leaf is true:
// the chain of ancestor subtrees (Player#1, Battlefield#1, ...) is shared with
// the walk's own and must not count, so the ids of build(f, false) are removed.
func absent(t *testing.T, what string, got map[int32]struct{}, build func(f *Node, leaf bool)) {
	t.Helper()
	with := idsFor(func(f *Node) { build(f, true) })
	without := idsFor(func(f *Node) { build(f, false) })
	for id := range with {
		if _, shared := without[id]; shared {
			continue
		}
		if _, ok := got[id]; ok {
			t.Fatalf("%s: unexpected id %d present", what, id)
		}
	}
}

// onePerm walks a one-permanent battlefield for seat 0 (opponent seat 1 empty,
// LandDropSpent unset) and returns the id set.
func onePerm(perms ...view.CardView) map[int32]struct{} {
	return ProcessState(view.View{Players: []view.PlayerView{{ID: 0, Battlefield: perms}, {ID: 1}}}, nil, 0, 0, "x")
}

// permNode rebuilds the Player/Battlefield/<name> node a permanent hangs from.
func permNode(f *Node, name string) *Node {
	return f.SubFeatures("Player", true).SubFeatures("Battlefield", true).SubFeatures(name, true)
}

func TestColorsAndSubtypes(t *testing.T) {
	got := onePerm(view.CardView{ID: 1, Name: "Wolf Ally", Types: "Legendary Creature Wolf Warrior", ManaCost: "W/U G"})
	want := idsFor(func(f *Node) {
		n := permNode(f, "Wolf Ally")
		n.AddFeature("creature")
		n.AddFeature("WhiteCard")
		n.AddFeature("GreenCard")
		n.AddFeature("BlueCard")
		n.AddFeature("MultiColored")
		n.AddFeature("wolf")
		n.AddFeature("warrior")
	})
	hasAll(t, "colours+subtypes", got, want)
	for _, nope := range []string{"RedCard", "ColorlessCard"} {
		absent(t, nope, got, func(f *Node, leaf bool) {
			n := permNode(f, "Wolf Ally")
			if leaf {
				n.AddFeature(nope)
			}
		})
	}
	// a costless land is ColorlessCard and not MultiColored.
	got = onePerm(view.CardView{ID: 2, Name: "Forest", Types: "Basic Land Forest"})
	hasAll(t, "colourless land", got, idsFor(func(f *Node) {
		n := permNode(f, "Forest")
		n.AddFeature("ColorlessCard")
		n.AddFeature("forest")
	}))
	absent(t, "land MultiColored", got, func(f *Node, leaf bool) {
		if n := permNode(f, "Forest"); leaf {
			n.AddFeature("MultiColored")
		}
	})
}

func TestCanAttackCanBlock(t *testing.T) {
	cases := []struct {
		name          string
		cv            view.CardView
		attack, block bool
	}{
		{"ready", view.CardView{Types: "Creature"}, true, true},
		{"sick", view.CardView{Types: "Creature", SummonSick: true}, false, true},
		{"sick haste", view.CardView{Types: "Creature", SummonSick: true, Keywords: []string{"Haste"}}, true, true},
		{"tapped", view.CardView{Types: "Creature", Tapped: true}, false, false},
		{"defender", view.CardView{Types: "Creature", Keywords: []string{"Defender"}}, false, true},
	}
	for _, c := range cases {
		c.cv.ID, c.cv.Name = 1, "C"
		got := onePerm(c.cv)
		for _, chk := range []struct {
			what string
			on   bool
		}{{"CanAttack", c.attack}, {"CanBlock", c.block}} {
			if chk.on {
				hasAll(t, c.name+" "+chk.what, got, idsFor(func(f *Node) { permNode(f, "C").AddFeature(chk.what) }))
			} else {
				absent(t, c.name+" "+chk.what, got, func(f *Node, leaf bool) {
					if n := permNode(f, "C"); leaf {
						n.AddFeature(chk.what)
					}
				})
			}
		}
	}
	// a non-creature never gets either.
	absent(t, "artifact", onePerm(view.CardView{ID: 1, Name: "C", Types: "Artifact"}), func(f *Node, leaf bool) {
		if n := permNode(f, "C"); leaf {
			n.AddFeature("CanAttack")
		}
	})
}

func TestPermanentFlags(t *testing.T) {
	got := onePerm(view.CardView{ID: 1, Name: "F", Types: "Creature",
		Flags: view.FlagSuspected | view.FlagMonstrous | view.FlagRingBearer | view.FlagLeftDoor})
	hasAll(t, "flags", got, idsFor(func(f *Node) {
		n := permNode(f, "F")
		n.AddFeature("suspected")
		n.AddFeature("RingBearer")
		n.AddFeature("Monstrous")
		n.AddFeature("Room-LeftDoor")
	}))
	for _, nope := range []string{"Renowned", "Room-RightDoor"} {
		absent(t, nope, got, func(f *Node, leaf bool) {
			if n := permNode(f, "F"); leaf {
				n.AddFeature(nope)
			}
		})
	}
}

func TestPermanentAttachmentFanOut(t *testing.T) {
	got := onePerm(
		view.CardView{ID: 1, Name: "Bear", Types: "Creature"},
		view.CardView{ID: 2, Name: "Pacifism", Types: "Enchantment Aura", ManaCost: "1 W", AttachedTo: 1},
	)
	want := idsFor(func(f *Node) {
		bf := f.SubFeatures("Player", true).SubFeatures("Battlefield", true)
		bear := bf.SubFeatures("Bear", true)
		att := bear.SubFeatures("attached", false).SubFeatures("Pacifism", true)
		att.AddFeature("aura")
		att.AddFeature("WhiteCard")
	})
	hasAll(t, "attached", got, want)
	// an unattached aura contributes no "attached" subtree.
	got = onePerm(view.CardView{ID: 1, Name: "Bear", Types: "Creature"}, view.CardView{ID: 2, Name: "Pacifism", Types: "Enchantment Aura"})
	absent(t, "unattached", got, func(f *Node, leaf bool) {
		if n := permNode(f, "Bear"); leaf {
			n.SubFeatures("attached", false)
		}
	})
}

func TestPlayerAttachments(t *testing.T) {
	v := view.View{Players: []view.PlayerView{
		{ID: 0},
		{ID: 1, Battlefield: []view.CardView{{ID: 5, Name: "Curse of Thirst", Types: "Enchantment Aura", AttachedToPlayer: true, AttachedPlayer: 0}}},
	}}
	got := ProcessState(v, nil, 0, 0, "x")
	hasAll(t, "player attachments", got, idsFor(func(f *Node) {
		a := f.SubFeatures("Player", true).SubFeatures("Attachments", false)
		a.AddFeature("aura")
	}))
}

func TestImprintedPairedAndPermanentExile(t *testing.T) {
	v := view.View{Players: []view.PlayerView{
		{ID: 0,
			Battlefield: []view.CardView{
				{ID: 1, Name: "Isochron Scepter", Types: "Artifact", Imprinted: []state.ObjID{9}},
				{ID: 2, Name: "Wolf", Types: "Creature", Paired: 3},
				{ID: 3, Name: "Owl", Types: "Creature", Paired: 2},
				{ID: 4, Name: "Banisher", Types: "Enchantment", ExiledCards: []state.ObjID{10}},
			},
			Exile: []view.CardView{
				{ID: 9, Name: "Shock", Types: "Instant", ManaCost: "R"},
				{ID: 10, Name: "Bear", Types: "Creature", ManaCost: "1 G"},
			}},
		{ID: 1},
	}}
	got := ProcessState(v, nil, 0, 0, "x")
	bf := func(f *Node) *Node { return f.SubFeatures("Player", true).SubFeatures("Battlefield", true) }
	hasAll(t, "imprinted", got, idsFor(func(f *Node) {
		c := bf(f).SubFeatures("Isochron Scepter", true).SubFeatures("imprinted", false).SubFeatures("Shock", true)
		c.AddFeature("instant")
		c.AddFeature("RedCard")
	}))
	hasAll(t, "paired", got, idsFor(func(f *Node) {
		bf(f).SubFeatures("Wolf", true).SubFeatures("paired", false).AddFeature("creature")
	}))
	hasAll(t, "permanent exile", got, idsFor(func(f *Node) {
		z := bf(f).SubFeatures("Banisher", true).SubFeatures("Banisher", false)
		z.SubFeatures("Bear", true).AddFeature("GreenCard")
	}))
	// an unresolvable imprinted id adds no "imprinted" subtree.
	v.Players[0].Battlefield[0].Imprinted = []state.ObjID{77}
	absent(t, "unresolved imprint", ProcessState(v, nil, 0, 0, "x"), func(f *Node, leaf bool) {
		if n := bf(f).SubFeatures("Isochron Scepter", true); leaf {
			n.SubFeatures("imprinted", false)
		}
	})
}

func TestTargetedByAndStackTargets(t *testing.T) {
	bear := view.CardView{ID: 1, Name: "Bear", Types: "Creature"}
	v := view.View{
		Players: []view.PlayerView{{ID: 0, Name: "Me", Battlefield: []view.CardView{bear}}, {ID: 1, Name: "Opp"}},
		Stack: []view.StackView{
			{ID: 50, Name: "Untargeted", Kind: "spell", Controller: 0},
			{ID: 51, Name: "Doom Blade", Kind: "spell", Controller: 1,
				Targets: []view.TargetView{{Obj: 1}, {IsPlayer: true, Player: 0}}},
		},
	}
	got := ProcessState(v, nil, 0, 0, "x")
	// TargetedBy: the untargeted spell does not advance the index (upstream
	// getSpellsTargetingPermanent), so Doom Blade is StackDepth 1, not 2.
	hasAll(t, "TargetedBy", got, idsFor(func(f *Node) {
		tb := permNode(f, "Bear").SubFeatures("TargetedBy", false)
		tb.SubFeatures("Doom Blade", true).AddNumericFeature("StackDepth", 1, true)
	}))
	absent(t, "TargetedBy depth", got, func(f *Node, leaf bool) {
		tb := permNode(f, "Bear").SubFeatures("TargetedBy", false).SubFeatures("Doom Blade", true)
		if leaf {
			tb.AddFeature("StackDepth@1") // the thermometer bit only a depth of 2 would set
		}
	})
	// StackTargets: "targets" under the stack object; the card target walks
	// processCard under its name, the player target is a bare named subtree.
	want := idsFor(func(f *Node) {
		st := f.SubFeatures("Stack", false)
		st.SubFeatures("Untargeted", true)
		d := st.SubFeatures("Doom Blade", true)
		tg := d.SubFeatures("targets", false)
		tg.SubFeatures("Bear", true).AddFeature("creature")
		tg.SubFeatures("Me", true)
	})
	hasAll(t, "StackTargets", got, want)
}

func TestPlayerCountersAndCanPlayLand(t *testing.T) {
	v := view.View{Players: []view.PlayerView{
		{ID: 0, Counters: map[string]int32{"poison": 3, "energy": 1}},
		{ID: 1, LandDropSpent: true},
	}}
	got := ProcessState(v, nil, 0, 0, "x")
	hasAll(t, "counters", got, idsFor(func(f *Node) {
		me := f.SubFeatures("Player", true)
		me.AddFeature("CanPlayLand")
		me.AddNumericFeature("poison", 3, true)
		me.AddNumericFeature("energy", 1, true)
	}))
	v.Players[0].LandDropSpent = true
	absent(t, "spent drop", ProcessState(v, nil, 0, 0, "x"), func(f *Node, leaf bool) {
		for _, name := range []string{"Player", "Opponent"} {
			if n := f.SubFeatures(name, true); leaf {
				n.AddFeature("CanPlayLand")
			}
		}
	})
}

func TestAttachmentCycleTerminates(t *testing.T) {
	// two permanents attached to each other (impossible in a real game) must
	// not recurse forever.
	_ = onePerm(
		view.CardView{ID: 1, Name: "A", Types: "Creature", AttachedTo: 2},
		view.CardView{ID: 2, Name: "B", Types: "Creature", AttachedTo: 1},
	)
}
