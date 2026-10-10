package cards

// SVarGrantHasActivator reports whether the ability SVar name (resolved
// against svars, then the builtin table, exactly as ResolveSVar does) is an
// activated AB whose body carries an Activator$ parameter -- the one way a
// granted ability can be activated by a player other than its recipient's
// controller. A name that resolves to nothing, or to a non-AB body, is never
// offered as a grant, so it reports false.
//
// It reads the shared immutable template parse and allocates nothing
// (ResolveSVar returns a fresh SA per call; this does not).
func SVarGrantHasActivator(svars map[string]string, name string) bool {
	if name == "" {
		return false
	}
	body, ok := svars[name]
	if !ok {
		body, ok = builtinSVars[name]
	}
	if !ok {
		return false
	}
	tmpl := svarTemplate(body)
	if tmpl == nil || tmpl.Kind != "AB" {
		return false
	}
	return tmpl.ParamStr(PKActivator) != ""
}
