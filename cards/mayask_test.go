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
