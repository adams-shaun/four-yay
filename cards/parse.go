package cards

import (
	"bufio"
	"bytes"
	"os"
	"strings"
)

// Parse reads one cardsfolder script from disk.
func Parse(path string) (*Card, []Diag) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, []Diag{{path, err.Error()}}
	}
	return ParseBytes(path, src)
}

// ParseBytes parses a script already in memory. Kept separate from Parse so
// tests never need fixture files on disk.
func ParseBytes(path string, src []byte) (*Card, []Diag) {
	c := &Card{Path: path}
	cur := newFace()
	c.Faces = append(c.Faces, cur)
	var diags []Diag

	sc := bufio.NewScanner(bytes.NewReader(src))
	// Initial buffer is the stdlib default (4096): the pinned corpus's longest
	// line is ~911 bytes, so a 64 KiB initial buffer was sixteen times too
	// large and, allocated once per script file, was ParseBytes' single
	// largest allocation (4.25 GB across the whole corpus). A tighter initial
	// buffer never grows here.
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), " \t\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "ALTERNATE" || strings.HasPrefix(line, "SPECIALIZE:") {
			cur = newFace()
			if color, ok := strings.CutPrefix(line, "SPECIALIZE:"); ok {
				cur.SpecializeColor = color
			}
			c.Faces = append(c.Faces, cur)
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			diags = append(diags, Diag{path, "unkeyed line: " + line})
			continue
		}
		switch key {
		case "AlternateMode":
			// This is a card-level layout marker even though Forge writes it
			// among the front face's fields. Retain it for rules that distinguish
			// split-card characteristics from transforming DFC characteristics.
			c.AlternateMode = val
		case "Name":
			cur.Name = val
		case "Variant":
			// Only the Universes-Within flavour-name alias carries a
			// decklist-visible name. Every other Variant: value (Attraction
			// lights, DFC face-variant metadata) stays ignored exactly like the
			// other unrecognised keys below: it carries no rules meaning and
			// turning it into a diag would only pollute the corpus diag count.
			if alias, ok := strings.CutPrefix(val, "UniversesWithin:FlavorName:"); ok {
				if alias = strings.TrimSpace(alias); alias != "" {
					cur.Aliases = append(cur.Aliases, alias)
				}
			}
		case "ManaCost":
			cur.ManaCost = val
		case "Types":
			cur.Types = strings.Fields(val)
		case "PT":
			cur.PT = val
		case "Loyalty":
			cur.Loyalty = val
		case "Defense":
			cur.Defense = val
		case "Colors":
			cur.Colors = val
		case "Oracle":
			cur.Oracle = val
		case "K":
			cur.Keywords = append(cur.Keywords, val)
			if strings.HasPrefix(val, "Specialize:") &&
				(strings.Contains(val, "AdditionalActivationZone$") || strings.Contains(val, "ReduceCost$")) {
				rider := "AdditionalActivationZone$"
				if strings.Contains(val, "ReduceCost$") {
					rider = "ReduceCost$"
				}
				diags = append(diags, Diag{path, "unsupported K:Specialize rider " + rider})
			}
		case "A":
			sa, d := parseSA(path, val)
			diags = append(diags, d...)
			if sa != nil {
				cur.Abilities = append(cur.Abilities, sa)
			}
		case "T":
			p := parseParams(val)
			cur.Triggers = append(cur.Triggers, Trigger{Mode: p["Mode"], Params: p})
		case "S":
			p := parseParams(val)
			for _, mode := range splitStaticModes(p["Mode"]) {
				cur.Statics = append(cur.Statics, Static{Mode: mode, Params: p})
			}
		case "R":
			p := parseParams(val)
			cur.Repls = append(cur.Repls, Repl{Event: p["Event"], Params: p})
		case "SVar":
			name, body, ok := strings.Cut(val, ":")
			if !ok {
				diags = append(diags, Diag{path, "malformed SVar: " + val})
				continue
			}
			cur.SVars[name] = body
		}
		// Unrecognised keys (DeckHas, AI, Draft, ...) are deck-builder and AI
		// hints Forge's own tooling consumes. Ignoring them is correct, not a
		// gap: they carry no rules meaning.
	}
	if err := sc.Err(); err != nil {
		diags = append(diags, Diag{path, err.Error()})
	}
	// derive every face now, once the printed fields are final: ParseBytes is
	// one of the two construction routes into a *Face (the other is the gob
	// decode in LoadRegistry), and both must end with identical derived
	// values.
	for _, f := range c.Faces {
		f.derive()
	}
	return c, diags
}

// parseSA turns "SP$ DealDamage | NumDmg$ 3" into an SA. The leading token is
// the ability kind and its value is the API name.
func parseSA(path, val string) (*SA, []Diag) {
	p := parseParams(val)
	for _, kind := range [...]string{"SP", "AB", "DB", "ST"} {
		if api, ok := p[kind]; ok {
			delete(p, kind)
			normalizeImplicitTarget(api, p)
			return &SA{Kind: kind, API: api, Params: p, Line: val}, nil
		}
	}
	return nil, []Diag{{path, "ability with no SP$/AB$/DB$/ST$ head: " + val}}
}

// normalizeImplicitTarget supplies the target spec a keyword action's own
// Forge effect class targets but the script omits, so the engine poses the
// same target ask Forge does. It is the single structural home for the
// rewrite: parseSA is the one constructor every printed A: line, every
// link.go-resolved Execute$ SVar body and every runtime cards.ResolveSVar
// reaches, so a DB$ Earthbend in any of those carriers is normalised here.
//
// Forge's EarthbendEffect (MagicCard.addAbility / EarthbendEffect) declares
// its target as TargetLandYouControl: the script carries no ValidTgts$ of its
// own because the keyword action owns it, and without the injection the
// engine's Defined() falls through to c.Source -- the resolving card, not a
// land -- so the animation and counters would land on the wrong object.
func normalizeImplicitTarget(api string, p map[string]string) {
	if api == "Earthbend" && p["ValidTgts"] == "" {
		p["ValidTgts"] = "Land.YouCtrl"
	}
}

// splitStaticModes splits a Static's Mode$ value on commas. The corpus
// prints compound statics as ONE S: line naming several modes over the same
// parameters (Pacifism's "S:Mode$ CantAttack,CantBlock | ValidCard$
// Creature.EnchantedBy"), and every Static.Mode consumer — primitive.go's
// "stat:" capability token, activeStatics, staticEffects, collectAction/
// collectCostStatics, face.go's CDA reads, compiled_catalog.go's
// staticModeCode — compares Mode against ONE literal mode, so the raw comma
// list is a single opaque name no consumer recognises: the static is inert at
// runtime and unsupported in coverage. One Static per mode, all sharing the
// same Params map (which keeps the full Mode$ text; no consumer reads
// Params["Mode"]), is the one structural home: parse is where every printed
// S: line is built, and ParseStaticLines covers the SVar-bodied route.
func splitStaticModes(mode string) []string {
	mode = strings.TrimSpace(mode)
	if !strings.Contains(mode, ",") {
		return []string{mode}
	}
	parts := strings.Split(mode, ",")
	out := make([]string, 0, len(parts))
	for _, m := range parts {
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, m)
		}
	}
	return out
}

// resplitStatics re-applies splitStaticModes to a decoded static list: the
// normalisation for the OTHER Static construction route, the gob decode in
// LoadRegistry. A cache predating the split stores each compound S: line as
// one Static whose Mode still carries the comma text (the full Mode$ param
// survives the gob), so LoadRegistry repairs in memory instead of bumping
// the cache version — the same "repair a stale shared cache without forcing
// a rewrite" discipline the post-decode keyword relink uses.
func resplitStatics(sts []Static) []Static {
	n := 0
	for _, s := range sts {
		if strings.Contains(s.Mode, ",") {
			n += len(splitStaticModes(s.Mode))
		} else {
			n++
		}
	}
	if n == len(sts) {
		return sts
	}
	out := make([]Static, 0, n)
	for _, s := range sts {
		for _, mode := range splitStaticModes(s.Mode) {
			out = append(out, Static{Mode: mode, Params: s.Params})
		}
	}
	return out
}

// ParseStaticLine parses one static body — an S: line's text, or an
// S:-shaped SVar body ("Mode$ Continuous | Affected$ You | ...") — into a
// Static. The face parser reads only printed S: lines; a static held in an
// SVar and granted by another static's AddStaticAbility$ (rules/layers.go's
// static grant walk) needs this to get the SAME shape — one shared pipe
// grammar, so the two readers cannot drift. ok is false only for a body with
// no Mode$ at all (an ability body or a Count$ expression an SVar walk
// handed in by mistake).
func ParseStaticLine(body string) (Static, bool) {
	p := parseParams(body)
	mode := strings.TrimSpace(p["Mode"])
	if mode == "" {
		return Static{}, false
	}
	return Static{Mode: mode, Params: p}, true
}

// ParseStaticLines is ParseStaticLine with the same comma-mode split the
// printed S: line gets (splitStaticModes): one Static per mode over the
// shared Params, in the body's own order. ok is false only when the body has
// no Mode$ at all, exactly like ParseStaticLine — a comma body always yields
// at least one Static. ParseStaticLine itself stays single-entry for its
// Mode=="Continuous" call sites; the two comma-aware call sites (kw_class
// grants and rules/layers.go's static grant walk) range over this one.
func ParseStaticLines(body string) ([]Static, bool) {
	p := parseParams(body)
	modes := splitStaticModes(p["Mode"])
	if len(modes) == 0 || modes[0] == "" {
		return nil, false
	}
	out := make([]Static, 0, len(modes))
	for _, mode := range modes {
		out = append(out, Static{Mode: mode, Params: p})
	}
	return out, true
}

// ParseTriggerLine parses one trigger body — a T: line's text, or a T:-shaped
// SVar body ("Mode$ SpellCast | ValidCard$ Card | ...") — into a Trigger. The
// face parser links only printed T: lines (parse.go's "T" case); a trigger
// held in an SVar and executed by some other machinery (the opening-hand
// Effect registration, rules.registerOpeningEffectTriggers) needs this to get
// the SAME shape — one shared pipe grammar, so the two readers cannot drift.
// ok is false only for a body with no Mode$ at all (an ability body or a
// Count$ expression an SVar walk handed in by mistake).
func ParseTriggerLine(body string) (Trigger, bool) {
	p := parseParams(body)
	mode := strings.TrimSpace(p["Mode"])
	if mode == "" {
		return Trigger{}, false
	}
	return Trigger{Mode: mode, Params: p}, true
}

// ParseReplacementLine parses an Event$ replacement body held in an SVar.
// Bodies without an Event$ key fail closed.
func ParseReplacementLine(body string) (Repl, bool) {
	p := parseParams(body)
	event := strings.TrimSpace(p["Event"])
	if event == "" {
		return Repl{}, false
	}
	return Repl{Event: event, Params: p}, true
}

// parseParams splits a "| Key$ value" chain. Values routinely contain "$" and
// occasionally "|" inside description text, so split on "|" first and then on
// the first "$" only.
func parseParams(val string) map[string]string {
	out := make(map[string]string, 8)
	for seg := range strings.SplitSeq(val, "|") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		k, v, ok := strings.Cut(seg, "$")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out
}
