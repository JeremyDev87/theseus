package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/JeremyDev87/theseus/internal/model"
	"github.com/JeremyDev87/theseus/internal/reportdiff"
	"github.com/JeremyDev87/theseus/internal/version"
)

type corpusCase struct {
	Name             string
	Target           string
	ContractPath     string
	ExpectedExit     int
	ExpectedStatus   string
	ExpectedFindings []string
}

func main() {
	binary := os.Getenv("THESEUS_BIN")
	if binary == "" {
		binary = filepath.Join("build", executableName())
	}
	cases := corpusCases()
	failures := 0
	for _, item := range cases {
		command := exec.Command(binary, "verify", item.Target, "--contract", item.ContractPath, "--format", "json")
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		err := command.Run()
		exit := 0
		if err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				exit = exitError.ExitCode()
			} else {
				fmt.Fprintf(os.Stderr, "FAIL %s: %v\n", item.Name, err)
				failures++
				continue
			}
		}
		report, err := reportdiff.Parse(stdout.Bytes())
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAIL %s: invalid verification report: %v; stderr=%s\n", item.Name, err, stderr.String())
			failures++
			continue
		}
		codes := []string{}
		for _, finding := range report.Comparison.Findings {
			codes = append(codes, finding.Code)
		}
		codeText := "none"
		if len(codes) > 0 {
			codeText = strings.Join(codes, ",")
		}
		if err := validateCorpusResult(item, exit, report, codes); err != nil {
			fmt.Fprintf(os.Stderr, "FAIL %s: %v; status=%s findings=%s\n", item.Name, err, report.Status, codeText)
			failures++
			continue
		}
		fmt.Printf("PASS %s: exit=%d status=%s findings=%s\n", item.Name, exit, report.Status, codeText)
	}
	if failures > 0 {
		os.Exit(1)
	}
}

func corpusCases() []corpusCase {
	return []corpusCase{
		{Name: "maximus-strict", Target: "@jeremyfellaz/maximus@0.1.4", ContractPath: "testdata/corpus/maximus.json", ExpectedExit: 1, ExpectedStatus: "drift", ExpectedFindings: []string{"THS-PARITY-002", "THS-RUNTIME-001", "THS-PARITY-001"}},
		{Name: "maximus-allowed", Target: "@jeremyfellaz/maximus@0.1.4", ContractPath: "testdata/corpus/maximus-allowed.json", ExpectedExit: 0, ExpectedStatus: "pass", ExpectedFindings: []string{}},
		{Name: "kratos", Target: "@jeremyfellaz/kratos@0.3.7", ContractPath: "testdata/corpus/kratos.json", ExpectedExit: 1, ExpectedStatus: "drift", ExpectedFindings: []string{"THS-PARITY-001", "THS-PARITY-002", "THS-PARITY-002", "THS-RUNTIME-001", "THS-PARITY-002"}},
		{Name: "legolas", Target: "@jeremyfellaz/legolas@0.1.7", ContractPath: "testdata/corpus/legolas.json", ExpectedExit: 0, ExpectedStatus: "pass", ExpectedFindings: []string{}},
		{Name: "ast-grep", Target: "@ast-grep/cli@0.44.1", ContractPath: "testdata/corpus/ast-grep.json", ExpectedExit: 2, ExpectedStatus: "incomplete", ExpectedFindings: []string{}},
		{Name: "biome", Target: "@biomejs/biome@2.5.4", ContractPath: "testdata/corpus/biome.json", ExpectedExit: 1, ExpectedStatus: "drift", ExpectedFindings: []string{"THS-PARITY-001", "THS-PARITY-002", "THS-PARITY-002", "THS-RUNTIME-001", "THS-PARITY-001", "THS-PARITY-002", "THS-PARITY-002"}},
		{Name: "turbo", Target: "turbo@2.10.5", ContractPath: "testdata/corpus/turbo.json", ExpectedExit: 1, ExpectedStatus: "drift", ExpectedFindings: []string{"THS-PARITY-002", "THS-RUNTIME-001"}},
		{Name: "esbuild", Target: "esbuild@0.28.1", ContractPath: "testdata/corpus/esbuild.json", ExpectedExit: 1, ExpectedStatus: "drift", ExpectedFindings: []string{"THS-RUNTIME-001"}},
		{Name: "oxlint", Target: "oxlint@1.74.0", ContractPath: "testdata/corpus/oxlint.json", ExpectedExit: 1, ExpectedStatus: "drift", ExpectedFindings: []string{"THS-PARITY-001", "THS-PARITY-002", "THS-PARITY-002", "THS-RUNTIME-001", "THS-PARITY-001", "THS-PARITY-002", "THS-PARITY-002"}},
	}
}

func validateCorpusResult(item corpusCase, processExit int, report model.VerificationReport, codes []string) error {
	if processExit != item.ExpectedExit || report.ExitCode != item.ExpectedExit {
		return fmt.Errorf("exit mismatch: process=%d report=%d expected=%d", processExit, report.ExitCode, item.ExpectedExit)
	}
	if report.SchemaVersion != 2 || report.IdentityVersion != 1 {
		return fmt.Errorf("schema mismatch: got schema=%d identity=%d expected schema=2 identity=1", report.SchemaVersion, report.IdentityVersion)
	}
	if report.Tool.Name != "theseus" || report.Tool.Version != version.Version {
		return fmt.Errorf("tool mismatch: got %s@%s expected theseus@%s", report.Tool.Name, report.Tool.Version, version.Version)
	}
	if report.Status != item.ExpectedStatus {
		return fmt.Errorf("status mismatch: got %s expected %s", report.Status, item.ExpectedStatus)
	}
	if !slices.Equal(codes, item.ExpectedFindings) {
		return fmt.Errorf("finding order mismatch: got %v expected %v", codes, item.ExpectedFindings)
	}
	return nil
}

func executableName() string {
	if strings.EqualFold(os.Getenv("GOOS"), "windows") || filepath.Separator == '\\' {
		return "theseus.exe"
	}
	return "theseus"
}
