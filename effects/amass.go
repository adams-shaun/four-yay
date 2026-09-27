package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("Amass", effAmass) }

// effAmass implements the Amass primitive (CR 701.55b-c; 62 corpus files, all
// DB$-on-a-trigger and SP$ spell shapes with Type$ + Num$): put Num$
// +1/+1 counters on an Army the resolving ability's controller controls; if
// they control none, first create a 0/0 black <Type> Army token. The Army
// also becomes the named creature type.
//
// Token choice: which existing Army gains the counters is a player choice in
// real Magic; this build asks no mid-resolution "which Army" question, so the
// stand-in is deterministic and replay-stable -- the FIRST Army in
// battlefield order (the same shape the Sacrifice first-match stand-in
// documents). Multi-player amass lines name their controller explicitly
// (Type$ is a creature type, never a player).
//
// Token script: the corpus ships dedicated 0/0 black <Type> Army token
// scripts for the types real Amass lines name (b_0_0_zombie_army,
// b_0_0_orc_army, b_0_0_goblin_army); the key tried first is
// "b_0_0_<lowercased type>_army", falling back to the generic
// "b_0_0_army". A created token is minted through h.EmitTokenCreate, exactly
// as effToken does, so replay derives the same object; because a CreateToken
// replacement can rewrite one would-be token into several mints (Doubling
// Season, Anointed Procession), every rider below -- the +1/+1 counters, the
// type grant and RememberAmass$ -- lands on EVERY mint the plan produced, not
// just the first. Each mint's CounterChange is its own event, so an
// AddCounter replacement doubles that mint's counters independently
// (CR 614.5/616.1e).
//
// The "it's also a <Type>" half is a permanent layer-4 type change on the
// token (or the existing Army) -- registered as a Permanent continuous
// effect whose Affects is "Card.Self" with the object as Source, so it
// outlives the turn and dies with the object (a permanent's own statics
// expire with its battlefield presence the same way). Dedicated token
// scripts already carry the type in their own Types line; the grant is
// registered anyway (idempotent at the layer system, and correct for the
// generic-Army fallback).
func effAmass(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	n := Num(h, c, sa, "Num", 1)
	if n <= 0 {
		return
	}
	typ := strings.TrimSpace(sa.Params["Type"])
	if typ == "" {
		typ = "Army"
	}
	// Find the controller's first Army in battlefield order.
	army := state.ObjID(0)
	for _, id := range g.Zone(state.ZBattlefield, c.Controller) {
		o := g.Obj(id)
		if o == nil || o.Face() == nil {
			continue
		}
		for _, t := range o.Face().Types {
			if t == "Army" {
				army = id
				break
			}
		}
		if army != 0 {
			break
		}
	}
	if army == 0 {
		// No Army: create the 0/0 black <Type> Army token first (CR 701.55c).
		key := "b_0_0_" + strings.ToLower(typ) + "_army"
		if _, ok := g.Tokens[key]; !ok {
			key = "b_0_0_army"
		}
		if _, ok := g.Tokens[key]; !ok {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "Amass: no Army token script available (" + key + ")"})
			return
		}
		mints := h.EmitTokenCreate(events.Event{Kind: events.TokenCreate, Player: c.Controller, Text: key})
		for _, want := range mints {
			o := g.Obj(want)
			if o == nil {
				continue
			}
			if !amassRiders(h, c, sa, want, o.Controller, typ, n) {
				return
			}
		}
		return
	}
	// An existing Army was found: single-object path, exactly as before.
	amassRiders(h, c, sa, army, g.Obj(army).Controller, typ, n)
}

// amassRiders applies amass's per-object riders -- the +1/+1 counters, the
// permanent "also a <Type>" grant and RememberAmass$ -- to one object. It
// returns false when the object folded away, so the no-Army loop can stop.
func amassRiders(h Host, c *Ctx, sa *cards.SA, obj state.ObjID, controller state.PlayerID, typ string, n int32) bool {
	g := h.Game()
	if g.Obj(obj) == nil {
		return false
	}
	h.Emit(events.Event{Kind: events.CounterChange, Obj: obj,
		Counter: "P1P1", Amount: n})
	// "It's also a <Type>" (CR 701.55b): a permanent type grant.
	h.AddContinuous(state.ContinuousEffect{
		Source:     obj,
		Controller: controller,
		Affects:    "Card.Self",
		Layer:      state.LType,
		AddTypes:   []string{typ},
		Permanent:  true,
	})
	if sa.Params["RememberAmass"] != "" {
		c.Remembered = append(c.Remembered, state.Target{Obj: obj})
	}
	return true
}
