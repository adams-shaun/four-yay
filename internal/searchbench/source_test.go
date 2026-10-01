package searchbench

import (
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestAuditCSVUsesBothTopPlayerThresholdsAndGzipMagic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := gzip.NewWriter(f)
	_, err = z.Write([]byte("expansion,event_type,user_game_win_rate_bucket,user_n_games_bucket\nFDN,PremierDraft,0.60,100\nFDN,PremierDraft,0.59,500\nFDN,PremierDraft,0.80,50\nMH3,PremierDraft,0.90,500\nFDN,PremierDraft,bad,100\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := AuditCSV(path, DefaultSourceFilter())
	if err != nil {
		t.Fatal(err)
	}
	if got != (SourceAudit{Rows: 5, FDNPremierRows: 4, EligibleRows: 1, MalformedEligibilityRows: 1}) {
		t.Fatalf("audit = %+v", got)
	}
}

func TestAuditCSVRejectsMissingEligibilityColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.csv")
	if err := os.WriteFile(path, []byte("expansion,event_type\nFDN,PremierDraft\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := AuditCSV(path, DefaultSourceFilter()); err == nil {
		t.Fatal("AuditCSV accepted missing columns")
	}
}
