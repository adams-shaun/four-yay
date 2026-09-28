package cards

import "testing"

func TestBackupExpandsEveryNamedGrantSVar(t *testing.T) {
	c, diags := ParseBytes("backup.txt", []byte("Name:Backup fixture\nManaCost:2 W\nTypes:Creature Human\nPT:2/2\n"+
		"K:Backup:2:First,Second\n"+
		"SVar:First:DB$ Pump | KW$ Flying\n"+
		"SVar:Second:DB$ Pump | KW$ Vigilance\nOracle:Backup\n"))
	if len(diags) != 0 {
		t.Fatal(diags)
	}
	if d := c.Link(); len(d) != 0 {
		t.Fatal(d)
	}
	f := c.Faces[0]
	if len(f.Triggers) != 1 || f.Triggers[0].Mode != "ChangesZone" {
		t.Fatalf("Backup trigger expansion = %+v, want one ChangesZone trigger", f.Triggers)
	}
	root := ResolveSVar(f.SVars, f.Triggers[0].Params["Execute"])
	if root == nil || root.API != "PutCounter" || root.Params["CounterNum"] != "2" || root.Sub == nil {
		t.Fatalf("Backup root effect = %+v, want PutCounter 2 followed by both grants", root)
	}
	if root.Sub.API != "Pump" || root.Sub.Params["KW"] != "Flying" || root.Sub.Sub == nil {
		t.Fatalf("first Backup grant = %+v, want Flying followed by Vigilance", root.Sub)
	}
	if root.Sub.Sub.API != "Pump" || root.Sub.Sub.Params["KW"] != "Vigilance" {
		t.Fatalf("second Backup grant = %+v, want Vigilance", root.Sub.Sub)
	}
}
