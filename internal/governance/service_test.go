package governance

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReportRetentionOnlyAcceptsGeneratedPattern(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().AddDate(0, 0, -100)
	for _, name := range []string{"auditoria-10.pdf", "auditoria-11-20260101-120000.pdf", "private.pdf", "../escape.pdf"} {
		if filepath.Base(name) != name {
			continue
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
		_ = os.Chtimes(path, old, old)
	}
	files, err := retentionReports(dir, time.Now().AddDate(0, 0, -90))
	if err != nil || len(files) != 2 {
		t.Fatalf("arquivos=%v err=%v", files, err)
	}
}
func TestDeleteRequiresMatchingConfirmationBeforeDatabase(t *testing.T) {
	_, err := (Service{}).DeleteDeal(context.Background(), 8260, "9999")
	if err == nil {
		t.Fatal("confirmação divergente aceita")
	}
}
