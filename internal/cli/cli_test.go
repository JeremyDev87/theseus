package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JeremyDev87/theseus/internal/evidenceid"
	"github.com/JeremyDev87/theseus/internal/model"
	"github.com/JeremyDev87/theseus/internal/reportdiff"
)

func invoke(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	exit := Main(args, &stdout, &stderr)
	return exit, stdout.String(), stderr.String()
}

func TestHelpVersionAndErrors(t *testing.T) {
	exit, out, errout := invoke("--help")
	wantHelp := "Theseus 0.1.0\n\nDistribution parity tests for packaged CLIs.\n\nUsage:\n  theseus verify <package|directory|tgz> --contract <file> [--format text|json]\n  theseus diff <before.json> <after.json> [--format text|json]\n  theseus --help\n  theseus --version\n\nExit codes:\n  0  all declared contracts hold, or no evidence was introduced\n  1  behavior drift, or evidence was introduced\n  2  input, contract, pack, install, or probe incomplete\n"
	if exit != 0 || out != wantHelp || errout != "" {
		t.Fatalf("help %d %q %q", exit, out, errout)
	}
	exit, out, errout = invoke("--version")
	if exit != 0 || out != "0.1.0\n" || errout != "" {
		t.Fatalf("version %d %q %q", exit, out, errout)
	}
	exit, _, errout = invoke("wat")
	if exit != 2 || !strings.Contains(errout, "unknown command wat") {
		t.Fatalf("unknown %d %q", exit, errout)
	}
	exit, _, errout = invoke("verify", ".", "--format", "yaml")
	if exit != 2 || !strings.Contains(errout, "--format must be text or json") {
		t.Fatalf("format %d %q", exit, errout)
	}
}

func TestMissingContractJSONIncomplete(t *testing.T) {
	exit, out, errout := invoke("verify", ".", "--contract", filepath.Join(t.TempDir(), "missing.json"), "--format", "json")
	if exit != 2 || errout != "" {
		t.Fatalf("exit=%d stderr=%q", exit, errout)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["schemaVersion"] != float64(2) || parsed["identityVersion"] != float64(1) || parsed["status"] != "incomplete" || parsed["exitCode"] != float64(2) {
		t.Fatalf("report=%v", parsed)
	}
	tool, ok := parsed["tool"].(map[string]any)
	if !ok || tool["name"] != "theseus" || tool["version"] != "0.1.0" {
		t.Fatalf("tool=%v", parsed["tool"])
	}
	errorBody, ok := parsed["error"].(map[string]any)
	if !ok || errorBody["code"] != "THS-INCOMPLETE-001" {
		t.Fatalf("error=%v", parsed["error"])
	}
}

func TestDiffCLIExitSemantics(t *testing.T) {
	oldFinding := cliFinding("THS-PARITY-001", "help", "exit", "compare:exit", "old drift")
	newFinding := cliFinding("THS-PARITY-002", "version", "stdout", "compare:stdout", "new drift")
	empty := cliReport(nil)
	oldReport := cliReport([]model.Finding{oldFinding})
	introducedReport := cliReport([]model.Finding{oldFinding, newFinding})

	tests := []struct {
		name       string
		before     model.VerificationReport
		after      model.VerificationReport
		format     string
		wantExit   int
		wantStatus string
	}{
		{name: "clean", before: oldReport, after: oldReport, format: "json", wantExit: 0, wantStatus: "clean"},
		{name: "introduced", before: oldReport, after: introducedReport, format: "json", wantExit: 1, wantStatus: "introduced"},
		{name: "resolved only", before: oldReport, after: empty, format: "json", wantExit: 0, wantStatus: "clean"},
		{name: "text", before: empty, after: oldReport, format: "text", wantExit: 1, wantStatus: "INTRODUCED"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			beforePath := writeCLIReport(t, test.before)
			afterPath := writeCLIReport(t, test.after)
			exit, out, errout := invoke("diff", beforePath, afterPath, "--format", test.format)
			if exit != test.wantExit || errout != "" {
				t.Fatalf("exit=%d stderr=%q stdout=%q", exit, errout, out)
			}
			if test.format == "json" {
				var parsed reportdiff.Result
				if err := json.Unmarshal([]byte(out), &parsed); err != nil {
					t.Fatal(err)
				}
				if parsed.Status != test.wantStatus || parsed.ExitCode != test.wantExit || parsed.SchemaVersion != 2 || parsed.IdentityVersion != 1 {
					t.Fatalf("diff=%#v", parsed)
				}
			} else if !strings.Contains(out, "status    "+test.wantStatus) {
				t.Fatalf("text=%q", out)
			}
		})
	}
}

func TestDiffRejectsSchemaV1AndMalformedReports(t *testing.T) {
	validPath := writeCLIReport(t, cliReport(nil))
	tests := []struct {
		name string
		data string
		want string
	}{
		{name: "schema v1", data: `{"schemaVersion":1}`, want: "schemaVersion 2"},
		{name: "malformed", data: `{"schemaVersion":`, want: "invalid report JSON"},
		{name: "unknown field", data: `{"schemaVersion":2,"identityVersion":1,"unknown":true}`, want: "unknown field"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "invalid.json")
			if err := os.WriteFile(path, []byte(test.data), 0o600); err != nil {
				t.Fatal(err)
			}
			exit, out, errout := invoke("diff", path, validPath, "--format", "json")
			if exit != 2 || errout != "" || !strings.Contains(out, test.want) {
				t.Fatalf("exit=%d stdout=%q stderr=%q want=%q", exit, out, errout, test.want)
			}
		})
	}
}

func cliReport(findings []model.Finding) model.VerificationReport {
	if findings == nil {
		findings = []model.Finding{}
	}
	status, exitCode := "pass", 0
	if len(findings) > 0 {
		status, exitCode = "drift", 1
	}
	return model.VerificationReport{
		SchemaVersion: 2, IdentityVersion: 1,
		Tool: model.ToolReceipt{Name: "theseus", Version: "0.1.0"}, Target: "fixture", Status: status, ExitCode: exitCode,
		Artifact: model.ArtifactReceipt{PackageName: "fixture", PackageVersion: "1.0.0", Filename: "fixture.tgz", SHA256: "fixture-sha", Size: 1},
		Receipts: []model.RunReceipt{completeCLIReceipt("default")}, Incomplete: []model.IncompleteEvidence{},
		Comparison: model.ComparisonResult{Findings: findings, AllowedDifferences: []model.AllowedDifference{}},
	}
}

func completeCLIReceipt(profile string) model.RunReceipt {
	return model.RunReceipt{
		Subject: "installed", Profile: profile, InstallArgs: []string{},
		Environment: model.EnvironmentReceipt{Platform: "darwin", Arch: "arm64", Node: "v20.0.0", NPM: "10.0.0"},
		Runtime: model.RuntimeIdentity{
			Kind: "installed", PackageName: "fixture", PackageVersion: "1.0.0", BinName: "fixture",
			ExecutablePath: "node_modules/.bin/fixture", ExecutableRealPath: "node_modules/fixture/bin.js",
			OptionalDependencies: []model.OptionalDependencyReceipt{}, Canonical: "runtime", SHA256: "runtime-sha",
		},
		Probes: []model.ProbeReceipt{},
	}
}

func cliFinding(code, probe, field, locator, message string) model.Finding {
	value := model.Finding{Code: code, Probe: probe, Field: field, Profiles: []string{"default", "noOptional"}, Locator: locator, Message: message}
	value.ID = evidenceid.Finding(value)
	return value
}

func writeCLIReport(t *testing.T, report model.VerificationReport) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "report.json")
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
