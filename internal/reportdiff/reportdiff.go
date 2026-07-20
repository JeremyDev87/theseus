package reportdiff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/JeremyDev87/theseus/internal/canonical"
	"github.com/JeremyDev87/theseus/internal/evidenceid"
	"github.com/JeremyDev87/theseus/internal/model"
	"github.com/JeremyDev87/theseus/internal/runtimeidentity"
	"github.com/JeremyDev87/theseus/internal/version"
)

const SchemaVersion = 2

var (
	sha256Pattern  = regexp.MustCompile(`^[a-f0-9]{64}$`)
	profilePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
)

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
	if err := validateWireShapes(data); err != nil {
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
	if !sha256Pattern.MatchString(report.Artifact.SHA256) {
		return fmt.Errorf("report artifact sha256 must be lowercase 64-hex")
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

	receipts := map[string]model.RunReceipt{}
	for index := range report.Receipts {
		receipt := report.Receipts[index]
		if err := validateReceipt(receipt, index, report.Artifact, report.Incomplete); err != nil {
			return err
		}
		if _, exists := receipts[receipt.Profile]; exists {
			return fmt.Errorf("duplicate receipt profile %s", receipt.Profile)
		}
		receipts[receipt.Profile] = receipt
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

	seenEvidence := map[string]bool{}
	findingReferences := map[string]bool{}
	for index := range report.Comparison.Findings {
		finding := report.Comparison.Findings[index]
		if strings.TrimSpace(finding.ID) == "" || strings.TrimSpace(finding.Code) == "" || strings.TrimSpace(finding.Probe) == "" || strings.TrimSpace(finding.Field) == "" || len(finding.Profiles) == 0 || strings.TrimSpace(finding.Locator) == "" || strings.TrimSpace(finding.Message) == "" {
			return fmt.Errorf("finding[%d] has incomplete identity fields", index)
		}
		if finding.ID != evidenceid.Finding(finding) {
			return fmt.Errorf("finding[%d] identity mismatch", index)
		}
		if seenEvidence[finding.ID] {
			return fmt.Errorf("duplicate evidence id %s", finding.ID)
		}
		if err := validateFinding(finding, index, receipts); err != nil {
			return err
		}
		if len(finding.Profiles) == 2 && validCompareField(model.CompareField(finding.Field)) {
			findingReferences[comparisonReferenceKey(finding.Probe, model.CompareField(finding.Field), finding.Profiles[0], finding.Profiles[1])] = true
		}
		seenEvidence[finding.ID] = true
	}
	for index := range report.Incomplete {
		incomplete := report.Incomplete[index]
		if strings.TrimSpace(incomplete.ID) == "" || strings.TrimSpace(incomplete.Code) == "" || strings.TrimSpace(incomplete.Profile) == "" || strings.TrimSpace(incomplete.Stage) == "" || strings.TrimSpace(incomplete.Message) == "" {
			return fmt.Errorf("incomplete[%d] has incomplete identity fields", index)
		}
		if incomplete.Profile != strings.TrimSpace(incomplete.Profile) || !profilePattern.MatchString(incomplete.Profile) {
			return fmt.Errorf("incomplete[%d] profile must be canonical", index)
		}
		if incomplete.ID != evidenceid.Incomplete(incomplete) {
			return fmt.Errorf("incomplete[%d] identity mismatch", index)
		}
		if seenEvidence[incomplete.ID] {
			return fmt.Errorf("duplicate evidence id %s", incomplete.ID)
		}
		seenEvidence[incomplete.ID] = true
	}
	seenAllowed := map[string]bool{}
	for index, allowed := range report.Comparison.AllowedDifferences {
		key, err := validateAllowedDifference(allowed, index, receipts)
		if err != nil {
			return err
		}
		if seenAllowed[key] {
			return fmt.Errorf("duplicate allowed difference %s", key)
		}
		if findingReferences[key] {
			return fmt.Errorf("allowed difference conflicts with finding %s", key)
		}
		seenAllowed[key] = true
	}
	return nil
}

func validateReceipt(receipt model.RunReceipt, index int, artifact model.ArtifactReceipt, incomplete []model.IncompleteEvidence) error {
	prefix := fmt.Sprintf("receipt[%d]", index)
	profile := receipt.Profile
	if strings.TrimSpace(receipt.Subject) == "" || strings.TrimSpace(profile) == "" {
		return fmt.Errorf("%s subject and profile must not be empty", prefix)
	}
	if profile != strings.TrimSpace(profile) || !profilePattern.MatchString(profile) {
		return fmt.Errorf("%s profile must be canonical", prefix)
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
	if receipt.Subject != runtime.Kind {
		return fmt.Errorf("%s subject and runtime kind must match", prefix)
	}
	if runtime.Kind == "source" {
		if profile != "source" || runtime.PackageName != "" || runtime.PackageVersion != "" {
			return fmt.Errorf("%s source runtime identity is inconsistent", prefix)
		}
	} else {
		if strings.TrimSpace(runtime.PackageName) == "" || strings.TrimSpace(runtime.PackageVersion) == "" {
			return fmt.Errorf("%s installed runtime package identity is incomplete", prefix)
		}
		if runtime.PackageName != artifact.PackageName || runtime.PackageVersion != artifact.PackageVersion {
			return fmt.Errorf("%s runtime package must match artifact", prefix)
		}
	}
	if strings.TrimSpace(runtime.BinName) == "" || strings.TrimSpace(runtime.ExecutablePath) == "" || strings.TrimSpace(runtime.ExecutableRealPath) == "" || strings.TrimSpace(runtime.Canonical) == "" || strings.TrimSpace(runtime.SHA256) == "" || runtime.OptionalDependencies == nil {
		return fmt.Errorf("%s runtime receipt is incomplete", prefix)
	}
	previousDependency := ""
	for dependencyIndex, dependency := range runtime.OptionalDependencies {
		if strings.TrimSpace(dependency.Name) == "" || strings.TrimSpace(dependency.Version) == "" {
			return fmt.Errorf("%s runtime optionalDependencies[%d] is incomplete", prefix, dependencyIndex)
		}
		if dependency.Name != strings.TrimSpace(dependency.Name) || dependency.Version != strings.TrimSpace(dependency.Version) {
			return fmt.Errorf("%s runtime optionalDependencies[%d] must be canonical", prefix, dependencyIndex)
		}
		if dependencyIndex > 0 && dependency.Name == previousDependency {
			return fmt.Errorf("%s contains duplicate optional dependency %s", prefix, dependency.Name)
		}
		if dependencyIndex > 0 && dependency.Name < previousDependency {
			return fmt.Errorf("%s runtime optionalDependencies must be name-sorted", prefix)
		}
		previousDependency = dependency.Name
	}
	recomputed, err := runtimeidentity.Create(runtime)
	if err != nil {
		return fmt.Errorf("%s runtime identity: %w", prefix, err)
	}
	if runtime.Canonical != recomputed.Canonical {
		return fmt.Errorf("%s runtime canonical payload mismatch", prefix)
	}
	if !sha256Pattern.MatchString(runtime.SHA256) || runtime.SHA256 != recomputed.SHA256 {
		return fmt.Errorf("%s runtime sha256 mismatch", prefix)
	}

	seenProbes := map[string]bool{}
	for probeIndex, probe := range receipt.Probes {
		if strings.TrimSpace(probe.ID) == "" || probe.Argv == nil || strings.TrimSpace(probe.StdoutSHA256) == "" || strings.TrimSpace(probe.StderrSHA256) == "" || probe.DurationMS < 0 {
			return fmt.Errorf("%s probes[%d] is incomplete", prefix, probeIndex)
		}
		if seenProbes[probe.ID] {
			return fmt.Errorf("%s contains duplicate probe id %s", prefix, probe.ID)
		}
		if !sha256Pattern.MatchString(probe.StdoutSHA256) || probe.StdoutSHA256 != canonical.SHA256String(probe.Stdout) {
			return fmt.Errorf("%s probes[%d] stdoutSha256 mismatch", prefix, probeIndex)
		}
		if !sha256Pattern.MatchString(probe.StderrSHA256) || probe.StderrSHA256 != canonical.SHA256String(probe.Stderr) {
			return fmt.Errorf("%s probes[%d] stderrSha256 mismatch", prefix, probeIndex)
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

func validateFinding(finding model.Finding, index int, receipts map[string]model.RunReceipt) error {
	if !validFindingField(finding.Field) || len(finding.Profiles) > 2 {
		return fmt.Errorf("finding[%d] has unsupported field or profile count", index)
	}
	seenProfiles := map[string]bool{}
	for _, profile := range finding.Profiles {
		if profile != strings.TrimSpace(profile) || !profilePattern.MatchString(profile) {
			return fmt.Errorf("finding[%d] profile must be canonical", index)
		}
		if seenProfiles[profile] {
			return fmt.Errorf("finding[%d] contains duplicate profile %s", index, profile)
		}
		receipt, exists := receipts[profile]
		if !exists {
			return fmt.Errorf("finding[%d] references unknown profile %s", index, profile)
		}
		if probeByID(receipt, finding.Probe) == nil {
			return fmt.Errorf("finding[%d] references unknown probe %s for profile %s", index, finding.Probe, profile)
		}
		seenProfiles[profile] = true
	}
	needsDigests := len(finding.Profiles) == 2 && (finding.Field == string(model.FieldStdout) || finding.Field == string(model.FieldStderr) || finding.Field == string(model.FieldRuntime))
	if needsDigests && len(finding.Digests) != len(finding.Profiles) {
		return fmt.Errorf("finding[%d] digest profiles do not match finding profiles", index)
	}
	for profile, digest := range finding.Digests {
		if !seenProfiles[profile] {
			return fmt.Errorf("finding[%d] digest references unknown profile %s", index, profile)
		}
		want, ok := receiptDigest(receipts[profile], finding.Probe, model.CompareField(finding.Field))
		if !ok || !sha256Pattern.MatchString(digest) || digest != want {
			return fmt.Errorf("finding[%d] finding digest does not match receipt for profile %s", index, profile)
		}
	}
	if err := validateFindingPayload(finding, index, receipts); err != nil {
		return err
	}
	if len(finding.Profiles) == 2 && validCompareField(model.CompareField(finding.Field)) {
		left, _ := receiptValue(receipts[finding.Profiles[0]], finding.Probe, model.CompareField(finding.Field))
		right, _ := receiptValue(receipts[finding.Profiles[1]], finding.Probe, model.CompareField(finding.Field))
		if left == right {
			return fmt.Errorf("finding[%d] does not describe an observed difference", index)
		}
	}
	return nil
}

func validateFindingPayload(finding model.Finding, index int, receipts map[string]model.RunReceipt) error {
	if len(finding.Profiles) == 2 && validCompareField(model.CompareField(finding.Field)) {
		wantCode := "THS-PARITY-002"
		if finding.Field == string(model.FieldExit) {
			wantCode = "THS-PARITY-001"
		} else if finding.Field == string(model.FieldRuntime) {
			wantCode = "THS-RUNTIME-001"
		}
		if finding.Code != wantCode || finding.Locator != "compare:"+finding.Field {
			return fmt.Errorf("finding[%d] compare finding code or locator is inconsistent", index)
		}
		if finding.Expected != nil {
			return fmt.Errorf("finding[%d] compare finding must not contain expected payload", index)
		}
		wantActual := map[string]any{}
		for _, profile := range finding.Profiles {
			value, ok := receiptActualValue(receipts[profile], finding.Probe, model.CompareField(finding.Field))
			if !ok {
				return fmt.Errorf("finding[%d] actual payload cannot be derived from receipts", index)
			}
			wantActual[profile] = value
		}
		if !canonicalValuesEqual(finding.Actual, wantActual) {
			return fmt.Errorf("finding[%d] actual payload does not match receipts", index)
		}
		return nil
	}

	if finding.Code == "THS-EXPECT-001" {
		if len(finding.Profiles) != 1 || finding.Field != string(model.FieldExit) || finding.Locator != "expect:exit" || len(finding.Digests) != 0 {
			return fmt.Errorf("finding[%d] exit expectation shape is inconsistent", index)
		}
		wantActual, ok := receiptActualValue(receipts[finding.Profiles[0]], finding.Probe, model.FieldExit)
		if !ok || !canonicalValuesEqual(finding.Actual, wantActual) {
			return fmt.Errorf("finding[%d] actual payload does not match receipts", index)
		}
		wantExit, ok := integerJSONValue(finding.Expected)
		if !ok {
			return fmt.Errorf("finding[%d] exit expectation must contain an integer expected payload", index)
		}
		if actualExit, ok := integerJSONValue(wantActual); ok && actualExit == wantExit {
			return fmt.Errorf("finding[%d] exit expectation does not describe a mismatch", index)
		}
		return nil
	}

	if finding.Code == "THS-EXPECT-002" {
		return validateStdoutJSONFinding(finding, index, receipts)
	}
	return fmt.Errorf("finding[%d] has unsupported code, field, or profile count", index)
}

func receiptActualValue(receipt model.RunReceipt, probe string, field model.CompareField) (any, bool) {
	value := probeByID(receipt, probe)
	if value == nil {
		return nil, false
	}
	switch field {
	case model.FieldExit:
		if value.ExitCode == nil {
			return nil, true
		}
		return *value.ExitCode, true
	case model.FieldStdout:
		return value.Stdout, true
	case model.FieldStderr:
		return value.Stderr, true
	case model.FieldRuntime:
		return receipt.Runtime.Canonical, true
	default:
		return nil, false
	}
}

func canonicalValuesEqual(left, right any) bool {
	leftJSON, leftErr := canonical.Marshal(left)
	rightJSON, rightErr := canonical.Marshal(right)
	return leftErr == nil && rightErr == nil && leftJSON == rightJSON
}

func integerJSONValue(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case json.Number:
		parsed, err := strconv.ParseInt(string(typed), 10, 64)
		return parsed, err == nil
	case float64:
		parsed := int64(typed)
		return parsed, float64(parsed) == typed
	default:
		return 0, false
	}
}

func validateStdoutJSONFinding(finding model.Finding, index int, receipts map[string]model.RunReceipt) error {
	if len(finding.Profiles) != 1 || finding.Field != "stdoutJson" || len(finding.Digests) != 0 {
		return fmt.Errorf("finding[%d] stdout JSON expectation shape is inconsistent", index)
	}
	probe := probeByID(receipts[finding.Profiles[0]], finding.Probe)
	if probe == nil {
		return fmt.Errorf("finding[%d] stdout JSON probe is missing", index)
	}
	const prefix = "expect:stdoutJson:"
	if !strings.HasPrefix(finding.Locator, prefix) {
		return fmt.Errorf("finding[%d] stdout JSON locator is inconsistent", index)
	}
	detail := strings.TrimPrefix(finding.Locator, prefix)
	if detail == "document" {
		if json.Valid([]byte(probe.Stdout)) || finding.Expected != nil || finding.Actual != nil {
			return fmt.Errorf("finding[%d] invalid JSON finding does not match receipt", index)
		}
		return nil
	}
	separator := strings.LastIndex(detail, ":")
	if separator <= 0 || separator == len(detail)-1 || !json.Valid([]byte(probe.Stdout)) {
		return fmt.Errorf("finding[%d] stdout JSON locator is inconsistent", index)
	}
	path, wantType := detail[:separator], detail[separator+1:]
	if !validJSONType(wantType) || finding.Expected != wantType {
		return fmt.Errorf("finding[%d] stdout JSON expected payload is inconsistent", index)
	}
	decoder := json.NewDecoder(strings.NewReader(probe.Stdout))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return fmt.Errorf("finding[%d] stdout JSON receipt cannot be decoded", index)
	}
	value, found := findingJSONPath(document, path)
	actualType := findingJSONType(value, found)
	if finding.Actual != actualType {
		return fmt.Errorf("finding[%d] actual payload does not match receipts", index)
	}
	if actualType == wantType {
		return fmt.Errorf("finding[%d] stdout JSON expectation does not describe a mismatch", index)
	}
	return nil
}

func validJSONType(value string) bool {
	switch value {
	case "undefined", "null", "boolean", "number", "string", "array", "object":
		return true
	default:
		return false
	}
}

func findingJSONPath(value any, path string) (any, bool) {
	segments := strings.Split(path, ".")
	if strings.HasPrefix(path, "/") {
		var ok bool
		segments, ok = findingJSONPointer(path)
		if !ok {
			return nil, false
		}
	}
	current := value
	for _, segment := range segments {
		switch typed := current.(type) {
		case map[string]any:
			var ok bool
			current, ok = typed[segment]
			if !ok {
				return nil, false
			}
		case []any:
			if segment == "length" {
				current = json.Number(strconv.Itoa(len(typed)))
				continue
			}
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= len(typed) || (len(segment) > 1 && segment[0] == '0') {
				return nil, false
			}
			current = typed[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func findingJSONPointer(path string) ([]string, bool) {
	tokens := strings.Split(path[1:], "/")
	for index, token := range tokens {
		var builder strings.Builder
		for offset := 0; offset < len(token); offset++ {
			if token[offset] != '~' {
				builder.WriteByte(token[offset])
				continue
			}
			if offset+1 >= len(token) || (token[offset+1] != '0' && token[offset+1] != '1') {
				return nil, false
			}
			offset++
			if token[offset] == '0' {
				builder.WriteByte('~')
			} else {
				builder.WriteByte('/')
			}
		}
		tokens[index] = builder.String()
	}
	return tokens, true
}

func findingJSONType(value any, found bool) string {
	if !found {
		return "undefined"
	}
	if value == nil {
		return "null"
	}
	switch value.(type) {
	case bool:
		return "boolean"
	case float64, json.Number:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "undefined"
	}
}

func validateAllowedDifference(allowed model.AllowedDifference, index int, receipts map[string]model.RunReceipt) (string, error) {
	if !validCompareField(allowed.Field) {
		return "", fmt.Errorf("allowedDifferences[%d] has invalid probe or field", index)
	}
	if strings.TrimSpace(allowed.Reason) == "" {
		return "", fmt.Errorf("allowedDifferences[%d] reason must be non-empty", index)
	}
	leftProfile, rightProfile := allowed.Profiles[0], allowed.Profiles[1]
	if leftProfile == rightProfile {
		return "", fmt.Errorf("allowedDifferences[%d] profiles must be distinct", index)
	}
	for _, profile := range allowed.Profiles {
		if profile != strings.TrimSpace(profile) || !profilePattern.MatchString(profile) {
			return "", fmt.Errorf("allowedDifferences[%d] profile must be canonical", index)
		}
		receipt, exists := receipts[profile]
		if !exists {
			return "", fmt.Errorf("allowedDifferences[%d] references unknown profile %s", index, profile)
		}
		if probeByID(receipt, allowed.Probe) == nil {
			return "", fmt.Errorf("allowedDifferences[%d] references unknown probe %s for profile %s", index, allowed.Probe, profile)
		}
	}
	left, _ := receiptValue(receipts[leftProfile], allowed.Probe, allowed.Field)
	right, _ := receiptValue(receipts[rightProfile], allowed.Probe, allowed.Field)
	if left == right {
		return "", fmt.Errorf("allowedDifferences[%d] does not describe an observed difference", index)
	}
	return comparisonReferenceKey(allowed.Probe, allowed.Field, leftProfile, rightProfile), nil
}

func comparisonReferenceKey(probe string, field model.CompareField, leftProfile, rightProfile string) string {
	profiles := []string{leftProfile, rightProfile}
	sort.Strings(profiles)
	return probe + "\x00" + string(field) + "\x00" + profiles[0] + "\x00" + profiles[1]
}

func validFindingField(field string) bool {
	return validCompareField(model.CompareField(field)) || field == "stdoutJson"
}

func validCompareField(field model.CompareField) bool {
	return field == model.FieldExit || field == model.FieldStdout || field == model.FieldStderr || field == model.FieldRuntime
}

func probeByID(receipt model.RunReceipt, id string) *model.ProbeReceipt {
	for index := range receipt.Probes {
		if receipt.Probes[index].ID == id {
			return &receipt.Probes[index]
		}
	}
	return nil
}

func receiptDigest(receipt model.RunReceipt, probe string, field model.CompareField) (string, bool) {
	switch field {
	case model.FieldRuntime:
		return receipt.Runtime.SHA256, true
	case model.FieldStdout:
		if value := probeByID(receipt, probe); value != nil {
			return value.StdoutSHA256, true
		}
	case model.FieldStderr:
		if value := probeByID(receipt, probe); value != nil {
			return value.StderrSHA256, true
		}
	}
	return "", false
}

func receiptValue(receipt model.RunReceipt, probe string, field model.CompareField) (string, bool) {
	value := probeByID(receipt, probe)
	if value == nil {
		return "", false
	}
	switch field {
	case model.FieldExit:
		if value.ExitCode == nil {
			return "null", true
		}
		return fmt.Sprintf("%d", *value.ExitCode), true
	case model.FieldStdout:
		return value.StdoutSHA256, true
	case model.FieldStderr:
		return value.StderrSHA256, true
	case model.FieldRuntime:
		return receipt.Runtime.SHA256, true
	default:
		return "", false
	}
}

func hasProbeIncomplete(incomplete []model.IncompleteEvidence, profile, probe string) bool {
	for _, evidence := range incomplete {
		if strings.TrimSpace(evidence.Profile) == profile && strings.TrimSpace(evidence.Stage) == "probe" && evidence.Probe == probe {
			return true
		}
	}
	return false
}

func validateWireShapes(data []byte) error {
	var wire struct {
		Comparison struct {
			AllowedDifferences []struct {
				Profiles []json.RawMessage `json:"profiles"`
			} `json:"allowedDifferences"`
		} `json:"comparison"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	for index, allowed := range wire.Comparison.AllowedDifferences {
		if len(allowed.Profiles) != 2 {
			return fmt.Errorf("allowedDifferences[%d] profiles must contain exactly two entries", index)
		}
	}
	return nil
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
