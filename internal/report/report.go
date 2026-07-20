package report

import (
	"fmt"
	"strings"

	"github.com/JeremyDev87/theseus/internal/evidenceid"
	"github.com/JeremyDev87/theseus/internal/model"
	"github.com/JeremyDev87/theseus/internal/version"
)

func Create(target string, result model.VerificationResult) model.VerificationReport {
	receipts := append([]model.RunReceipt{}, result.Receipts...)
	incomplete := append([]model.IncompleteEvidence{}, result.Incomplete...)
	for index := range incomplete {
		incomplete[index].ID = evidenceid.Incomplete(incomplete[index])
	}
	findings := append([]model.Finding{}, result.Comparison.Findings...)
	for index := range findings {
		findings[index].ID = evidenceid.Finding(findings[index])
	}
	result.Comparison.Findings = findings
	result.Comparison.AllowedDifferences = append([]model.AllowedDifference{}, result.Comparison.AllowedDifferences...)

	status, exitCode := "pass", 0
	if len(incomplete) > 0 {
		status, exitCode = "incomplete", 2
	} else if len(result.Comparison.Findings) > 0 {
		status, exitCode = "drift", 1
	}
	return model.VerificationReport{
		SchemaVersion:   2,
		IdentityVersion: evidenceid.Version,
		Tool:            model.ToolReceipt{Name: "theseus", Version: version.Version},
		Target:          target,
		Status:          status,
		ExitCode:        exitCode,
		Artifact:        result.Artifact,
		Receipts:        receipts,
		Incomplete:      incomplete,
		Comparison:      result.Comparison,
	}
}

func Text(report model.VerificationReport) string {
	profiles := []string{}
	for _, receipt := range report.Receipts {
		profiles = append(profiles, receipt.Profile)
	}
	profileText := strings.Join(profiles, ", ")
	if profileText == "" {
		profileText = "none"
	}
	lines := []string{
		fmt.Sprintf("Theseus %s", report.Tool.Version),
		fmt.Sprintf("artifact  %s@%s", report.Artifact.PackageName, report.Artifact.PackageVersion),
		fmt.Sprintf("sha256    %s", report.Artifact.SHA256),
		fmt.Sprintf("profiles  %s", profileText),
		fmt.Sprintf("status    %s (exit %d)", strings.ToUpper(report.Status), report.ExitCode),
	}
	for _, incomplete := range report.Incomplete {
		suffix := ""
		if incomplete.Probe != "" {
			suffix = "/" + incomplete.Probe
		}
		lines = append(lines, fmt.Sprintf("! %s %s/%s%s: %s", incomplete.Code, incomplete.Profile, incomplete.Stage, suffix, incomplete.Message))
	}
	for _, finding := range report.Comparison.Findings {
		lines = append(lines, fmt.Sprintf("! %s %s", finding.Code, finding.Message))
		if finding.Digests != nil {
			for _, profile := range finding.Profiles {
				if digest, ok := finding.Digests[profile]; ok {
					lines = append(lines, fmt.Sprintf("  %s: %s", profile, digest))
				}
			}
		}
	}
	for _, allowed := range report.Comparison.AllowedDifferences {
		lines = append(lines, fmt.Sprintf("~ ALLOWED %s/%s %s↔%s: %s", allowed.Probe, allowed.Field, allowed.Profiles[0], allowed.Profiles[1], allowed.Reason))
	}
	return strings.Join(lines, "\n") + "\n"
}
