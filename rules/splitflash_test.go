package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// splitflash_test.go pins the split/fuse half of target-conditional
// CastWithFlash (task agent-20260926T040050Z-551a6e08). No corpus card
// combines a split/fuse face with a target-conditional cast-with-flash
// grant, so every card here is an inline synthetic fixture (never a Forge
// script file, per the licensing rule): two non-Room faces with disjoint
// target specs (front targets a Creature, alternate targets an Artifact) and
// the `S:Mode$ CastWithFlash | ValidSA$ Spell.IsTargeting Valid Permanent.
// YouCtrl` grant printed on ONE face. That grant is the Flash Photography
// shape the single-face tests in istargeting_statics_test.go pin.

const splitFlashGrant = "S:Mode$ CastWithFlash | ValidCard$ Card.Self | ValidSA$ Spell.IsTargeting Valid Permanent.YouCtrl | EffectZone$ All | Caster$ You | Description$ You may cast this as though it had flash if it targets a permanent you control.\n"

const splitFrontCreature = "A:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 2 | SpellDescription$ x\n"
const splitAltArtifact = "A:SP$ DealDamage | ValidTgts$ Artifact | NumDmg$ 1 | SpellDescription$ x\n"

// altGrantSplitSrc: the grant lives on the ALTERNATE face. Front "Spark"
// {R} Sorcery, alternate "Ricochet" {U} Sorcery.
const altGrantSplitSrc = "Name:Spark\nManaCost:R\nTypes:Sorcery\n" +
	splitFrontCreature +
	"AlternateMode:Split\n" +
	"ALTERNATE\n" +
	"Name:Ricochet\nManaCost:U\nTypes:Sorcery\n" +
	splitFlashGrant + splitAltArtifact +
	"Oracle:x\n"

// frontGrantSplitSrc: the same card with the grant on the FRONT face.
const frontGrantSplitSrc = "Name:Spark\nManaCost:R\nTypes:Sorcery\n" +
	splitFlashGrant + splitFrontCreature +
	"AlternateMode:Split\n" +
	"ALTERNATE\n" +
	"Name:Ricochet\nManaCost:U\nTypes:Sorcery\n" +
	splitAltArtifact +
	"Oracle:x\n"

// instantAltSplitSrc: the alternate half is an INSTANT that also carries the
// grant -- its off-sorcery timing rests on IsInstant, not on the grant, so a
// non-qualifying target must not reverse it.
const instantAltSplitSrc = "Name:Spark\nManaCost:R\nTypes:Sorcery\n" +
	splitFrontCreature +
	"AlternateMode:Split\n" +
	"ALTERNATE\n" +
	"Name:Hasty Reply\nManaCost:U\nTypes:Instant\n" +
	splitFlashGrant + splitAltArtifact +
	"Oracle:x\n"

// altGrantFuseSrc: a fuse card whose FRONT half is an Instant and whose
// ALTERNATE half is a Sorcery carrying the grant.
const altGrantFuseSrc = "Name:Bind\nManaCost:W\nTypes:Instant\nK:Fuse\n" +
	splitFrontCreature +
	"AlternateMode:Split\n" +
	"ALTERNATE\n" +
	"Name:Loose\nManaCost:2 U\nTypes:Sorcery\n" +
	splitFlashGrant + splitAltArtifact +
	"Oracle:x\n"

// frontGrantFuseSrc: the same fuse shape with the grant on the FRONT face.
const frontGrantFuseSrc = "Name:Bind\nManaCost:W\nTypes:Instant\nK:Fuse\n" +
	splitFlashGrant + splitFrontCreature +
	"AlternateMode:Split\n" +
	"ALTERNATE\n" +
	"Name:Loose\nManaCost:2 U\nTypes:Sorcery\n" +
	splitAltArtifact +
	"Oracle:x\n"

const splitBearSrc = "Name:My Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
const splitMyGizmoSrc = "Name:My Gizmo\nTypes:Artifact\nOracle:x\n"
const splitTheirGizmoSrc = "Name:Their Gizmo\nTypes:Artifact\nOracle:x\n"

// splitFlashOption returns the cast option for id with the given Mode, read
// off the hand walk (legalActions) rather than a posed decision -- the
// handEngine fixtures never enter a priority round, exactly like the
// istargeting_statics_test.go reads.
func splitFlashOption(e *Engine, id state.ObjID, mode string) *decision.Option {
	for _, o := range e.legalActions(0) {
		if o.Kind == "cast" && o.Obj == id && o.Mode == mode {
			cp := o
			return &cp
		}
	}
	return nil
}

// splitFlashPreconditions asserts the shape every test below leans on: the
// spell is in hand, a two-face Split card, the two faces have disjoint
// target specs, the named face carries exactly one CastWithFlash static and
// the other carries none, the expected instanthood holds, and the card
// carries no Flash keyword (so its off-turn timing can only rest on the
// grant or on IsInstant).
func splitFlashPreconditions(t *testing.T, e *Engine, spell state.ObjID, grantedFace int, frontInstant, altInstant bool) {
	t.Helper()
	o := e.G.Obj(spell)
	if o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: spell %d is in %s, want an object in hand", spell, o.Zone)
	}
	if o.Card.AlternateMode != "Split" || len(o.Card.Faces) != 2 {
		t.Fatalf("precondition: not a two-face split card: %+v", o.Card)
	}
	front, alt := o.Card.Faces[0], o.Card.Faces[1]
	if front.SpellAbility().Params["ValidTgts"] != "Creature" || alt.SpellAbility().Params["ValidTgts"] != "Artifact" {
		t.Fatalf("precondition: face target specs are not the disjoint Creature/Artifact pair: %q / %q",
			front.SpellAbility().Params["ValidTgts"], alt.SpellAbility().Params["ValidTgts"])
	}
	if front.IsInstant() != frontInstant || alt.IsInstant() != altInstant {
		t.Fatalf("precondition: face instanthood front=%v alt=%v, want %v/%v",
			front.IsInstant(), alt.IsInstant(), frontInstant, altInstant)
	}
	if e.HasKeyword(spell, "Flash") {
		t.Fatal("precondition: the card carries Flash; its timing would not rest on the grant")
	}
	countStatics := func(f int) int {
		n := 0
		for _, st := range o.Card.Faces[f].Statics {
			if st.Mode == "CastWithFlash" {
				n++
			}
		}
		return n
	}
	want := map[int]int{0: 0, 1: 0}
	want[grantedFace] = 1
	if countStatics(0) != want[0] || countStatics(1) != want[1] {
		t.Fatalf("precondition: CastWithFlash statics on front/alt = %d/%d, want %d/%d",
			countStatics(0), countStatics(1), want[0], want[1])
	}
}

// splitFlashBoard places My Bear (a creature you control) and Their Gizmo
// (an artifact your OPPONENT controls) and asserts the TYPE disjointness the
// target specs lean on. My Gizmo -- the qualifying artifact you control --
// is added separately by the tests that need the two directions measured
// apart.
func splitFlashBoard(t *testing.T, e *Engine) (bear, theirGizmo state.ObjID) {
	t.Helper()
	bear = battlePerm(t, e, 0, splitBearSrc)
	theirGizmo = battlePerm(t, e, 1, splitTheirGizmoSrc)
	if !e.G.Obj(bear).Face().IsCreature() {
		t.Fatal("precondition: My Bear is not a creature")
	}
	if e.G.Obj(theirGizmo).Face().IsCreature() {
		t.Fatal("precondition: Their Gizmo must not be a creature (disjoint candidate sets)")
	}
	if e.G.Obj(theirGizmo).Zone != state.ZBattlefield {
		t.Fatal("precondition: Their Gizmo is not on the battlefield")
	}
	return bear, theirGizmo
}

// TestSplitAltTargetConditionalFlash pins CR 709.4 for a target-conditional
// CastWithFlash grant on a split card: the offer judges the half actually
// being cast (its own face-local statics and its own potential targets), and
// CR 601.2e re-checks the ANNOUNCED target of a split_alt cast against the
// same half. A grant printed on the front face must not offer the alternate
// half off-turn, and a qualifying alternate-half target must not be withheld
// because the front face is not a target.
func TestSplitAltTargetConditionalFlash(t *testing.T) {
	// --- the grant lives on the ALTERNATE face: a qualifying target must
	// unlock the off-turn split_alt offer, and the announced target is policed.
	e := handEngine(t, card(t, altGrantSplitSrc))
	e.G.Active = 1 // off-turn: seat 0 needs the flash permission
	e.G.Players[0].Pool[state.MC] = 2
	e.G.Players[0].Pool[state.MU] = 1
	spell := e.G.Zone(state.ZHand, 0)[0]
	splitFlashPreconditions(t, e, spell, 1, false, false)
	bear, theirGizmo := splitFlashBoard(t, e)
	_ = bear

	// Their Gizmo is the alt half's only legal artifact target and the
	// OPPONENT's: the grant has no qualifying permanent you control, so the
	// off-turn offer stays withheld.
	if opt := splitFlashOption(e, spell, "split_alt"); opt != nil {
		t.Fatalf("split_alt offered off-turn with no qualifying target: %+v", *opt)
	}
	// A permanent you control that is ALSO an artifact (so a legal target of
	// the alt half itself) makes the grant satisfiable: the offer appears.
	qualifying := battlePerm(t, e, 0, splitMyGizmoSrc)
	if e.G.Obj(qualifying).Zone != state.ZBattlefield || e.G.Obj(qualifying).Face().IsCreature() {
		t.Fatal("precondition: the qualifying artifact is not a battlefield artifact")
	}
	if opt := splitFlashOption(e, spell, "split_alt"); opt == nil {
		t.Fatalf("split_alt withheld off-turn although a qualifying target exists: %+v", e.legalActions(0))
	}
	// Sorcery timing still offers the half without any grant (no over-withhold).
	e.G.Active = 0
	if opt := splitFlashOption(e, spell, "split_alt"); opt == nil {
		t.Fatal("split_alt not offered at sorcery timing")
	}
	e.G.Active = 1

	// Announcing the NON-qualifying legal target reverses the cast (CR 601.2e).
	opt := splitFlashOption(e, spell, "split_alt")
	e.beginCast(0, *opt)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("split_alt target decision = %+v", d)
	}
	if istTargetOptionFor(d, theirGizmo) < 0 {
		t.Fatalf("precondition: the non-qualifying artifact must be a legal target: %+v", d.Options)
	}
	submitChoices(t, e, istTargetOptionFor(d, theirGizmo))
	if z := e.G.Obj(spell).Zone; z != state.ZHand {
		t.Fatalf("a non-qualifying target left the spell in %s, want the reversal back to hand", z)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack after the reversal = %v, want empty", e.G.Stack)
	}

	// Announcing the qualifying target completes.
	e.beginCast(0, *splitFlashOption(e, spell, "split_alt"))
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("second target decision = %+v", d)
	}
	idx := istTargetOptionFor(d, qualifying)
	if idx < 0 {
		t.Fatalf("precondition: the qualifying artifact must be offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	if z := e.G.Obj(spell).Zone; z == state.ZHand {
		t.Fatal("a qualifying target did not let the split_alt cast proceed past CR 601.2e")
	}
	if fi := e.G.Obj(spell).FaceIdx; fi != 1 {
		t.Fatalf("the cast half is face %d, want the alternate face 1", fi)
	}

	// --- the grant lives on the FRONT face: it must not offer the alternate
	// half off-turn, and it must not withhold anything at sorcery timing.
	e2 := handEngine(t, card(t, frontGrantSplitSrc))
	e2.G.Active = 1
	e2.G.Players[0].Pool[state.MC] = 2
	e2.G.Players[0].Pool[state.MU] = 1
	spell2 := e2.G.Zone(state.ZHand, 0)[0]
	splitFlashPreconditions(t, e2, spell2, 0, false, false)
	bear2, theirGizmo2 := splitFlashBoard(t, e2)
	myGizmo2 := battlePerm(t, e2, 0, splitMyGizmoSrc)
	_ = bear2
	_ = myGizmo2
	_ = theirGizmo2
	// My Bear makes the FRONT half's grant satisfiable; the alternate half has
	// no permission of its own, so its off-turn offer must stay withheld.
	if opt := splitFlashOption(e2, spell2, "split_alt"); opt != nil {
		t.Fatalf("the front half's flash grant offered the alternate half off-turn: %+v", *opt)
	}
	e2.G.Active = 0
	if opt := splitFlashOption(e2, spell2, "split_alt"); opt == nil {
		t.Fatal("split_alt not offered at sorcery timing (front-grant card)")
	}

	// --- the alternate half is an INSTANT: its timing rests on IsInstant, so
	// a non-qualifying target must NOT reverse it (unconditional permission).
	e3 := handEngine(t, card(t, instantAltSplitSrc))
	e3.G.Active = 1
	e3.G.Players[0].Pool[state.MC] = 2
	e3.G.Players[0].Pool[state.MU] = 1
	spell3 := e3.G.Zone(state.ZHand, 0)[0]
	splitFlashPreconditions(t, e3, spell3, 1, false, true)
	_, theirGizmo3 := splitFlashBoard(t, e3)
	opt3 := splitFlashOption(e3, spell3, "split_alt")
	if opt3 == nil {
		t.Fatal("the instant alternate half was not offered off-turn")
	}
	e3.beginCast(0, *opt3)
	d3 := e3.Pending()
	if d3 == nil || d3.Kind != decision.KTarget {
		t.Fatalf("instant-half target decision = %+v", d3)
	}
	submitChoices(t, e3, istTargetOptionFor(d3, theirGizmo3))
	if z := e3.G.Obj(spell3).Zone; z != state.ZStack {
		t.Fatalf("an instant half cast on a non-qualifying target ended in %s, want the stack (no CR 601.2e reversal)", z)
	}
}

// TestFuseTargetConditionalFlash pins the fused-cast half of CR 702.101b +
// 601.2e: each half's target-conditional grant is judged against THAT half's
// own potential targets (the front half's grant is not permission for the
// alternate half), and each non-instant half's OWN stage targets must
// satisfy its grant -- the flat target list of a fused spell is never read
// as either half's.
func TestFuseTargetConditionalFlash(t *testing.T) {
	// --- the grant lives on the ALTERNATE face; the front half is an Instant.
	e := handEngine(t, card(t, altGrantFuseSrc))
	e.G.Active = 1 // off-turn: the fused spell needs BOTH halves instant-speed
	e.G.Players[0].Pool[state.MW] = 1
	e.G.Players[0].Pool[state.MU] = 3
	e.G.Players[0].Pool[state.MC] = 3
	spell := e.G.Zone(state.ZHand, 0)[0]
	splitFlashPreconditions(t, e, spell, 1, true, false)
	if !e.G.Obj(spell).Card.Faces[0].HasKeyword("Fuse") {
		t.Fatal("precondition: the front face does not carry K:Fuse")
	}
	bear, theirGizmo := splitFlashBoard(t, e)

	// With only the opponent's artifact present, the alternate half's grant
	// has no qualifying target: the fused cast stays withheld.
	if opt := splitFlashOption(e, spell, "fuse"); opt != nil {
		t.Fatalf("fuse offered off-turn with no qualifying target: %+v", *opt)
	}
	// My Gizmo (you control, an artifact, thus a legal target of the alternate
	// half itself) satisfies the grant; the front half is an instant on its
	// own. The fused cast is offered.
	myGizmo := battlePerm(t, e, 0, splitMyGizmoSrc)
	if e.G.Obj(myGizmo).Zone != state.ZBattlefield || e.G.Obj(myGizmo).Face().IsCreature() {
		t.Fatal("precondition: the qualifying artifact is not a battlefield artifact")
	}
	if opt := splitFlashOption(e, spell, "fuse"); opt == nil {
		t.Fatalf("fused cast withheld off-turn although each half is instant-speed with a qualifying target: %+v", e.legalActions(0))
	}
	// Sorcery timing still offers the fused cast (no over-withhold).
	e.G.Active = 0
	if opt := splitFlashOption(e, spell, "fuse"); opt == nil {
		t.Fatal("fused cast not offered at sorcery timing")
	}
	e.G.Active = 1

	// Cast fused: stage 0 asks the FRONT half's targets (creatures only),
	// stage 1 the ALTERNATE half's (artifacts only).
	fuse := splitFlashOption(e, spell, "fuse")
	e.beginCast(0, *fuse)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("fused stage 0 decision = %+v", d)
	}
	s0 := istTargetOptionFor(d, bear)
	if s0 < 0 {
		t.Fatalf("precondition: the front stage must offer the creature: %+v", d.Options)
	}
	if istTargetOptionFor(d, myGizmo) >= 0 || istTargetOptionFor(d, theirGizmo) >= 0 {
		t.Fatalf("precondition: the front stage must not offer the artifacts: %+v", d.Options)
	}
	submitChoices(t, e, s0)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("fused stage 1 decision = %+v", d)
	}
	if istTargetOptionFor(d, bear) >= 0 {
		t.Fatalf("precondition: the alternate stage must not offer the creature: %+v", d.Options)
	}
	t1 := istTargetOptionFor(d, theirGizmo)
	if t1 < 0 {
		t.Fatalf("precondition: the alternate stage must offer the opponent's artifact: %+v", d.Options)
	}
	// The non-qualifying artifact on the ALTERNATE half's own stage reverses
	// the whole cast (CR 601.2e): the front half's qualifying creature target
	// must not launder the alternate half's announcement.
	submitChoices(t, e, t1)
	if z := e.G.Obj(spell).Zone; z != state.ZHand {
		t.Fatalf("a non-qualifying alternate-half target left the spell in %s, want the reversal back to hand", z)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack after the fused reversal = %v, want empty", e.G.Stack)
	}
	if fi := e.G.Obj(spell).FaceIdx; fi != 0 {
		t.Fatalf("fused spell left the card at face %d, want the front face 0", fi)
	}

	// The qualifying artifact completes the fused cast.
	e.beginCast(0, *splitFlashOption(e, spell, "fuse"))
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("second fused stage 0 = %+v", d)
	}
	submitChoices(t, e, istTargetOptionFor(d, bear))
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("second fused stage 1 = %+v", d)
	}
	idx := istTargetOptionFor(d, myGizmo)
	if idx < 0 {
		t.Fatalf("precondition: the qualifying artifact must be offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	if z := e.G.Obj(spell).Zone; z == state.ZHand {
		t.Fatal("a qualifying alternate-half target did not let the fused cast proceed past CR 601.2e")
	}

	// --- the grant lives on the FRONT face: it is not permission for the
	// alternate half, so the fused cast stays withheld off-turn.
	e2 := handEngine(t, card(t, frontGrantFuseSrc))
	e2.G.Active = 1
	e2.G.Players[0].Pool[state.MW] = 1
	e2.G.Players[0].Pool[state.MU] = 3
	e2.G.Players[0].Pool[state.MC] = 3
	spell2 := e2.G.Zone(state.ZHand, 0)[0]
	splitFlashPreconditions(t, e2, spell2, 0, true, false)
	bear2, theirGizmo2 := splitFlashBoard(t, e2)
	myGizmo2 := battlePerm(t, e2, 0, splitMyGizmoSrc)
	_ = bear2
	_ = myGizmo2
	_ = theirGizmo2
	// My Bear satisfies the FRONT half's grant; the alternate half has no
	// permission of its own, so the fused spell is not instant-speed.
	if opt := splitFlashOption(e2, spell2, "fuse"); opt != nil {
		t.Fatalf("the front half's flash grant permitted the fused cast off-turn: %+v", *opt)
	}
	e2.G.Active = 0
	if opt := splitFlashOption(e2, spell2, "fuse"); opt == nil {
		t.Fatal("fused cast not offered at sorcery timing (front-grant card)")
	}
}
