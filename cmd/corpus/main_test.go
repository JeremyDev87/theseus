package main

import (
	"strings"
	"testing"

	"github.com/JeremyDev87/theseus/internal/model"
)

func TestValidateCorpusResultChecksCompleteLedger(t *testing.T) {
	item := corpusCase{Name: "fixture", ExpectedExit: 1, ExpectedStatus: "drift", ExpectedFindings: []string{"THS-PARITY-001", "THS-RUNTIME-001"}}
	report := model.VerificationReport{
		SchemaVersion: 1,
		Tool:          model.ToolReceipt{Name: "theseus", Version: "0.1.0"},
		Status:        "drift",
		ExitCode:      1,
	}
	if err := validateCorpusResult(item, 1, report, []string{"THS-PARITY-001", "THS-RUNTIME-001"}); err != nil {
		t.Fatalf("valid ledger rejected: %v", err)
	}

	cases := []struct {
		name   string
		exit   int
		report model.VerificationReport
		codes  []string
		want   string
	}{
		{name: "process exit", exit: 2, report: report, codes: item.ExpectedFindings, want: "exit mismatch"},
		{name: "schema", exit: 1, report: withSchema(report, 2), codes: item.ExpectedFindings, want: "schema mismatch"},
		{name: "tool", exit: 1, report: withTool(report, "other", "0.1.0"), codes: item.ExpectedFindings, want: "tool mismatch"},
		{name: "status", exit: 1, report: withStatus(report, "pass"), codes: item.ExpectedFindings, want: "status mismatch"},
		{name: "finding order", exit: 1, report: report, codes: []string{"THS-RUNTIME-001", "THS-PARITY-001"}, want: "finding order mismatch"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := validateCorpusResult(item, test.exit, test.report, test.codes)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want %q", err, test.want)
			}
		})
	}
}

func withSchema(report model.VerificationReport, schema int) model.VerificationReport {
	report.SchemaVersion = schema
	return report
}

func withTool(report model.VerificationReport, name, version string) model.VerificationReport {
	report.Tool = model.ToolReceipt{Name: name, Version: version}
	return report
}

func withStatus(report model.VerificationReport, status string) model.VerificationReport {
	report.Status = status
	return report
}
