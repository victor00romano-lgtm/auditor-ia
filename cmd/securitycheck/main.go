package main

import (
	"fmt"
	platformconfig "github.com/portfolio/auditor-ia/internal/platform/config"
	"github.com/portfolio/auditor-ia/internal/platform/security"
	"os"
	"path/filepath"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "securitycheck:", security.Error(err))
		os.Exit(1)
	}
	fmt.Println("securitycheck: configuração básica aprovada; nenhuma alteração realizada")
}
func run() error {
	c, err := platformconfig.Load(platformconfig.SecurityCheck)
	if err != nil {
		return err
	}
	reports := os.Getenv("REPORTS_DIR")
	if reports == "" {
		reports = "./reports"
	}
	absolute, err := filepath.Abs(reports)
	if err != nil {
		return fmt.Errorf("REPORTS_DIR inválido")
	}
	if filepath.Clean(absolute) == filepath.VolumeName(absolute)+string(os.PathSeparator) {
		return fmt.Errorf("REPORTS_DIR não pode ser raiz do volume")
	}
	if c.AppEnv == "production" && c.PrivacyMode == "preserve" && !c.AllowPII {
		return fmt.Errorf("modo preserve inseguro")
	}
	return nil
}
