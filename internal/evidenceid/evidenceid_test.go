package evidenceid

import (
	"testing"

	"github.com/JeremyDev87/theseus/internal/model"
)

func TestEvidenceIdentityIsStableAcrossVolatileFields(t *testing.T) {
	finding := model.Finding{
		Code: "THS-PARITY-002", Probe: "help", Field: "stdout",
		Profiles: []string{"noOptional", "default"}, Locator: "compare:stdout",
		Message: "first wording", Expected: "old", Actual: "new",
		Digests: map[string]string{"default": "left", "noOptional": "right"},
	}
	const wantFinding = "ths:v1:9ef3688658007f1378afd2b0a62795c73b9b2710199ccfe9aba1cffafe59de5f"
	if got := Finding(finding); got != wantFinding {
		t.Fatalf("finding id=%q want %q", got, wantFinding)
	}

	changedPresentation := finding
	changedPresentation.Message = "rewritten"
	changedPresentation.Expected = map[string]any{"value": 1}
	changedPresentation.Actual = map[string]any{"value": 2}
	changedPresentation.Digests = map[string]string{"default": "new-left", "noOptional": "new-right"}
	if got := Finding(changedPresentation); got != wantFinding {
		t.Fatalf("presentation fields changed identity: %q", got)
	}

	changedLocator := finding
	changedLocator.Locator = "compare:stderr"
	if got := Finding(changedLocator); got == wantFinding {
		t.Fatalf("semantic locator must change identity: %q", got)
	}

	incomplete := model.IncompleteEvidence{
		Code: "THS-INCOMPLETE-001", Profile: "noOptional", Stage: "install", Probe: "help",
		Message: "first failure", Stderr: "volatile stderr",
	}
	const wantIncomplete = "ths:v1:d4da2904258502dedac4104ec8a96ce6bde6987cd1dfcee7ba1e06b35c3f57d2"
	if got := Incomplete(incomplete); got != wantIncomplete {
		t.Fatalf("incomplete id=%q want %q", got, wantIncomplete)
	}
	incomplete.Message = "rewritten failure"
	incomplete.Stderr = "different stderr"
	if got := Incomplete(incomplete); got != wantIncomplete {
		t.Fatalf("volatile incomplete fields changed identity: %q", got)
	}
}

func TestEvidenceIdentityHasNoDelimiterBoundaryCollisions(t *testing.T) {
	left := model.Finding{Code: "A\x00B", Probe: "C", Field: "exit", Profiles: []string{"default"}, Locator: "compare:exit"}
	right := model.Finding{Code: "A", Probe: "B\x00C", Field: "exit", Profiles: []string{"default"}, Locator: "compare:exit"}
	if Finding(left) == Finding(right) {
		t.Fatal("canonical identity payload must distinguish embedded delimiters")
	}
}
