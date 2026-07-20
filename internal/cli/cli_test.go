package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JeremyDev87/theseus/internal/canonical"
	"github.com/JeremyDev87/theseus/internal/evidenceid"
	"github.com/JeremyDev87/theseus/internal/model"
	"github.com/JeremyDev87/theseus/internal/reportdiff"
	"github.com/JeremyDev87/theseus/internal/runtimeidentity"
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

func TestDiffRejectsDuplicateKeysAndTimedOutPassReport(t *testing.T) {
	valid := cliReport(nil)
	validJSON, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}

	timedOut := cliReport(nil)
	timedOut.Receipts[0].Probes = []model.ProbeReceipt{{
		ID: "help", Argv: []string{"--help"}, TimedOut: true,
		StdoutSHA256: canonical.SHA256String(""), StderrSHA256: canonical.SHA256String(""),
	}}
	timedOutJSON, err := json.Marshal(timedOut)
	if err != nil {
		t.Fatal(err)
	}

	tampered := cliReport(nil)
	tampered.Receipts[0].Probes[0].Stdout = "tampered"
	tamperedJSON, err := json.Marshal(tampered)
	if err != nil {
		t.Fatal(err)
	}

	findingActual := cliReport([]model.Finding{cliFinding("THS-PARITY-001", "help", "exit", "compare:exit", "exit drift")})
	findingActual.Comparison.Findings[0].Actual = map[string]any{"default": 999, "noOptional": 999}
	findingActualJSON, err := json.Marshal(findingActual)
	if err != nil {
		t.Fatal(err)
	}

	findingExpected := cliReport([]model.Finding{cliFinding("THS-PARITY-001", "help", "exit", "compare:exit", "exit drift")})
	findingExpected.Comparison.Findings[0].Expected = "forged"
	findingExpectedJSON, err := json.Marshal(findingExpected)
	if err != nil {
		t.Fatal(err)
	}

	allowedExtraProfileJSON := []byte(strings.Replace(string(validJSON), `"allowedDifferences":[]`, `"allowedDifferences":[{"probe":"help","field":"stdout","profiles":["default","noOptional","ghost"],"reason":"fixture"}]`, 1))

	tests := []struct {
		name string
		data []byte
		want string
	}{
		{name: "duplicate root key", data: []byte(strings.Replace(string(validJSON), `"schemaVersion":2`, `"schemaVersion":1,"schemaVersion":2`, 1)), want: "duplicate JSON object key"},
		{name: "duplicate nested key", data: []byte(strings.Replace(string(validJSON), `"name":"theseus"`, `"name":"other","name":"theseus"`, 1)), want: "duplicate JSON object key"},
		{name: "timed out pass", data: timedOutJSON, want: "timed out probe requires matching incomplete evidence"},
		{name: "tampered probe digest", data: tamperedJSON, want: "stdoutSha256 mismatch"},
		{name: "tampered finding actual", data: findingActualJSON, want: "actual payload does not match receipts"},
		{name: "tampered parity expected", data: findingExpectedJSON, want: "compare finding must not contain expected payload"},
		{name: "allowed difference extra profile", data: allowedExtraProfileJSON, want: "profiles must contain exactly two entries"},
	}

	validPath := writeCLIReport(t, valid)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "invalid.json")
			if err := os.WriteFile(path, test.data, 0o600); err != nil {
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
	receipts := []model.RunReceipt{completeCLIReceipt("default"), completeCLIReceipt("noOptional")}
	for index := range findings {
		applyCLIFindingDifference(&findings[index], receipts)
	}
	return model.VerificationReport{
		SchemaVersion: 2, IdentityVersion: 1,
		Tool: model.ToolReceipt{Name: "theseus", Version: "0.1.0"}, Target: "fixture", Status: status, ExitCode: exitCode,
		Artifact: model.ArtifactReceipt{PackageName: "fixture", PackageVersion: "1.0.0", Filename: "fixture.tgz", SHA256: canonical.SHA256String("fixture artifact"), Size: 1},
		Receipts: receipts, Incomplete: []model.IncompleteEvidence{},
		Comparison: model.ComparisonResult{Findings: findings, AllowedDifferences: []model.AllowedDifference{}},
	}
}

func completeCLIReceipt(profile string) model.RunReceipt {
	runtime, err := runtimeidentity.Create(model.RuntimeIdentity{
		Kind: "installed", PackageName: "fixture", PackageVersion: "1.0.0", BinName: "fixture",
		ExecutablePath: "node_modules/.bin/fixture", ExecutableRealPath: "node_modules/fixture/bin.js",
		OptionalDependencies: []model.OptionalDependencyReceipt{},
	})
	if err != nil {
		panic(err)
	}
	exit := 0
	return model.RunReceipt{
		Subject: "installed", Profile: profile, InstallArgs: []string{},
		Environment: model.EnvironmentReceipt{Platform: "darwin", Arch: "arm64", Node: "v20.0.0", NPM: "10.0.0"},
		Runtime:     runtime,
		Probes: []model.ProbeReceipt{
			{ID: "help", Argv: []string{"--help"}, ExitCode: &exit, StdoutSHA256: canonical.SHA256String(""), StderrSHA256: canonical.SHA256String("")},
			{ID: "version", Argv: []string{"--version"}, ExitCode: &exit, StdoutSHA256: canonical.SHA256String(""), StderrSHA256: canonical.SHA256String("")},
		},
	}
}

func applyCLIFindingDifference(finding *model.Finding, receipts []model.RunReceipt) {
	if len(finding.Profiles) != 2 {
		return
	}
	right := &receipts[1]
	for index := range right.Probes {
		if right.Probes[index].ID != finding.Probe {
			continue
		}
		if finding.Field == "exit" {
			exit := 1
			right.Probes[index].ExitCode = &exit
			finding.Actual = map[string]any{"default": *receipts[0].Probes[index].ExitCode, "noOptional": *right.Probes[index].ExitCode}
		} else if finding.Field == "stdout" {
			right.Probes[index].Stdout = "changed:" + finding.Probe
			right.Probes[index].StdoutSHA256 = canonical.SHA256String(right.Probes[index].Stdout)
			finding.Digests = map[string]string{
				"default":    receipts[0].Probes[index].StdoutSHA256,
				"noOptional": right.Probes[index].StdoutSHA256,
			}
			finding.Actual = map[string]any{"default": receipts[0].Probes[index].Stdout, "noOptional": right.Probes[index].Stdout}
		}
		return
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
