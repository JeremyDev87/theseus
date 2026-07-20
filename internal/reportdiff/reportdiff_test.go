package reportdiff

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/JeremyDev87/theseus/internal/evidenceid"
	"github.com/JeremyDev87/theseus/internal/model"
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
		StdoutSHA256: "stdout-sha", StderrSHA256: "stderr-sha",
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
				StdoutSHA256: "stdout-sha", StderrSHA256: "stderr-sha",
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
		Artifact: model.ArtifactReceipt{PackageName: "fixture", PackageVersion: "1.0.0", Filename: "fixture.tgz", SHA256: "fixture-sha", Size: 1},
		Receipts: []model.RunReceipt{completeReceipt("default")}, Incomplete: incompleteEvidence,
		Comparison: model.ComparisonResult{Findings: findings, AllowedDifferences: []model.AllowedDifference{}},
	}
}

func completeReceipt(profile string) model.RunReceipt {
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
