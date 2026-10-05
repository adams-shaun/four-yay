package cards

import "testing"

// TestClassExpandsEntryCounterActivatorsAndGrants pins the kw:Class expansion
// on Fortune Teller's Talent's real script shape: level 1 is intrinsic (no
// entry counter or replacement), one sorcery-speed level-up activator per
// level gated on the Class being at EXACTLY that level minus one (CR
// 716.2b/716.2d: "activate only if this Class's level is less than N" is
// Constructed's exact N-1 rule), each carrying its target Level$ N, and the
// level's granted static appended with a ClassBand$<N> gate so it is live
// from level N on.
func TestClassExpandsEntryCounterActivatorsAndGrants(t *testing.T) {
	f := expanded(t, "Name:Fortune Teller's Talent\nManaCost:U\nTypes:Enchantment Class\n"+
		"K:Class:2:3 U:AddStaticAbility$ SFutureSight\n"+
		"K:Class:3:2 U:AddStaticAbility$ SReduceCost\n"+
		"SVar:SFutureSight:Mode$ Continuous | Affected$ Card.TopLibrary+YouCtrl | CheckSVar$ X | AffectedZone$ Library | MayPlay$ True\n"+
		"SVar:SReduceCost:Mode$ ReduceCost | ValidCard$ Card.!wasCastFromYourHand | Type$ Spell | Activator$ You | Amount$ 2\n"+
		"SVar:X:Count$ThisTurnCast_Card.YouCtrl\nOracle:x\n")

	// Level 1 is intrinsic: there is no counter-placing entry replacement.
	if len(f.Repls) != 0 {
		t.Fatalf("Class entry installed counter replacement: %+v", f.Repls)
	}

	// 2. One activator per level, at the named cost, the right level gate.
	if len(f.Abilities) != 2 {
		t.Fatalf("want 2 level-up activators, got %d: %+v", len(f.Abilities), f.Abilities)
	}
	byCost := map[string]*SA{}
	for _, a := range f.Abilities {
		if a.API != "ClassLevelUp" {
			t.Fatalf("level-up activator is not ClassLevelUp: %+v", a)
		}
		byCost[a.Params["Cost"]] = a
	}
	l2, l3 := byCost["3 U"], byCost["2 U"]
	if l2 == nil || l3 == nil {
		t.Fatalf("activator costs wrong: %+v", byCost)
	}
	if l2.Params["IsPresent"] != "Card.Self+classLevel_EQ1" {
		t.Fatalf("level-2 activator gate: %q", l2.Params["IsPresent"])
	}
	if l3.Params["IsPresent"] != "Card.Self+classLevel_EQ2" {
		t.Fatalf("level-3 activator gate: %q", l3.Params["IsPresent"])
	}
	if l2.Params["Level"] != "2" {
		t.Fatalf("level-2 activator target level: %q", l2.Params["Level"])
	}
	if l3.Params["Level"] != "3" {
		t.Fatalf("level-3 activator target level: %q", l3.Params["Level"])
	}
	for _, a := range f.Abilities {
		if a.Params["SorcerySpeed"] != "True" {
			t.Fatalf("level-up must be sorcery speed: %+v", a.Params)
		}
	}

	// 3. The granted statics, gated at their own level.
	var sf, sr *Static
	for i := range f.Statics {
		switch f.Statics[i].Params["ClassBand"] {
		case "2":
			sf = &f.Statics[i]
		case "3":
			sr = &f.Statics[i]
		}
		// The band is a dedicated ClassBand$ param, NEVER an IsPresent$
		// overwrite: a body's own present clause must survive intact.
		if _, ok := f.Statics[i].Params["IsPresent2"]; ok {
			t.Fatalf("Class grant wrote IsPresent2$: %+v", f.Statics[i].Params)
		}
	}
	if sf == nil || sf.Mode != "Continuous" || sf.Params["MayPlay"] != "True" || sf.Params["CheckSVar"] != "X" {
		t.Fatalf("level-2 grant wrong: %+v", sf)
	}
	if sr == nil || sr.Mode != "ReduceCost" || sr.Params["Amount"] != "2" {
		t.Fatalf("level-3 grant wrong: %+v", sr)
	}
}

// TestClassExpandsTriggerAndAmpersandGrants pins the other two grant kinds:
// AddTrigger$ appends a trigger with the level band (Warlock Class's
// TriggerClassLevel shape) and an "A & B" AddStaticAbility$ value yields one
// static per named body (with_two_of_everything? -> SMayLook & SMayPlay). The
// trigger body here also carries its OWN ClassLevel$ crossing gate (the real
// Mode$ ClassLevelGained param), which the band must NOT overwrite -- the band
// is ClassBand$, a distinct name.
func TestClassExpandsTriggerAndAmpersandGrants(t *testing.T) {
	f := expanded(t, "Name:Warlock Class\nManaCost:B\nTypes:Enchantment Class\n"+
		"K:Class:2:1 B:AddTrigger$ TriggerClassLevel\n"+
		"K:Class:3:2 U:AddStaticAbility$ SMayLook & SMayPlay\n"+
		"SVar:TriggerClassLevel:Mode$ ClassLevelGained | ClassLevel$ 2 | ValidCard$ Card.Self | Execute$ TrigDig | TriggerZones$ Battlefield\n"+
		"SVar:TrigDig:DB$ Dig | DigNum$ 3\n"+
		"SVar:SMayLook:Mode$ Continuous | Affected$ Card.TopLibrary+YouCtrl | AffectedZone$ Library | MayLookAt$ You\n"+
		"SVar:SMayPlay:Mode$ Continuous | Affected$ Card.TopLibrary+YouCtrl | AffectedZone$ Library | MayPlay$ True\n"+
		"Oracle:x\n")

	if len(f.Triggers) != 1 || f.Triggers[0].Mode != "ClassLevelGained" {
		t.Fatalf("want one ClassLevelGained trigger, got %+v", f.Triggers)
	}
	if f.Triggers[0].Params["ClassBand"] != "2" {
		t.Fatalf("trigger level gate: %q", f.Triggers[0].Params["ClassBand"])
	}
	if f.Triggers[0].Params["ClassLevel"] != "2" {
		t.Fatalf("trigger's own ClassLevel$ crossing gate overwritten: %q", f.Triggers[0].Params["ClassLevel"])
	}
	if _, ok := f.Triggers[0].Params["IsPresent2"]; ok {
		t.Fatalf("Class trigger grant wrote IsPresent2$: %+v", f.Triggers[0].Params)
	}
	if f.Triggers[0].Effect == nil || f.Triggers[0].Effect.API != "Dig" {
		t.Fatalf("trigger Execute$ did not link: %+v", f.Triggers[0].Effect)
	}
	n := 0
	for _, s := range f.Statics {
		if s.Params["ClassBand"] == "3" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("want 2 statics for an 'A & B' grant, got %d: %+v", n, f.Statics)
	}
}

// TestClassGrantKeepsTheBodysOwnIsPresent pins the defect class the Class
// level band must not reintroduce: a granted body that carries its OWN
// IsPresent$ keeps it intact and gains the band as a SEPARATE ClassBand$
// parameter. The old expansion pushed the band into IsPresent2$, which the
// trigger gate reads as a UNION with IsPresent$ (Hunter's Talent's end-step
// draw fired at level 1), and which the replacement gate never read at all.
func TestClassGrantKeepsTheBodysOwnIsPresent(t *testing.T) {
	f := expanded(t, "Name:Hunter's Talent\nManaCost:G\nTypes:Enchantment Class\n"+
		"K:Class:3:3 G:AddTrigger$ TriggerEndStep\n"+
		"K:Class:2:1 G:AddReplacementEffect$ SRepl\n"+
		"SVar:TriggerEndStep:Mode$ Phase | Phase$ End of Turn | ValidPlayer$ You | TriggerZones$ Battlefield | IsPresent$ Creature.powerGE4+YouCtrl | Execute$ TrigDraw\n"+
		"SVar:TrigDraw:DB$ Draw\n"+
		"SVar:SRepl:Event$ Moved | Destination$ Battlefield | ValidCard$ Card.Self | ReplaceWith$ DBPutCounter | IsPresent$ Creature.YouCtrl\n"+
		"SVar:DBPutCounter:DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ 1\n"+
		"Oracle:x\n")
	if len(f.Triggers) != 1 {
		t.Fatalf("want one granted trigger, got %+v", f.Triggers)
	}
	tr := f.Triggers[0]
	if tr.Params["IsPresent"] != "Creature.powerGE4+YouCtrl" {
		t.Fatalf("granted trigger lost its own IsPresent$: %q", tr.Params["IsPresent"])
	}
	if tr.Params["IsPresent2"] != "" {
		t.Fatalf("granted trigger wrote the band into IsPresent2$: %q", tr.Params["IsPresent2"])
	}
	if tr.Params["ClassBand"] != "3" {
		t.Fatalf("granted trigger band = %q, want 3", tr.Params["ClassBand"])
	}
	var granted *Repl
	for i := range f.Repls {
		if f.Repls[i].Params["KeywordLine"] != "" {
			granted = &f.Repls[i]
		}
	}
	if granted == nil {
		t.Fatalf("want one Class-granted replacement, got %+v", f.Repls)
	}
	if granted.Params["IsPresent"] != "Creature.YouCtrl" {
		t.Fatalf("granted replacement lost its own IsPresent$: %q", granted.Params["IsPresent"])
	}
	if granted.Params["ClassBand"] != "2" {
		t.Fatalf("granted replacement band = %q, want 2", granted.Params["ClassBand"])
	}
}

// TestClassExpansionIsIdempotent pins the KeywordLine idempotence across a
// second Link() of the same face -- the cached-face re-link path. Without the
// static arm of the `has` check a second Link double-appends every level's
// granted static and activator.
func TestClassExpansionIsIdempotent(t *testing.T) {
	c, diags := ParseBytes("k.txt", []byte("Name:Warlock Class\nManaCost:B\nTypes:Enchantment Class\n"+
		"K:Class:2:1 B:AddStaticAbility$ SBoost\n"+
		"SVar:SBoost:Mode$ Continuous | Affected$ Creature.YouCtrl | AddPower$ 1\n"+
		"Oracle:x\n"))
	if len(diags) > 0 {
		t.Fatal(diags)
	}
	if d := c.Link(); len(d) > 0 {
		t.Fatal(d)
	}
	f := c.Faces[0]
	repls, abilities, statics := len(f.Repls), len(f.Abilities), len(f.Statics)
	if repls != 0 || abilities != 1 || statics != 1 {
		t.Fatalf("first Link: repls=%d abilities=%d statics=%d", repls, abilities, statics)
	}
	if d := c.Link(); len(d) > 0 {
		t.Fatal(d)
	}
	if len(f.Repls) != repls || len(f.Abilities) != abilities || len(f.Statics) != statics {
		t.Fatalf("second Link re-expanded: repls=%d abilities=%d statics=%d", len(f.Repls), len(f.Abilities), len(f.Statics))
	}
}
