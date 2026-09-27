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
// "b_0_0_army". A created token is minted through the ordinary TokenCreate
// event, exactly as effToken does, so replay derives the same object.
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
	if rest := resumingMint(c, sa); rest != nil {
		// The Army's mint parked behind a CR 616.1 order ask and the answer
		// has minted it: amass onto it with the values the first pass
		// resolved (an Army the answer did not create leaves nothing to
		// amass onto, the same stop the unparked miss takes).
		if len(rest.Parked) > 0 && g.Obj(rest.Parked[0]) != nil {
			amassOnto(h, c, sa, rest.Parked[0], rest.Amount, rest.Script)
		}
		return
	}
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
		want := g.NextID
		wasSuspended := h.Suspended()
		minted := h.EmitTokenCreate(events.Event{Kind: events.TokenCreate, Player: c.Controller, Text: key})
		if !wasSuspended && h.Suspended() && len(minted) == 0 &&
			suspendMint(h, c, TokenRest{SA: sa, Amount: n, Script: typ}) {
			// The Army parked behind an order ask: the counters, the type
			// grant and the remember land on it when the answer mints it.
			return
		}
		if o := g.Obj(want); o != nil {
			army = want
		}
	}
	if army == 0 {
		return
	}
	amassOnto(h, c, sa, army, n, typ)
}

// amassOnto puts n +1/+1 counters on army and makes it a typ (CR 701.55b),
// remembering it for RememberAmass$.
func amassOnto(h Host, c *Ctx, sa *cards.SA, army state.ObjID, n int32, typ string) {
	g := h.Game()
	h.Emit(events.Event{Kind: events.CounterChange, Obj: army,
		Counter: "P1P1", Amount: n})
	// "It's also a <Type>" (CR 701.55b): a permanent type grant.
	h.AddContinuous(state.ContinuousEffect{
		Source:     army,
		Controller: g.Obj(army).Controller,
		Affects:    "Card.Self",
		Layer:      state.LType,
		AddTypes:   []string{typ},
		Permanent:  true,
	})
	if sa.Params["RememberAmass"] != "" {
		c.Remembered = append(c.Remembered, state.Target{Obj: army})
	}
}
