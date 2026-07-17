package report

import (
	"testing"

	"github.com/JeremyDev87/theseus/internal/model"
)

func TestExitSemanticsAndIncompletePrecedence(t *testing.T) {
	base := model.VerificationResult{
		Artifact:   model.ArtifactReceipt{PackageName: "fixture", PackageVersion: "1.0.0", SHA256: "abc"},
		Receipts:   []model.RunReceipt{},
		Incomplete: []model.IncompleteEvidence{},
		Comparison: model.ComparisonResult{Findings: []model.Finding{}, AllowedDifferences: []model.AllowedDifference{}},
	}
	pass := Create("fixture", base)
	if pass.Status != "pass" || pass.ExitCode != 0 || pass.SchemaVersion != 1 || pass.Tool.Name != "theseus" || pass.Tool.Version != "0.1.0" {
		t.Fatalf("pass report=%#v", pass)
	}

	driftResult := base
	driftResult.Comparison.Findings = []model.Finding{{Code: "THS-PARITY-001", Probe: "help", Field: "exit", Profiles: []string{"default", "noOptional"}, Message: "drift"}}
	drift := Create("fixture", driftResult)
	if drift.Status != "drift" || drift.ExitCode != 1 {
		t.Fatalf("drift report=%#v", drift)
	}

	incompleteResult := driftResult
	incompleteResult.Incomplete = []model.IncompleteEvidence{{Code: "THS-INCOMPLETE-001", Profile: "default", Stage: "probe", Probe: "help", Message: "timed out"}}
	incomplete := Create("fixture", incompleteResult)
	if incomplete.Status != "incomplete" || incomplete.ExitCode != 2 {
		t.Fatalf("incomplete must outrank drift: %#v", incomplete)
	}
}

func TestTextPreservesReceiptFindingAndAllowedOrder(t *testing.T) {
	report := model.VerificationReport{
		Tool: model.ToolReceipt{Name: "theseus", Version: "0.1.0"}, Status: "drift", ExitCode: 1,
		Artifact: model.ArtifactReceipt{PackageName: "fixture", PackageVersion: "1.0.0", SHA256: "abc"},
		Receipts: []model.RunReceipt{{Profile: "default"}, {Profile: "noOptional"}},
		Comparison: model.ComparisonResult{
			Findings:           []model.Finding{{Code: "THS-PARITY-002", Message: "help differs", Profiles: []string{"default", "noOptional"}, Digests: map[string]string{"default": "left", "noOptional": "right"}}},
			AllowedDifferences: []model.AllowedDifference{{Probe: "version", Field: model.FieldStdout, Profiles: [2]string{"default", "noOptional"}, Reason: "documented"}},
		},
	}
	want := "Theseus 0.1.0\nartifact  fixture@1.0.0\nsha256    abc\nprofiles  default, noOptional\nstatus    DRIFT (exit 1)\n! THS-PARITY-002 help differs\n  default: left\n  noOptional: right\n~ ALLOWED version/stdout default↔noOptional: documented\n"
	if got := Text(report); got != want {
		t.Fatalf("text drift\ngot:  %q\nwant: %q", got, want)
	}
}
