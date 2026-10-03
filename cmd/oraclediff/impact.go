package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/adams-shaun/gorge/compliance/adopt"
)

// runImpact prints the primitive impact table (section 11.3 C5): every
// unsupported primitive, the tournament cards it blocks per declared-target
// format and the sets implementing it unlocks, ranked by format.
func runImpact(dir string, top int, asJSON bool) error {
	reg, err := loadReg(dir)
	if err != nil {
		return err
	}
	cs, err := adopt.Build(reg, ".")
	if err != nil {
		return err
	}
	rows := cs.Impact()
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		for _, r := range rows {
			if err := enc.Encode(r); err != nil {
				return err
			}
		}
		return nil
	}
	fmt.Printf("# Primitive impact (%d cards over %d sets)\n\n", len(cs.Cards), len(cs.Sets))
	cs.WriteImpact(os.Stdout, rows, top)
	return nil
}
