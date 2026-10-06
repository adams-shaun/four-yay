package cards

func (im *Image) str(id StringID) string {
	if id == 0 || int(id) > len(im.Strs) {
		return ""
	}
	r := im.Strs[id-1]
	end := uint64(r.Offset) + uint64(r.Length)
	if end > uint64(len(im.Blob)) {
		return ""
	}
	return string(im.Blob[r.Offset:uint32(end)])
}
func (im *Image) slice(s Span, n int) (int, int) {
	a := int(s.Start)
	z := a + int(s.Count)
	if s.Count == 0 || z > n {
		return 0, 0
	}
	return a, z
}
func (im *Image) params(s Span) map[string]string {
	if s.Count == 0 && s.Start == ^uint32(0) {
		return make(map[string]string)
	}
	a, z := im.slice(s, len(im.Params))
	if a == z {
		return nil
	}
	m := make(map[string]string, z-a)
	for _, p := range im.Params[a:z] {
		m[im.str(p.Key)] = im.str(p.Value)
	}
	return m
}
func (im *Image) words(s Span) []string {
	a, z := im.slice(s, len(im.Words))
	if a == z {
		return nil
	}
	v := make([]string, z-a)
	for i, id := range im.Words[a:z] {
		v[i] = im.str(id)
	}
	return v
}
func (im *Image) node(id NodeID) *SA {
	if id == 0 || int(id) > len(im.Nodes) {
		return nil
	}
	r := im.Nodes[id-1]
	return &SA{Kind: im.str(r.Kind), API: im.str(r.API), Line: im.str(r.Line), Params: im.params(r.Params), Sub: im.node(r.Sub)}
}
func (im *Image) rawCard(i int) *Card {
	if im == nil || i < 0 || i >= len(im.Cards) {
		return nil
	}
	return im.rawRecord(im.Cards[i])
}
func (im *Image) rawToken(i int) *Card {
	if im == nil || i < 0 || i >= len(im.Tokens) {
		return nil
	}
	return im.rawRecord(im.Tokens[i].Card)
}
func (im *Image) rawRecord(r CardRec) *Card {
	c := &Card{Path: im.str(r.Path), AlternateMode: im.str(r.AlternateMode)}
	a, z := im.slice(r.Faces, len(im.Faces))
	if a == z {
		return c
	}
	c.Faces = make([]*Face, z-a)
	for j, fr := range im.Faces[a:z] {
		if fr.Nil != 0 {
			continue
		}
		f := &Face{SpecializeColor: im.str(fr.SpecializeColor), CopyFaceFrom: im.str(fr.CopyFaceFrom), Name: im.str(fr.Name), ManaCost: im.str(fr.ManaCost), PT: im.str(fr.PT), Loyalty: im.str(fr.Loyalty), Defense: im.str(fr.Defense), Colors: im.str(fr.Colors), Oracle: im.str(fr.Oracle), Types: im.words(fr.Types), Keywords: im.words(fr.Keywords), Aliases: im.words(fr.Aliases), SVars: make(map[string]string)}
		x, y := im.slice(fr.Abilities, len(im.FaceSAs))
		if x != y {
			f.Abilities = make([]*SA, y-x)
			for k, id := range im.FaceSAs[x:y] {
				f.Abilities[k] = im.node(id)
			}
		}
		x, y = im.slice(fr.Triggers, len(im.Trigs))
		if x != y {
			f.Triggers = make([]Trigger, y-x)
			for k, t := range im.Trigs[x:y] {
				f.Triggers[k] = Trigger{Mode: im.str(t.Mode), Params: im.params(t.Params), Effect: im.node(t.Effect)}
			}
		}
		x, y = im.slice(fr.Statics, len(im.Stats))
		if x != y {
			f.Statics = make([]Static, y-x)
			for k, s := range im.Stats[x:y] {
				f.Statics[k] = Static{Mode: im.str(s.Mode), Params: im.params(s.Params)}
			}
		}
		x, y = im.slice(fr.Repls, len(im.Repls))
		if x != y {
			f.Repls = make([]Repl, y-x)
			for k, v := range im.Repls[x:y] {
				f.Repls[k] = Repl{Event: im.str(v.Event), Params: im.params(v.Params), With: im.node(v.With)}
			}
		}
		x, y = im.slice(fr.SVars, len(im.SVars))
		if fr.SVars.Start == ^uint32(0) {
			f.SVars = make(map[string]string)
		}
		if x != y {
			f.SVars = make(map[string]string, y-x)
			for _, v := range im.SVars[x:y] {
				f.SVars[im.str(v.Name)] = im.str(v.Body)
			}
		}
		c.Faces[j] = f
	}
	return c
}
