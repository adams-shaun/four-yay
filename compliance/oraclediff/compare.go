// Package oraclediff compares gorge's oracle-scenario snapshots with the
// XMage driver's (tools/xmageoracle) and reports the first difference
// (spec 2026-10-02-xmage-compliance-oracle-design section 7).
package oraclediff

import (
	"fmt"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/rules"
)

// XResult is one line of the XMage driver's output.
type XResult struct {
	Name      string                 `json:"name"`
	ID        string                 `json:"id,omitempty"`
	Harness   string                 `json:"harness,omitempty"`
	MS        int                    `json:"ms"`
	Snapshots []rules.OracleSnapshot `json:"snapshots"`
}

// Status is a comparison outcome.
type Status string

const (
	Agree   Status = "AGREE"
	Diverge Status = "DIVERGE"
	Harness Status = "HARNESS"
)

// Verdict is the first difference between the two engines, or Agree.
type Verdict struct {
	Status     Status `json:"status"`
	Checkpoint string `json:"checkpoint,omitempty"`
	Field      string `json:"field,omitempty"`
	Gorge      string `json:"gorge,omitempty"`
	XMage      string `json:"xmage,omitempty"`
	Engine     string `json:"engine,omitempty"` // for Harness
	Msg        string `json:"msg,omitempty"`    // for Harness
}

// Compare walks both engines' checkpoints in order. A harness failure on
// either side is never a verdict on the card.
func Compare(g rules.OracleResult, gerr error, x XResult) Verdict {
	if gerr != nil {
		return Verdict{Status: Harness, Engine: "gorge", Msg: gerr.Error()}
	}
	if x.Harness != "" {
		return Verdict{Status: Harness, Engine: "xmage", Msg: x.Harness}
	}
	// The runner marks a failure to PERFORM a scenario "harness:"; any other
	// "step N (op): ..." failure is gorge refusing or failing an action XMage
	// performed, and a leftover answer is a decision-shape divergence.
	for _, f := range g.Fails {
		if strings.Contains(f, "harness:") {
			return Verdict{Status: Harness, Engine: "gorge", Msg: f}
		}
	}
	for _, f := range g.Fails {
		switch {
		case strings.Contains(f, "unconsumed answer(s)"):
			return Verdict{Status: Diverge, Field: "decisions", Gorge: f, XMage: "consumed"}
		case strings.HasPrefix(f, "step "):
			return Verdict{Status: Diverge, Field: "action", Gorge: f, XMage: "performed"}
		}
	}
	if len(g.Snapshots) != len(x.Snapshots) {
		return Verdict{Status: Harness, Engine: "both", Msg: fmt.Sprintf("%d gorge checkpoints, %d xmage", len(g.Snapshots), len(x.Snapshots))}
	}
	for i := range g.Snapshots {
		if v, ok := compareSnap(g.Snapshots[i], x.Snapshots[i]); !ok {
			return v
		}
	}
	return Verdict{Status: Agree}
}

func compareSnap(g, x rules.OracleSnapshot) (Verdict, bool) {
	cp := g.Checkpoint
	diff := func(field, gv, xv string) (Verdict, bool) {
		return Verdict{Status: Diverge, Checkpoint: cp, Field: field, Gorge: gv, XMage: xv}, false
	}
	if g.Checkpoint != x.Checkpoint {
		return Verdict{Status: Harness, Engine: "both", Msg: fmt.Sprintf("checkpoint %q vs %q", g.Checkpoint, x.Checkpoint)}, false
	}
	if g.Turn != x.Turn {
		return diff("turn", fmt.Sprint(g.Turn), fmt.Sprint(x.Turn))
	}
	if gs, xs := g.Step, XMageStep(x.Step); gs != xs {
		return diff("step", gs, xs)
	}
	if g.Active != x.Active {
		return diff("active", fmt.Sprint(g.Active), fmt.Sprint(x.Active))
	}
	if g.Over != x.Over {
		return diff("over", fmt.Sprint(g.Over), fmt.Sprint(x.Over))
	}
	if len(g.Players) != len(x.Players) {
		return diff("players", fmt.Sprint(len(g.Players)), fmt.Sprint(len(x.Players)))
	}
	for i := range g.Players {
		gp, xp := g.Players[i], x.Players[i]
		pf := func(f string) string { return fmt.Sprintf("p%d.%s", i, f) }
		for _, c := range []struct {
			f      string
			gv, xv string
		}{
			{"life", fmt.Sprint(gp.Life), fmt.Sprint(xp.Life)},
			{"counters", counters(gp.Counters), counters(xp.Counters)},
			{"hand", list(gp.Hand, true), list(xp.Hand, true)},
			{"graveyard", list(gp.Graveyard, false), list(xp.Graveyard, false)},
			{"exile", list(gp.Exile, true), list(xp.Exile, true)},
			{"library_count", fmt.Sprint(gp.LibraryCount), fmt.Sprint(xp.LibraryCount)},
			{"library_top", list(gp.LibraryTop, false), list(xp.LibraryTop, false)},
			{"pool", sortMana(gp.Pool), sortMana(xp.Pool)},
		} {
			if c.gv != c.xv {
				return diff(pf(c.f), c.gv, c.xv)
			}
		}
	}
	gperm, xperm := permKeys(g.Permanents), permKeys(x.Permanents)
	if strings.Join(gperm, "\n") != strings.Join(xperm, "\n") {
		onlyG, onlyX := symDiff(gperm, xperm)
		return diff("permanents", strings.Join(onlyG, "; "), strings.Join(onlyX, "; "))
	}
	if gs, xs := stackKeys(g.Stack), stackKeys(x.Stack); gs != xs {
		return diff("stack", gs, xs)
	}
	return Verdict{}, true
}

// XMageStep maps an XMage PhaseStep name onto gorge's step name.
func XMageStep(s string) string {
	switch s {
	case "UNTAP":
		return "untap"
	case "UPKEEP":
		return "upkeep"
	case "DRAW":
		return "draw"
	case "PRECOMBAT_MAIN":
		return "main1"
	case "BEGIN_COMBAT":
		return "begin-combat"
	case "DECLARE_ATTACKERS":
		return "declare-attackers"
	case "DECLARE_BLOCKERS":
		return "declare-blockers"
	case "FIRST_COMBAT_DAMAGE", "COMBAT_DAMAGE":
		return "combat-damage"
	case "END_COMBAT":
		return "end-combat"
	case "POSTCOMBAT_MAIN":
		return "main2"
	case "END_TURN":
		return "end"
	case "CLEANUP":
		return "cleanup"
	}
	return s
}

// RefName strips a scenario ref ("p1:token:Name#2") to the object name.
func RefName(ref string) string {
	n := ref
	if i := strings.IndexByte(n, ':'); i >= 0 && strings.HasPrefix(n, "p") {
		n = n[i+1:]
	}
	n = strings.TrimPrefix(n, "token:")
	if j := strings.LastIndexByte(n, '#'); j >= 0 && j+1 < len(n) && strings.Trim(n[j+1:], "0123456789") == "" {
		n = n[:j]
	}
	return n
}

func normCounter(k string) string {
	switch strings.ReplaceAll(strings.ToLower(k), " ", "") {
	case "+1/+1", "p1p1":
		return "P1P1"
	case "-1/-1", "m1m1":
		return "M1M1"
	}
	return strings.ToUpper(strings.ReplaceAll(k, " ", ""))
}

func counters(m map[string]int32) string {
	keys := make([]string, 0, len(m))
	agg := map[string]int32{}
	for k, v := range m {
		if v == 0 {
			continue
		}
		nk := normCounter(k)
		if _, ok := agg[nk]; !ok {
			keys = append(keys, nk)
		}
		agg[nk] += v
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, agg[k]))
	}
	return strings.Join(parts, ",")
}

func list(s []string, sorted bool) string {
	c := append([]string(nil), s...)
	if sorted {
		sort.Strings(c)
	}
	return "[" + strings.Join(c, ", ") + "]"
}

// sortMana renders any WUBRGC letter string in WUBRGC order.
func sortMana(s string) string {
	var b strings.Builder
	for _, c := range "WUBRGC" {
		b.WriteString(strings.Repeat(string(c), strings.Count(strings.ToUpper(s), string(c))))
	}
	return b.String()
}

// colors keeps only WUBRG letters, in WUBRG order: XMage's ObjectColor and
// gorge's colour string spell colourless differently.
func colors(s string) string {
	var b strings.Builder
	for _, c := range "WUBRG" {
		if strings.ContainsRune(strings.ToUpper(s), c) {
			b.WriteRune(c)
		}
	}
	return b.String()
}

func types(ts []string) string {
	c := make([]string, 0, len(ts))
	for _, t := range ts {
		c = append(c, strings.ToLower(strings.ReplaceAll(t, " ", "")))
	}
	sort.Strings(c)
	return strings.Join(c, " ")
}

// permKeys renders each permanent as one canonical line and sorts them:
// cards match by controller and name (the multiset), tokens by
// characteristics, since the engines name tokens differently.
func permKeys(ps []rules.OracleSnapPerm) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		name := p.Name
		if p.Token {
			name = "token"
		}
		k := fmt.Sprintf("c%d o%d %s [%s] {%s}", p.Controller, p.Owner, name, types(p.Types), colors(p.Colors))
		if p.PT != "" {
			k += " " + p.PT
		}
		if p.Tapped {
			k += " tapped"
		}
		if p.FaceDown {
			k += " facedown"
		}
		if p.Damage != 0 {
			k += fmt.Sprintf(" dmg=%d", p.Damage)
		}
		if c := counters(p.Counters); c != "" {
			k += " counters=" + c
		}
		if p.AttachedTo != "" {
			k += " on=" + RefName(p.AttachedTo)
		}
		if p.Attacking {
			k += " attacking"
		}
		if p.Blocking {
			k += " blocking"
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func symDiff(a, b []string) (onlyA, onlyB []string) {
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case j >= len(b) || i < len(a) && a[i] < b[j]:
			onlyA = append(onlyA, a[i])
			i++
		case i >= len(a) || b[j] < a[i]:
			onlyB = append(onlyB, b[j])
			j++
		default:
			i++
			j++
		}
	}
	return onlyA, onlyB
}

func stackKeys(st []rules.OracleSnapStack) string {
	parts := make([]string, 0, len(st))
	for _, s := range st {
		parts = append(parts, fmt.Sprintf("%s:%s@p%d", s.Kind, RefName(s.Source), s.Controller))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
