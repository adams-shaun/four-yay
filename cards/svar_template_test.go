package cards

import (
	"reflect"
	"sync"
	"testing"
)

// TestResolveSVarTemplateMatchesFreshParse: every corpus SVar resolves to a
// tree equal (Kind/API/Params/Line, recursively through Sub) to a fresh
// parse, while each call still returns fresh SA structs.
func TestResolveSVarTemplateMatchesFreshParse(t *testing.T) {
	reg := compiledCorpus(t)
	var fresh func(svars map[string]string, name string, depth int) *SA
	fresh = func(svars map[string]string, name string, depth int) *SA {
		if name == "" || depth > maxSVarDepth {
			return nil
		}
		body, ok := svars[name]
		if !ok {
			body, ok = builtinSVars[name]
		}
		if !ok {
			return nil
		}
		sa, _ := parseSA("", body)
		if sa != nil {
			sa.Sub = fresh(svars, sa.Params["SubAbility"], depth+1)
		}
		return sa
	}
	var same func(a, b *SA) bool
	same = func(a, b *SA) bool {
		if a == nil || b == nil {
			return a == b
		}
		return a.Kind == b.Kind && a.API == b.API && a.Line == b.Line &&
			reflect.DeepEqual(a.Params, b.Params) && same(a.Sub, b.Sub)
	}
	n := 0
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			for name := range f.SVars {
				got, want := ResolveSVar(f.SVars, name), fresh(f.SVars, name, 0)
				if !same(got, want) {
					t.Fatalf("%s SVar %s: template resolve differs from a fresh parse", f.Name, name)
				}
				if got != nil {
					again := ResolveSVar(f.SVars, name)
					if again == got {
						t.Fatalf("%s SVar %s: ResolveSVar returned the same SA twice", f.Name, name)
					}
					n++
				}
			}
		}
	}
	if n == 0 {
		t.Fatal("no SVar resolved")
	}
}

// TestSVarTemplateConcurrent resolves the same bodies from many goroutines
// (run under -race).
func TestSVarTemplateConcurrent(t *testing.T) {
	svars := map[string]string{"A": "DB$ Draw | NumCards$ 1 | SubAbility$ B", "B": "DB$ GainLife | LifeAmount$ 2"}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				sa := ResolveSVar(svars, "A")
				if sa == nil || sa.API != "Draw" || sa.Sub == nil || sa.Sub.ParamStr(PKAffected) != "" || sa.Sub.Params["LifeAmount"] != "2" {
					t.Error("bad resolve")
					return
				}
			}
		}()
	}
	wg.Wait()
}
