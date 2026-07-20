package reportdiff

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/JeremyDev87/theseus/internal/canonical"
	"github.com/JeremyDev87/theseus/internal/evidenceid"
	"github.com/JeremyDev87/theseus/internal/model"
	"github.com/JeremyDev87/theseus/internal/runtimeidentity"
)

func TestDiffClassifiesLifecycle(t *testing.T) {
	persistedBefore := finding("THS-PARITY-001", "help", "exit", "compare:exit", "before wording")
	persistedAfter := persistedBefore
	persistedAfter.Message = "after wording"
	resolved := incomplete("default", "install", "", "old install failure")
	introduced := finding("THS-PARITY-002", "version", "stdout", "compare:stdout", "new stdout drift")

	before := verificationReport([]model.Finding{persistedBefore}, []model.IncompleteEvidence{resolved})
	after := verificationReport([]model.Finding{introduced, persistedAfter}, nil)

	got, err := Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if got.ExitCode != 1 || got.Status != "introduced" {
		t.Fatalf("status=%q exit=%d", got.Status, got.ExitCode)
	}
	assertIDs(t, got.Introduced, introduced.ID)
	assertIDs(t, got.Resolved, resolved.ID)
	assertIDs(t, got.Persisted, persistedBefore.ID)
	if got.Persisted[0].Before == nil || got.Persisted[0].After == nil || got.Persisted[0].Before.Finding == nil || got.Persisted[0].After.Finding == nil || got.Persisted[0].Before.Finding.Message == got.Persisted[0].After.Finding.Message {
		t.Fatalf("persisted transition must retain both versions: %#v", got.Persisted[0])
	}
}

func TestDiffIsDeterministicAndCleanWithoutIntroducedEvidence(t *testing.T) {
	first := finding("THS-PARITY-002", "z", "stdout", "compare:stdout", "z")
	second := finding("THS-PARITY-001", "a", "exit", "compare:exit", "a")
	before := verificationReport([]model.Finding{first, second}, nil)
	after := verificationReport([]model.Finding{second, first}, nil)

	got, err := Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if got.ExitCode != 0 || got.Status != "clean" || len(got.Introduced) != 0 || len(got.Resolved) != 0 {
		t.Fatalf("unexpected clean diff: %#v", got)
	}
	if len(got.Persisted) != 2 || got.Persisted[0].ID > got.Persisted[1].ID {
		t.Fatalf("persisted output is not ID-sorted: %#v", got.Persisted)
	}
	firstJSON, _ := json.Marshal(got)
	repeated, err := Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, _ := json.Marshal(repeated)
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("non-deterministic output\nfirst=%s\nsecond=%s", firstJSON, secondJSON)
	}
}

func TestParseRejectsSchemaV1UnknownFieldsDuplicateAndMismatchedIDs(t *testing.T) {
	valid := verificationReport([]model.Finding{finding("THS-PARITY-001", "help", "exit", "compare:exit", "drift")}, nil)
	validJSON, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(validJSON); err != nil {
		t.Fatalf("valid report rejected: %v", err)
	}

	cases := []struct {
		name string
		data []byte
		want string
	}{
		{name: "malformed", data: []byte(`{"schemaVersion":`), want: "invalid report JSON"},
		{name: "schema v1", data: []byte(strings.Replace(string(validJSON), `"schemaVersion":2`, `"schemaVersion":1`, 1)), want: "schemaVersion 2"},
		{name: "unknown root", data: []byte(strings.Replace(string(validJSON), `{"schemaVersion"`, `{"unexpected":true,"schemaVersion"`, 1)), want: "unknown field"},
		{name: "duplicate root key", data: []byte(strings.Replace(string(validJSON), `"schemaVersion":2`, `"schemaVersion":1,"schemaVersion":2`, 1)), want: "duplicate JSON object key"},
		{name: "duplicate nested key", data: []byte(strings.Replace(string(validJSON), `"name":"theseus"`, `"name":"other","name":"theseus"`, 1)), want: "duplicate JSON object key"},
		{name: "mismatched id", data: []byte(strings.Replace(string(validJSON), `ths:v1:`, `ths:v1:0`, 1)), want: "identity mismatch"},
	}

	duplicate := valid
	duplicate.Comparison.Findings = append(duplicate.Comparison.Findings, duplicate.Comparison.Findings[0])
	duplicateJSON, _ := json.Marshal(duplicate)
	cases = append(cases, struct {
		name string
		data []byte
		want string
	}{name: "duplicate", data: duplicateJSON, want: "duplicate evidence id"})

	inconsistent := valid
	inconsistent.Status = "pass"
	inconsistent.ExitCode = 0
	inconsistentJSON, _ := json.Marshal(inconsistent)
	cases = append(cases, struct {
		name string
		data []byte
		want string
	}{name: "evidence status", data: inconsistentJSON, want: "evidence requires status"})

	partial := valid
	partial.Artifact = model.ArtifactReceipt{}
	partialJSON, _ := json.Marshal(partial)
	cases = append(cases, struct {
		name string
		data []byte
		want string
	}{name: "partial envelope", data: partialJSON, want: "artifact receipt is incomplete"})

	partialReceipt := valid
	partialReceipt.Receipts = []model.RunReceipt{{Profile: "default"}}
	partialReceiptJSON, _ := json.Marshal(partialReceipt)
	cases = append(cases, struct {
		name string
		data []byte
		want string
	}{name: "partial receipt", data: partialReceiptJSON, want: "subject and profile must not be empty"})

	timedOutPass := verificationReport(nil, nil)
	timedOutPass.Receipts[0].Probes = []model.ProbeReceipt{{
		ID: "help", Argv: []string{"--help"}, TimedOut: true,
		StdoutSHA256: canonical.SHA256String(""), StderrSHA256: canonical.SHA256String(""),
	}}
	timedOutPassJSON, _ := json.Marshal(timedOutPass)
	cases = append(cases, struct {
		name string
		data []byte
		want string
	}{name: "timed out pass receipt", data: timedOutPassJSON, want: "timed out probe requires matching incomplete evidence"})

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Parse(test.data); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want substring %q", err, test.want)
			}
		})
	}
}

func TestParseAcceptsProbeFailureWithMatchingIncompleteEvidence(t *testing.T) {
	tests := []struct {
		name     string
		timedOut bool
		signal   *string
	}{
		{name: "spawn failure without process outcome"},
		{name: "timed out process", timedOut: true, signal: stringPointer("SIGKILL")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence := incomplete("default", "probe", "help", test.name)
			report := verificationReport(nil, []model.IncompleteEvidence{evidence})
			report.Receipts[0].Probes = []model.ProbeReceipt{{
				ID: "help", Argv: []string{"--help"}, Signal: test.signal, TimedOut: test.timedOut,
				StdoutSHA256: canonical.SHA256String(""), StderrSHA256: canonical.SHA256String(""),
			}}
			data, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Parse(data); err != nil {
				t.Fatalf("matching incomplete evidence was rejected: %v", err)
			}
		})
	}
}

func TestParseRejectsReceiptSelfIntegrityTampering(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*model.VerificationReport)
		want   string
	}{
		{name: "artifact digest shape", mutate: func(report *model.VerificationReport) {
			report.Artifact.SHA256 = "ABC123"
		}, want: "artifact sha256 must be lowercase 64-hex"},
		{name: "runtime canonical payload", mutate: func(report *model.VerificationReport) {
			report.Receipts[0].Runtime.Canonical += " "
			report.Receipts[0].Runtime.SHA256 = canonical.SHA256String(report.Receipts[0].Runtime.Canonical)
		}, want: "runtime canonical payload mismatch"},
		{name: "runtime digest", mutate: func(report *model.VerificationReport) {
			report.Receipts[0].Runtime.SHA256 = strings.Repeat("0", 64)
		}, want: "runtime sha256 mismatch"},
		{name: "probe stdout digest", mutate: func(report *model.VerificationReport) {
			report.Receipts[0].Probes[0].Stdout = "tampered"
		}, want: "stdoutSha256 mismatch"},
		{name: "profile canonicality", mutate: func(report *model.VerificationReport) {
			report.Receipts[0].Profile = " default "
		}, want: "profile must be canonical"},
		{name: "subject runtime kind", mutate: func(report *model.VerificationReport) {
			report.Receipts[0].Subject = "source"
		}, want: "subject and runtime kind must match"},
		{name: "installed package artifact", mutate: func(report *model.VerificationReport) {
			runtime := report.Receipts[0].Runtime
			runtime.PackageName = "other"
			report.Receipts[0].Runtime, _ = runtimeidentity.Create(runtime)
		}, want: "runtime package must match artifact"},
		{name: "duplicate optional dependency", mutate: func(report *model.VerificationReport) {
			runtime := report.Receipts[0].Runtime
			runtime.OptionalDependencies = []model.OptionalDependencyReceipt{{Name: "native", Version: "1"}, {Name: "native", Version: "1"}}
			report.Receipts[0].Runtime, _ = runtimeidentity.Create(runtime)
		}, want: "duplicate optional dependency"},
		{name: "finding profile reference", mutate: func(report *model.VerificationReport) {
			finding := report.Comparison.Findings[0]
			finding.Profiles[1] = "ghost"
			finding.ID = evidenceid.Finding(finding)
			report.Comparison.Findings[0] = finding
		}, want: "references unknown profile"},
		{name: "finding probe reference", mutate: func(report *model.VerificationReport) {
			finding := report.Comparison.Findings[0]
			finding.Probe = "ghost"
			finding.ID = evidenceid.Finding(finding)
			report.Comparison.Findings[0] = finding
		}, want: "references unknown probe"},
		{name: "finding digest reference", mutate: func(report *model.VerificationReport) {
			report.Comparison.Findings[0].Digests["default"] = strings.Repeat("0", 64)
		}, want: "finding digest does not match receipt"},
		{name: "allowed difference unknown profile", mutate: func(report *model.VerificationReport) {
			report.Comparison.AllowedDifferences[0].Profiles[1] = "ghost"
		}, want: "references unknown profile"},
		{name: "allowed difference without observed delta", mutate: func(report *model.VerificationReport) {
			left := &report.Receipts[1].Probes[0]
			left.Stdout = ""
			left.StdoutSHA256 = canonical.SHA256String("")
		}, want: "does not describe an observed difference"},
		{name: "duplicate allowed difference", mutate: func(report *model.VerificationReport) {
			report.Comparison.AllowedDifferences = append(report.Comparison.AllowedDifferences, report.Comparison.AllowedDifferences[0])
		}, want: "duplicate allowed difference"},
		{name: "allowed difference finding collision", mutate: func(report *model.VerificationReport) {
			withFinding := verificationReport([]model.Finding{finding("THS-PARITY-002", "help", "stdout", "compare:stdout", "stdout drift")}, nil)
			*report = withFinding
			report.Comparison.AllowedDifferences = []model.AllowedDifference{{Probe: "help", Profiles: [2]string{"default", "noOptional"}, Field: model.FieldStdout, Reason: "expected"}}
		}, want: "allowed difference conflicts with finding"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var report model.VerificationReport
			if strings.HasPrefix(test.name, "finding") {
				report = verificationReport([]model.Finding{finding("THS-PARITY-002", "help", "stdout", "compare:stdout", "stdout drift")}, nil)
			} else if strings.HasPrefix(test.name, "allowed") || test.name == "duplicate allowed difference" {
				report = allowedDifferenceReport()
			} else {
				report = verificationReport(nil, nil)
			}
			test.mutate(&report)
			data, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Parse(data); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want substring %q", err, test.want)
			}
		})
	}
}

func TestParseAcceptsReceiptBoundFindingAndAllowedDifference(t *testing.T) {
	for name, report := range map[string]model.VerificationReport{
		"finding":            verificationReport([]model.Finding{finding("THS-PARITY-002", "help", "stdout", "compare:stdout", "stdout drift")}, nil),
		"allowed difference": allowedDifferenceReport(),
	} {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Parse(data); err != nil {
				t.Fatalf("valid receipt-bound report rejected: %v", err)
			}
		})
	}
}

func TestParseAcceptsProducerPermittedReceiptText(t *testing.T) {
	tests := []struct {
		name   string
		report model.VerificationReport
	}{
		{name: "installed profile named source", report: func() model.VerificationReport {
			report := verificationReport(nil, nil)
			report.Receipts[0].Profile = "source"
			return report
		}()},
		{name: "probe id with surrounding whitespace", report: func() model.VerificationReport {
			report := verificationReport(nil, nil)
			for index := range report.Receipts {
				report.Receipts[index].Probes[0].ID = " help "
			}
			return report
		}()},
		{name: "allowed difference reason with surrounding whitespace", report: func() model.VerificationReport {
			report := allowedDifferenceReport()
			report.Comparison.AllowedDifferences[0].Reason = " fixture contract "
			return report
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data, err := json.Marshal(test.report)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Parse(data); err != nil {
				t.Fatalf("producer-permitted report text was rejected: %v", err)
			}
		})
	}
}

func allowedDifferenceReport() model.VerificationReport {
	report := verificationReport(nil, nil)
	probe := &report.Receipts[1].Probes[0]
	probe.Stdout = "allowed difference"
	probe.StdoutSHA256 = canonical.SHA256String(probe.Stdout)
	report.Comparison.AllowedDifferences = []model.AllowedDifference{{
		Probe: "help", Field: model.FieldStdout, Profiles: [2]string{"default", "noOptional"}, Reason: "fixture contract",
	}}
	return report
}

func TestLanguageNeutralDiffLifecycleFixture(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "conformance")
	readReport := func(name string) model.VerificationReport {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, "reports", name))
		if err != nil {
			t.Fatal(err)
		}
		value, err := Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	actual, err := Compare(readReport("diff-before.json"), readReport("diff-after.json"))
	if err != nil {
		t.Fatal(err)
	}

	var expected struct {
		Introduced []string `json:"introduced"`
		Resolved   []string `json:"resolved"`
		Persisted  []string `json:"persisted"`
	}
	expectedBytes, err := os.ReadFile(filepath.Join(root, "expected", "diff-lifecycle.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(expectedBytes, &expected); err != nil {
		t.Fatal(err)
	}
	if got := itemIDs(actual.Introduced); !slices.Equal(got, expected.Introduced) {
		t.Fatalf("introduced=%v want %v", got, expected.Introduced)
	}
	if got := itemIDs(actual.Resolved); !slices.Equal(got, expected.Resolved) {
		t.Fatalf("resolved=%v want %v", got, expected.Resolved)
	}
	if got := itemIDs(actual.Persisted); !slices.Equal(got, expected.Persisted) {
		t.Fatalf("persisted=%v want %v", got, expected.Persisted)
	}
}

func verificationReport(findings []model.Finding, incompleteEvidence []model.IncompleteEvidence) model.VerificationReport {
	findings = nonNilFindings(findings)
	incompleteEvidence = nonNilIncomplete(incompleteEvidence)
	receipts := []model.RunReceipt{completeReceipt("default"), completeReceipt("noOptional")}
	for index := range findings {
		ensureFindingProbe(findings[index], receipts)
		applyFindingDifference(&findings[index], receipts)
	}
	status, exitCode := "pass", 0
	if len(incompleteEvidence) > 0 {
		status, exitCode = "incomplete", 2
	} else if len(findings) > 0 {
		status, exitCode = "drift", 1
	}
	return model.VerificationReport{
		SchemaVersion: 2, IdentityVersion: 1,
		Tool:   model.ToolReceipt{Name: "theseus", Version: "0.1.0"},
		Target: "fixture", Status: status, ExitCode: exitCode,
		Artifact: model.ArtifactReceipt{PackageName: "fixture", PackageVersion: "1.0.0", Filename: "fixture.tgz", SHA256: canonical.SHA256String("fixture artifact"), Size: 1},
		Receipts: receipts, Incomplete: incompleteEvidence,
		Comparison: model.ComparisonResult{Findings: findings, AllowedDifferences: []model.AllowedDifference{}},
	}
}

func ensureFindingProbe(finding model.Finding, receipts []model.RunReceipt) {
	exit := 0
	for index := range receipts {
		if probeForID(receipts[index].Probes, finding.Probe) != nil {
			continue
		}
		receipts[index].Probes = append(receipts[index].Probes, model.ProbeReceipt{
			ID: finding.Probe, Argv: []string{}, ExitCode: &exit,
			StdoutSHA256: canonical.SHA256String(""), StderrSHA256: canonical.SHA256String(""),
		})
	}
}

func applyFindingDifference(finding *model.Finding, receipts []model.RunReceipt) {
	if len(finding.Profiles) != 2 {
		return
	}
	left := receiptForProfile(receipts, finding.Profiles[0])
	right := receiptForProfile(receipts, finding.Profiles[1])
	if left == nil || right == nil {
		return
	}
	leftProbe := probeForID(left.Probes, finding.Probe)
	rightProbe := probeForID(right.Probes, finding.Probe)
	if leftProbe == nil || rightProbe == nil {
		return
	}
	switch finding.Field {
	case "exit":
		exit := 1
		rightProbe.ExitCode = &exit
	case "stdout":
		rightProbe.Stdout = "changed:" + finding.Probe
		rightProbe.StdoutSHA256 = canonical.SHA256String(rightProbe.Stdout)
		finding.Digests = map[string]string{left.Profile: leftProbe.StdoutSHA256, right.Profile: rightProbe.StdoutSHA256}
	case "stderr":
		rightProbe.Stderr = "changed:" + finding.Probe
		rightProbe.StderrSHA256 = canonical.SHA256String(rightProbe.Stderr)
		finding.Digests = map[string]string{left.Profile: leftProbe.StderrSHA256, right.Profile: rightProbe.StderrSHA256}
	case "runtime":
		right.Runtime.BinName += "-changed"
		updated, err := runtimeidentity.Create(right.Runtime)
		if err != nil {
			panic(err)
		}
		right.Runtime = updated
		finding.Digests = map[string]string{left.Profile: left.Runtime.SHA256, right.Profile: right.Runtime.SHA256}
	}
}

func receiptForProfile(receipts []model.RunReceipt, profile string) *model.RunReceipt {
	for index := range receipts {
		if receipts[index].Profile == profile {
			return &receipts[index]
		}
	}
	return nil
}

func probeForID(probes []model.ProbeReceipt, id string) *model.ProbeReceipt {
	for index := range probes {
		if probes[index].ID == id {
			return &probes[index]
		}
	}
	return nil
}

func completeReceipt(profile string) model.RunReceipt {
	runtime, err := runtimeidentity.Create(model.RuntimeIdentity{
		Kind: "installed", PackageName: "fixture", PackageVersion: "1.0.0", BinName: "fixture",
		ExecutablePath: "node_modules/.bin/fixture", ExecutableRealPath: "node_modules/fixture/bin.js",
		OptionalDependencies: []model.OptionalDependencyReceipt{},
	})
	if err != nil {
		panic(err)
	}
	exit := 0
	probes := []model.ProbeReceipt{
		{ID: "help", Argv: []string{"--help"}, ExitCode: &exit, StdoutSHA256: canonical.SHA256String(""), StderrSHA256: canonical.SHA256String("")},
		{ID: "version", Argv: []string{"--version"}, ExitCode: &exit, StdoutSHA256: canonical.SHA256String(""), StderrSHA256: canonical.SHA256String("")},
	}
	return model.RunReceipt{
		Subject: "installed", Profile: profile, InstallArgs: []string{},
		Environment: model.EnvironmentReceipt{Platform: "darwin", Arch: "arm64", Node: "v20.0.0", NPM: "10.0.0"},
		Runtime:     runtime,
		Probes:      probes,
	}
}

func finding(code, probe, field, locator, message string) model.Finding {
	value := model.Finding{Code: code, Probe: probe, Field: field, Profiles: []string{"default", "noOptional"}, Locator: locator, Message: message}
	value.ID = evidenceid.Finding(value)
	return value
}

func incomplete(profile, stage, probe, message string) model.IncompleteEvidence {
	value := model.IncompleteEvidence{Code: "THS-INCOMPLETE-001", Profile: profile, Stage: stage, Probe: probe, Message: message}
	value.ID = evidenceid.Incomplete(value)
	return value
}

func assertIDs(t *testing.T, items []Item, ids ...string) {
	t.Helper()
	if len(items) != len(ids) {
		t.Fatalf("items=%#v ids=%#v", items, ids)
	}
	for index := range ids {
		if items[index].ID != ids[index] {
			t.Fatalf("item[%d].id=%q want %q", index, items[index].ID, ids[index])
		}
	}
}

func itemIDs(items []Item) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func stringPointer(value string) *string { return &value }

func nonNilFindings(values []model.Finding) []model.Finding {
	if values == nil {
		return []model.Finding{}
	}
	return values
}

func nonNilIncomplete(values []model.IncompleteEvidence) []model.IncompleteEvidence {
	if values == nil {
		return []model.IncompleteEvidence{}
	}
	return values
}
