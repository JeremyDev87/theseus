package compare

import (
	"testing"

	"github.com/JeremyDev87/theseus/internal/canonical"
	"github.com/JeremyDev87/theseus/internal/model"
)

func pointer(value int) *int { return &value }

func TestReportsIndependentExitOutputRuntimeDrift(t *testing.T) {
	leftOut, rightOut := "left\n", "right\n"
	contract := model.Contract{Probes: []model.ProbeConfig{{ID: "help", Compare: []model.CompareField{model.FieldExit, model.FieldStdout, model.FieldRuntime}}}}
	receipts := []model.RunReceipt{
		{Profile: "default", Runtime: model.RuntimeIdentity{SHA256: "runtime-a", Canonical: "a"}, Probes: []model.ProbeReceipt{{ID: "help", ExitCode: pointer(0), Stdout: leftOut, StdoutSHA256: canonical.SHA256String(leftOut)}}},
		{Profile: "noOptional", Runtime: model.RuntimeIdentity{SHA256: "runtime-b", Canonical: "b"}, Probes: []model.ProbeReceipt{{ID: "help", ExitCode: pointer(1), Stdout: rightOut, StdoutSHA256: canonical.SHA256String(rightOut)}}},
	}
	result := Receipts(contract, receipts)
	want := []string{"THS-PARITY-001", "THS-PARITY-002", "THS-RUNTIME-001"}
	if len(result.Findings) != len(want) {
		t.Fatalf("findings=%#v", result.Findings)
	}
	for index, code := range want {
		if result.Findings[index].Code != code {
			t.Fatalf("finding %d=%s want %s", index, result.Findings[index].Code, code)
		}
	}
}

func TestJSONLargeExponentRemainsANumber(t *testing.T) {
	contract := model.Contract{Probes: []model.ProbeConfig{{ID: "json", Expect: &model.ProbeExpectation{StdoutJSON: []model.JSONPathExpectation{{Path: "value", Type: "number"}}}}}}
	receipts := []model.RunReceipt{{Profile: "default", Probes: []model.ProbeReceipt{{ID: "json", Stdout: `{"value":1e400}`}}}}
	result := Receipts(contract, receipts)
	if len(result.Findings) != 0 {
		t.Fatalf("unexpected findings %#v", result.Findings)
	}
}

func TestJSONPointerAndOwnProperties(t *testing.T) {
	contract := model.Contract{Probes: []model.ProbeConfig{{ID: "json", Expect: &model.ProbeExpectation{StdoutJSON: []model.JSONPathExpectation{{Path: "/meta/a.b", Type: "number"}, {Path: "/items/0/name", Type: "string"}, {Path: "/items/length", Type: "number"}, {Path: "/items/00/name", Type: "string"}, {Path: "/__proto__/polluted", Type: "string"}}}}}}
	receipts := []model.RunReceipt{{Profile: "default", Probes: []model.ProbeReceipt{{ID: "json", Stdout: `{"meta":{"a.b":1},"items":[{"name":"ok"}]}`}}}}
	result := Receipts(contract, receipts)
	if len(result.Findings) != 2 || result.Findings[0].Actual != "undefined" || result.Findings[1].Actual != "undefined" {
		t.Fatalf("unexpected findings %#v", result.Findings)
	}
}

func TestExactAllowedDeltaDoesNotSuppressUnrelatedDrift(t *testing.T) {
	leftOut, rightOut := "full\n", "fallback\n"
	leftErr, rightErr := "", "unexpected\n"
	contract := model.Contract{
		Probes: []model.ProbeConfig{{ID: "help", Compare: []model.CompareField{model.FieldStdout, model.FieldStderr}}},
		AllowedDeltas: []model.AllowedDelta{{
			Probe: "help", Between: [2]string{"default", "noOptional"}, Field: model.FieldStdout,
			FromSHA256: canonical.SHA256String(leftOut), ToSHA256: canonical.SHA256String(rightOut), Reason: "documented reduced help",
		}},
	}
	receipts := []model.RunReceipt{
		comparisonReceipt("default", 0, leftOut, leftErr),
		comparisonReceipt("noOptional", 0, rightOut, rightErr),
	}
	result := Receipts(contract, receipts)
	if len(result.AllowedDifferences) != 1 || len(result.Findings) != 1 || result.Findings[0].Field != "stderr" {
		t.Fatalf("unexpected exact-delta result %#v", result)
	}
}

func TestExitAndTypedJSONExpectationsArePerProfile(t *testing.T) {
	zero := 0
	contract := model.Contract{Probes: []model.ProbeConfig{{
		ID: "json", Compare: []model.CompareField{model.FieldExit},
		Expect: &model.ProbeExpectation{Exit: &zero, StdoutJSON: []model.JSONPathExpectation{{Path: "meta.version", Type: "string"}, {Path: "/escaped~1key/~0value", Type: "boolean"}}},
	}}}
	receipts := []model.RunReceipt{
		comparisonReceipt("default", 0, `{"meta":{"version":"1"},"escaped/key":{"~value":true}}`, ""),
		comparisonReceipt("noOptional", 2, `{"meta":{"version":1},"escaped/key":{}}`, ""),
	}
	result := Receipts(contract, receipts)
	want := []string{"THS-EXPECT-001", "THS-EXPECT-002", "THS-EXPECT-002", "THS-PARITY-001"}
	if len(result.Findings) != len(want) {
		t.Fatalf("findings=%#v", result.Findings)
	}
	for index, code := range want {
		if result.Findings[index].Code != code {
			t.Fatalf("finding %d=%s want %s", index, result.Findings[index].Code, code)
		}
	}
}

func comparisonReceipt(profile string, exit int, stdout, stderr string) model.RunReceipt {
	return model.RunReceipt{
		Profile: profile,
		Runtime: model.RuntimeIdentity{SHA256: "same-runtime", Canonical: "same-runtime"},
		Probes: []model.ProbeReceipt{{
			ID: "help", ExitCode: pointer(exit), Stdout: stdout, Stderr: stderr,
			StdoutSHA256: canonical.SHA256String(stdout), StderrSHA256: canonical.SHA256String(stderr),
		}, {
			ID: "json", ExitCode: pointer(exit), Stdout: stdout, Stderr: stderr,
			StdoutSHA256: canonical.SHA256String(stdout), StderrSHA256: canonical.SHA256String(stderr),
		}},
	}
}
