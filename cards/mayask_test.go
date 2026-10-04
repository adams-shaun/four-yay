package cards

import "testing"

func mayAskCard(t *testing.T, src string) *Face {
	t.Helper()
	c, diags := ParseBytes("mayask.txt", []byte(src))
	if c == nil {
		t.Fatalf("parse: %v", diags)
	}
	c.Link()
	return c.Faces[0]
}

// TestSAChainMayAsk pins the text half of the ask-free predicate on the
// shapes it must tell apart: an allowlisted chain with no rider is ask-free;
// an off-list API, a deny-listed rider (vocabulary or not), a DB$ Cost$
// window, a ChangeZone sub-target (asked mid-resolution) and a hidden-zone search may ask.
func TestSAChainMayAsk(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want bool
	}{
		{"plain burn", "A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3 | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2", false},
		{"off-list API", "A:SP$ Scry | ScryNum$ 2", true},
		{"vocabulary rider", "A:SP$ Draw | NumCards$ 1 | OptionalDecider$ You", true},
		{"raw rider", "A:SP$ PutCounter | Defined$ Self | CounterType$ P1P1 | Bolster$ True", true},
		{"DB cost window", "A:SP$ Draw | NumCards$ 1 | SubAbility$ DBPay\nSVar:DBPay:DB$ GainLife | LifeAmount$ 1 | Cost$ 2", true},
		{"cast-time sub target", "A:SP$ Draw | NumCards$ 1 | SubAbility$ DBDmg\nSVar:DBDmg:DB$ DealDamage | ValidTgts$ Any | NumDmg$ 1", false},
		{"changezone sub target", "A:SP$ Draw | NumCards$ 1 | SubAbility$ DBBounce\nSVar:DBBounce:DB$ ChangeZone | ValidTgts$ Creature | Origin$ Battlefield | Destination$ Hand", true},
		{"library search", "A:SP$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Land", true},
		{"counter-kind pick", "A:SP$ PutCounter | Defined$ Self | CounterType$ P1P1,LOYALTY", true},
		{"charm of ask-free bodies", "A:SP$ Charm | Choices$ DBA,DBB\nSVar:DBA:DB$ GainLife | LifeAmount$ 2\nSVar:DBB:DB$ Draw | NumCards$ 1", false},
		{"copy of a defined board object", "A:SP$ CopyPermanent | Defined$ Remembered", true},
		{"copy of a target", "A:SP$ CopyPermanent | ValidTgts$ Permanent", true},
		{"copy of self", "A:SP$ CopyPermanent | Defined$ Self", false},
		{"copy of a self that enchants", "K:Enchant creature\nA:SP$ CopyPermanent | Defined$ Self", true},
		{"charm with an asking body", "A:SP$ Charm | Choices$ DBA,DBB\nSVar:DBA:DB$ GainLife | LifeAmount$ 2\nSVar:DBB:DB$ Scry | ScryNum$ 1", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := mayAskCard(t, "Name:Ask Probe\nManaCost:R\nTypes:Sorcery\n"+tc.src+"\nOracle:x\n")
			sa := f.SpellAbility()
			if sa == nil {
				t.Fatal("no spell ability")
			}
			if got := SAChainMayAsk(sa, f.SVars, f, true); got != tc.want {
				t.Fatalf("SAChainMayAsk = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestFaceEntryMayAsk: an as-enters choice keyword asks as the permanent
// enters; a vanilla permanent does not.
func TestFaceEntryMayAsk(t *testing.T) {
	plain := mayAskCard(t, "Name:Plain Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if FaceEntryMayAsk(plain) {
		t.Fatal("a vanilla creature's entry cannot ask")
	}
	painter := mayAskCard(t, "Name:Ask Painter\nManaCost:R\nTypes:Creature Shapeshifter\nPT:1/3\n"+
		"K:ETBReplacement:Other:ChooseColor\n"+
		"SVar:ChooseColor:DB$ ChooseColor | Defined$ You | SpellDescription$ As CARDNAME enters, choose a color.\nOracle:x\n")
	if !FaceEntryMayAsk(painter) {
		t.Fatal("an as-enters color choice asks as the permanent enters")
	}
}

// TestTriggerLineMayAsk: an optional trigger asks at resolution.
func TestTriggerLineMayAsk(t *testing.T) {
	tr, ok := ParseTriggerLine("Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | OptionalDecider$ You | Execute$ TrigDraw")
	if !ok || !TriggerLineMayAsk(tr) {
		t.Fatal("an OptionalDecider$ trigger asks")
	}
	tr, ok = ParseTriggerLine("Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigDraw")
	if !ok || TriggerLineMayAsk(tr) {
		t.Fatal("a mandatory trigger line does not ask by itself")
	}
}

// TestSAChainMayAskAttachAndAbilitySubTargets pins the miss classes closed
// in W3 step 2: an Equipment whose own "as this becomes attached" choice
// (an R:Event$ Attached replacement) asks as it equips, an Attach of a
// non-Self object (whose face is board-dependent), and an ability's
// sub-ability target set (asked mid-resolution; a spell's is asked at cast).
func TestSAChainMayAskAttachAndAbilitySubTargets(t *testing.T) {
	plain := mayAskCard(t, "Name:Plain Blade\nManaCost:1\nTypes:Artifact Equipment\nK:Equip:1\nOracle:x\n")
	paper := mayAskCard(t, "Name:Paper Probe\nManaCost:2\nTypes:Artifact Equipment\n"+
		"R:Event$ Attached | ValidCard$ Card.Self | ValidTarget$ Creature | ReplaceWith$ ChooseColor | ActiveZones$ Battlefield\n"+
		"SVar:ChooseColor:DB$ ChooseColor | Defined$ You\nK:Equip:1\nOracle:x\n")
	equip := func(f *Face) *SA {
		for _, a := range f.Abilities {
			if a.API == "Attach" {
				return a
			}
		}
		t.Fatalf("%s: no equip ability", f.Name)
		return nil
	}
	if SAChainMayAsk(equip(plain), plain.SVars, plain, true) {
		t.Fatal("a plain Equipment's equip is ask-free")
	}
	if !SAChainMayAsk(equip(paper), paper.SVars, paper, true) {
		t.Fatal("an Equipment with an Attached replacement asks as it equips")
	}
	if !SAChainMayAsk(equip(plain), plain.SVars, nil, true) {
		t.Fatal("an equip with no known Self face may ask")
	}
	ab := mayAskCard(t, "Name:Sub Probe\nManaCost:1\nTypes:Artifact\n"+
		"A:AB$ Pump | Cost$ 1 | Defined$ Self | SubAbility$ DBDmg\n"+
		"SVar:DBDmg:DB$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n")
	if !SAChainMayAsk(ab.Abilities[0], ab.SVars, ab, true) {
		t.Fatal("an ability's sub-ability target set is asked mid-resolution")
	}
	if g := SAChainBoardGates(ab.Abilities[0], ab.SVars); !g.Has(ReplDamageDone) || g.Has(ReplMoved) {
		t.Fatalf("board gates %b, want the damage gate", g)
	}
}

// askFreeEntryDecisions is the entering-object census: every allowlisted API
// says whether its resolution can put an object onto the battlefield and, if
// so, how the text half judges what that object asks as it enters (an Aura's
// CR 303.4f enchant choice, an as-enters choice, a Clone's copy election).
// A new allowlisted API fails TestAskFreeEntryCensus until it is decided.
var askFreeEntryDecisions = map[string]string{
	"ChangeZone":    "enters: changeZoneMayAsk exempts only the source returning itself (FaceEntryMayAsk, faceEnchants)",
	"CopyPermanent": "enters: copyEntryMayAsk exempts only a copy of the source itself (FaceEntryMayAsk, faceEnchants)",
	"Token":         "enters: a token script's entry is held by rules' TestTokenEntryAskCensus (an Aura token names its bearer with AttachedTo$)",

	"Animate": "", "Attach": "", "Charm": "", "Cleanup": "", "Counter": "",
	"DamageAll": "", "DealDamage": "", "Debuff": "", "Destroy": "", "DestroyAll": "",
	"Draw": "", "Effect": "", "Fight": "", "GainLife": "", "ImmediateTrigger": "",
	"LoseLife": "", "Mill": "", "MultiplyCounter": "", "Pump": "", "PumpAll": "",
	"PutCounter": "", "PutCounterAll": "", "Regenerate": "", "StoreSVar": "",
	"Tap": "", "TapAll": "", "UntapAll": "",
}

// TestAskFreeEntryCensus holds askFreeEntryDecisions equal to the allowlist:
// "" means the API never puts an object onto the battlefield.
func TestAskFreeEntryCensus(t *testing.T) {
	names := AskFreeAPINames()
	seen := map[string]bool{}
	for _, n := range names {
		seen[n] = true
		if _, ok := askFreeEntryDecisions[n]; !ok {
			t.Errorf("allowlisted API %q has no entering-object decision", n)
		}
	}
	for n := range askFreeEntryDecisions {
		if !seen[n] {
			t.Errorf("entering-object decision for %q, which is not allowlisted", n)
		}
	}
}
