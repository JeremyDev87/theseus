package reportdiff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/JeremyDev87/theseus/internal/evidenceid"
	"github.com/JeremyDev87/theseus/internal/model"
	"github.com/JeremyDev87/theseus/internal/version"
)

const SchemaVersion = 2

type Evidence struct {
	Kind       string                    `json:"kind"`
	Finding    *model.Finding            `json:"finding,omitempty"`
	Incomplete *model.IncompleteEvidence `json:"incomplete,omitempty"`
}

type Item struct {
	ID     string    `json:"id"`
	Kind   string    `json:"kind"`
	Before *Evidence `json:"before,omitempty"`
	After  *Evidence `json:"after,omitempty"`
}

type ReportRef struct {
	Target         string `json:"target"`
	Status         string `json:"status"`
	ExitCode       int    `json:"exitCode"`
	ArtifactSHA256 string `json:"artifactSha256,omitempty"`
}

type Result struct {
	SchemaVersion   int               `json:"schemaVersion"`
	IdentityVersion int               `json:"identityVersion"`
	Tool            model.ToolReceipt `json:"tool"`
	Status          string            `json:"status"`
	ExitCode        int               `json:"exitCode"`
	Before          ReportRef         `json:"before"`
	After           ReportRef         `json:"after"`
	Introduced      []Item            `json:"introduced"`
	Resolved        []Item            `json:"resolved"`
	Persisted       []Item            `json:"persisted"`
}

func Parse(data []byte) (model.VerificationReport, error) {
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return model.VerificationReport{}, fmt.Errorf("invalid report JSON: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	var value model.VerificationReport
	if err := decoder.Decode(&value); err != nil {
		return model.VerificationReport{}, fmt.Errorf("invalid report JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return model.VerificationReport{}, fmt.Errorf("invalid report JSON: %w", err)
	}
	if err := validate(value); err != nil {
		return model.VerificationReport{}, err
	}
	return value, nil
}

func Compare(before, after model.VerificationReport) (Result, error) {
	if err := validate(before); err != nil {
		return Result{}, fmt.Errorf("before report: %w", err)
	}
	if err := validate(after); err != nil {
		return Result{}, fmt.Errorf("after report: %w", err)
	}

	beforeEvidence := evidenceMap(before)
	afterEvidence := evidenceMap(after)
	result := Result{
		SchemaVersion: SchemaVersion, IdentityVersion: evidenceid.Version,
		Tool:   model.ToolReceipt{Name: "theseus", Version: version.Version},
		Status: "clean", ExitCode: 0,
		Before: reportRef(before), After: reportRef(after),
		Introduced: []Item{}, Resolved: []Item{}, Persisted: []Item{},
	}

	for id, current := range afterEvidence {
		if previous, ok := beforeEvidence[id]; ok {
			result.Persisted = append(result.Persisted, Item{ID: id, Kind: current.Kind, Before: evidencePointer(previous), After: evidencePointer(current)})
		} else {
			result.Introduced = append(result.Introduced, Item{ID: id, Kind: current.Kind, After: evidencePointer(current)})
		}
	}
	for id, previous := range beforeEvidence {
		if _, ok := afterEvidence[id]; !ok {
			result.Resolved = append(result.Resolved, Item{ID: id, Kind: previous.Kind, Before: evidencePointer(previous)})
		}
	}
	sortItems(result.Introduced)
	sortItems(result.Resolved)
	sortItems(result.Persisted)
	if len(result.Introduced) > 0 {
		result.Status = "introduced"
		result.ExitCode = 1
	}
	return result, nil
}

func Text(result Result) string {
	lines := []string{
		fmt.Sprintf("Theseus report diff %s", result.Tool.Version),
		fmt.Sprintf("before    %s", displayRef(result.Before)),
		fmt.Sprintf("after     %s", displayRef(result.After)),
		fmt.Sprintf("status    %s (exit %d)", strings.ToUpper(result.Status), result.ExitCode),
		fmt.Sprintf("summary   introduced=%d resolved=%d persisted=%d", len(result.Introduced), len(result.Resolved), len(result.Persisted)),
	}
	for _, group := range []struct {
		prefix string
		items  []Item
	}{
		{prefix: "+", items: result.Introduced},
		{prefix: "-", items: result.Resolved},
		{prefix: "=", items: result.Persisted},
	} {
		for _, item := range group.items {
			evidence := item.After
			if evidence == nil {
				evidence = item.Before
			}
			lines = append(lines, fmt.Sprintf("%s %s %s %s", group.prefix, item.Kind, item.ID, evidenceSummary(evidence)))
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func validate(report model.VerificationReport) error {
	if report.SchemaVersion != SchemaVersion {
		return fmt.Errorf("report requires schemaVersion 2, got %d", report.SchemaVersion)
	}
	if report.IdentityVersion != evidenceid.Version {
		return fmt.Errorf("report requires identityVersion 1, got %d", report.IdentityVersion)
	}
	if report.Tool.Name != "theseus" || strings.TrimSpace(report.Tool.Version) == "" {
		return fmt.Errorf("report tool must be theseus with a non-empty version")
	}
	if strings.TrimSpace(report.Target) == "" {
		return fmt.Errorf("report target must not be empty")
	}
	if strings.TrimSpace(report.Artifact.PackageName) == "" || strings.TrimSpace(report.Artifact.PackageVersion) == "" || strings.TrimSpace(report.Artifact.Filename) == "" || strings.TrimSpace(report.Artifact.SHA256) == "" || report.Artifact.Size <= 0 {
		return fmt.Errorf("report artifact receipt is incomplete")
	}
	if !validStatusExit(report.Status, report.ExitCode) {
		return fmt.Errorf("report status %q and exitCode %d are inconsistent", report.Status, report.ExitCode)
	}
	if report.Receipts == nil || report.Incomplete == nil || report.Comparison.Findings == nil || report.Comparison.AllowedDifferences == nil {
		return fmt.Errorf("report evidence arrays must be present")
	}
	if len(report.Receipts) == 0 && len(report.Incomplete) == 0 {
		return fmt.Errorf("report requires at least one run receipt or incomplete evidence entry")
	}
	seenProfiles := map[string]bool{}
	for index := range report.Receipts {
		receipt := report.Receipts[index]
		profile := strings.TrimSpace(receipt.Profile)
		if err := validateReceipt(receipt, index, report.Incomplete); err != nil {
			return err
		}
		if seenProfiles[profile] {
			return fmt.Errorf("duplicate receipt profile %s", profile)
		}
		seenProfiles[profile] = true
	}
	wantStatus, wantExit := "pass", 0
	if len(report.Incomplete) > 0 {
		wantStatus, wantExit = "incomplete", 2
	} else if len(report.Comparison.Findings) > 0 {
		wantStatus, wantExit = "drift", 1
	}
	if report.Status != wantStatus || report.ExitCode != wantExit {
		return fmt.Errorf("report evidence requires status %q and exitCode %d, got status %q and exitCode %d", wantStatus, wantExit, report.Status, report.ExitCode)
	}

	seen := map[string]bool{}
	for index := range report.Comparison.Findings {
		finding := report.Comparison.Findings[index]
		if strings.TrimSpace(finding.ID) == "" || strings.TrimSpace(finding.Code) == "" || strings.TrimSpace(finding.Probe) == "" || strings.TrimSpace(finding.Field) == "" || len(finding.Profiles) == 0 || strings.TrimSpace(finding.Locator) == "" || strings.TrimSpace(finding.Message) == "" {
			return fmt.Errorf("finding[%d] has incomplete identity fields", index)
		}
		if finding.ID != evidenceid.Finding(finding) {
			return fmt.Errorf("finding[%d] identity mismatch", index)
		}
		if seen[finding.ID] {
			return fmt.Errorf("duplicate evidence id %s", finding.ID)
		}
		seen[finding.ID] = true
	}
	for index := range report.Incomplete {
		incomplete := report.Incomplete[index]
		if strings.TrimSpace(incomplete.ID) == "" || strings.TrimSpace(incomplete.Code) == "" || strings.TrimSpace(incomplete.Profile) == "" || strings.TrimSpace(incomplete.Stage) == "" || strings.TrimSpace(incomplete.Message) == "" {
			return fmt.Errorf("incomplete[%d] has incomplete identity fields", index)
		}
		if incomplete.ID != evidenceid.Incomplete(incomplete) {
			return fmt.Errorf("incomplete[%d] identity mismatch", index)
		}
		if seen[incomplete.ID] {
			return fmt.Errorf("duplicate evidence id %s", incomplete.ID)
		}
		seen[incomplete.ID] = true
	}
	return nil
}

func validateReceipt(receipt model.RunReceipt, index int, incomplete []model.IncompleteEvidence) error {
	prefix := fmt.Sprintf("receipt[%d]", index)
	profile := strings.TrimSpace(receipt.Profile)
	if strings.TrimSpace(receipt.Subject) == "" || profile == "" {
		return fmt.Errorf("%s subject and profile must not be empty", prefix)
	}
	if receipt.InstallArgs == nil || receipt.Probes == nil {
		return fmt.Errorf("%s installArgs and probes arrays must be present", prefix)
	}
	if strings.TrimSpace(receipt.Environment.Platform) == "" || strings.TrimSpace(receipt.Environment.Arch) == "" || strings.TrimSpace(receipt.Environment.Node) == "" || strings.TrimSpace(receipt.Environment.NPM) == "" {
		return fmt.Errorf("%s environment receipt is incomplete", prefix)
	}
	runtime := receipt.Runtime
	if runtime.Kind != "installed" && runtime.Kind != "source" {
		return fmt.Errorf("%s runtime kind must be installed or source", prefix)
	}
	if runtime.Kind == "installed" && (strings.TrimSpace(runtime.PackageName) == "" || strings.TrimSpace(runtime.PackageVersion) == "") {
		return fmt.Errorf("%s installed runtime package identity is incomplete", prefix)
	}
	if strings.TrimSpace(runtime.BinName) == "" || strings.TrimSpace(runtime.ExecutablePath) == "" || strings.TrimSpace(runtime.ExecutableRealPath) == "" || strings.TrimSpace(runtime.Canonical) == "" || strings.TrimSpace(runtime.SHA256) == "" || runtime.OptionalDependencies == nil {
		return fmt.Errorf("%s runtime receipt is incomplete", prefix)
	}
	for dependencyIndex, dependency := range runtime.OptionalDependencies {
		if strings.TrimSpace(dependency.Name) == "" || strings.TrimSpace(dependency.Version) == "" {
			return fmt.Errorf("%s runtime optionalDependencies[%d] is incomplete", prefix, dependencyIndex)
		}
	}
	seenProbes := map[string]bool{}
	for probeIndex, probe := range receipt.Probes {
		if strings.TrimSpace(probe.ID) == "" || probe.Argv == nil || strings.TrimSpace(probe.StdoutSHA256) == "" || strings.TrimSpace(probe.StderrSHA256) == "" || probe.DurationMS < 0 {
			return fmt.Errorf("%s probes[%d] is incomplete", prefix, probeIndex)
		}
		if seenProbes[probe.ID] {
			return fmt.Errorf("%s contains duplicate probe id %s", prefix, probe.ID)
		}
		if probe.ExitCode != nil && probe.Signal != nil {
			return fmt.Errorf("%s probes[%d] cannot contain both exitCode and signal", prefix, probeIndex)
		}
		matchedIncomplete := hasProbeIncomplete(incomplete, profile, probe.ID)
		if probe.TimedOut && !matchedIncomplete {
			return fmt.Errorf("%s probes[%d] timed out probe requires matching incomplete evidence", prefix, probeIndex)
		}
		if probe.ExitCode == nil && probe.Signal == nil && !probe.TimedOut && !matchedIncomplete {
			return fmt.Errorf("%s probes[%d] probe without a process outcome requires matching incomplete evidence", prefix, probeIndex)
		}
		seenProbes[probe.ID] = true
	}
	return nil
}

func hasProbeIncomplete(incomplete []model.IncompleteEvidence, profile, probe string) bool {
	for _, evidence := range incomplete {
		if strings.TrimSpace(evidence.Profile) == profile && strings.TrimSpace(evidence.Stage) == "probe" && evidence.Probe == probe {
			return true
		}
	}
	return false
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return scanJSONValue(decoder)
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key must be a string")
			}
			if seen[key] {
				return fmt.Errorf("duplicate JSON object key %q", key)
			}
			seen[key] = true
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
}

func validStatusExit(status string, exitCode int) bool {
	return (status == "pass" && exitCode == 0) || (status == "drift" && exitCode == 1) || (status == "incomplete" && exitCode == 2)
}

func evidenceMap(report model.VerificationReport) map[string]Evidence {
	result := make(map[string]Evidence, len(report.Comparison.Findings)+len(report.Incomplete))
	for index := range report.Comparison.Findings {
		finding := report.Comparison.Findings[index]
		result[finding.ID] = Evidence{Kind: "finding", Finding: &finding}
	}
	for index := range report.Incomplete {
		incomplete := report.Incomplete[index]
		result[incomplete.ID] = Evidence{Kind: "incomplete", Incomplete: &incomplete}
	}
	return result
}

func evidencePointer(value Evidence) *Evidence {
	copy := value
	return &copy
}

func sortItems(items []Item) {
	sort.Slice(items, func(left, right int) bool {
		if items[left].ID == items[right].ID {
			return items[left].Kind < items[right].Kind
		}
		return items[left].ID < items[right].ID
	})
}

func reportRef(report model.VerificationReport) ReportRef {
	return ReportRef{Target: report.Target, Status: report.Status, ExitCode: report.ExitCode, ArtifactSHA256: report.Artifact.SHA256}
}

func displayRef(ref ReportRef) string {
	if ref.ArtifactSHA256 == "" {
		return ref.Target
	}
	return ref.Target + " sha256=" + ref.ArtifactSHA256
}

func evidenceSummary(evidence *Evidence) string {
	if evidence == nil {
		return ""
	}
	if evidence.Finding != nil {
		return evidence.Finding.Code + " " + evidence.Finding.Message
	}
	if evidence.Incomplete != nil {
		return evidence.Incomplete.Code + " " + evidence.Incomplete.Message
	}
	return ""
}
