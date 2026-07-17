package compare

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/JeremyDev87/theseus/internal/model"
)

func Receipts(contract model.Contract, receipts []model.RunReceipt) model.ComparisonResult {
	result := model.ComparisonResult{Findings: []model.Finding{}, AllowedDifferences: []model.AllowedDifference{}}
	for _, receipt := range receipts {
		for _, probe := range contract.Probes {
			observed := findProbe(receipt.Probes, probe.ID)
			if observed == nil {
				continue
			}
			if probe.Expect != nil && probe.Expect.Exit != nil && !equalIntPointer(observed.ExitCode, probe.Expect.Exit) {
				result.Findings = append(result.Findings, model.Finding{
					Code: "THS-EXPECT-001", Probe: probe.ID, Field: "exit", Profiles: []string{receipt.Profile},
					Message:  fmt.Sprintf("%s/%s: expected exit %d, observed %s", receipt.Profile, probe.ID, *probe.Expect.Exit, displayExit(observed.ExitCode)),
					Expected: *probe.Expect.Exit, Actual: exitValue(observed.ExitCode),
				})
			}
			if probe.Expect != nil && len(probe.Expect.StdoutJSON) > 0 {
				checkJSON(probe.ID, receipt.Profile, *observed, probe.Expect.StdoutJSON, &result.Findings)
			}
		}
	}
	for leftIndex := 0; leftIndex < len(receipts); leftIndex++ {
		for rightIndex := leftIndex + 1; rightIndex < len(receipts); rightIndex++ {
			left, right := receipts[leftIndex], receipts[rightIndex]
			for _, probe := range contract.Probes {
				leftProbe, rightProbe := findProbe(left.Probes, probe.ID), findProbe(right.Probes, probe.ID)
				if leftProbe == nil || rightProbe == nil {
					continue
				}
				for _, field := range probe.Compare {
					leftValue, rightValue, leftDisplay, rightDisplay := fieldValues(field, left, right, *leftProbe, *rightProbe)
					if reflect.DeepEqual(leftValue, rightValue) {
						continue
					}
					if allowed := findAllowed(contract.AllowedDeltas, probe.ID, field, left.Profile, right.Profile, leftValue, rightValue); allowed != nil {
						result.AllowedDifferences = append(result.AllowedDifferences, model.AllowedDifference{Probe: probe.ID, Field: field, Profiles: [2]string{left.Profile, right.Profile}, Reason: allowed.Reason})
						continue
					}
					result.Findings = append(result.Findings, finding(field, probe.ID, left.Profile, right.Profile, leftValue, rightValue, leftDisplay, rightDisplay))
				}
			}
		}
	}
	return result
}

func findProbe(probes []model.ProbeReceipt, id string) *model.ProbeReceipt {
	for index := range probes {
		if probes[index].ID == id {
			return &probes[index]
		}
	}
	return nil
}

func fieldValues(field model.CompareField, left, right model.RunReceipt, leftProbe, rightProbe model.ProbeReceipt) (any, any, any, any) {
	switch field {
	case model.FieldExit:
		return exitValue(leftProbe.ExitCode), exitValue(rightProbe.ExitCode), exitValue(leftProbe.ExitCode), exitValue(rightProbe.ExitCode)
	case model.FieldStdout:
		return leftProbe.StdoutSHA256, rightProbe.StdoutSHA256, leftProbe.Stdout, rightProbe.Stdout
	case model.FieldStderr:
		return leftProbe.StderrSHA256, rightProbe.StderrSHA256, leftProbe.Stderr, rightProbe.Stderr
	default:
		return left.Runtime.SHA256, right.Runtime.SHA256, left.Runtime.Canonical, right.Runtime.Canonical
	}
}

func findAllowed(deltas []model.AllowedDelta, probe string, field model.CompareField, leftProfile, rightProfile string, left, right any) *model.AllowedDelta {
	for index := range deltas {
		delta := &deltas[index]
		if delta.Probe != probe || delta.Field != field {
			continue
		}
		if delta.Between[0] == leftProfile && delta.Between[1] == rightProfile && allowedValues(*delta, left, right) {
			return delta
		}
		if delta.Between[0] == rightProfile && delta.Between[1] == leftProfile && allowedValues(*delta, right, left) {
			return delta
		}
	}
	return nil
}

func allowedValues(delta model.AllowedDelta, from, to any) bool {
	if delta.Field == model.FieldExit {
		return reflect.DeepEqual(nullableValue(delta.From), from) && reflect.DeepEqual(nullableValue(delta.To), to)
	}
	return delta.FromSHA256 == fmt.Sprint(from) && delta.ToSHA256 == fmt.Sprint(to)
}

func nullableValue(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func finding(field model.CompareField, probe, leftProfile, rightProfile string, left, right, displayLeft, displayRight any) model.Finding {
	profiles := []string{leftProfile, rightProfile}
	actual := map[string]any{leftProfile: displayLeft, rightProfile: displayRight}
	if field == model.FieldExit {
		return model.Finding{Code: "THS-PARITY-001", Probe: probe, Field: string(field), Profiles: profiles, Message: fmt.Sprintf("%s: exit differs between %s and %s", probe, leftProfile, rightProfile), Actual: actual}
	}
	code := "THS-PARITY-002"
	if field == model.FieldRuntime {
		code = "THS-RUNTIME-001"
	}
	return model.Finding{Code: code, Probe: probe, Field: string(field), Profiles: profiles, Message: fmt.Sprintf("%s: %s differs between %s and %s", probe, field, leftProfile, rightProfile), Actual: actual, Digests: map[string]string{leftProfile: fmt.Sprint(left), rightProfile: fmt.Sprint(right)}}
}

func checkJSON(probe, profile string, receipt model.ProbeReceipt, expectations []model.JSONPathExpectation, findings *[]model.Finding) {
	var parsed any
	if !json.Valid([]byte(receipt.Stdout)) {
		*findings = append(*findings, model.Finding{Code: "THS-EXPECT-002", Probe: probe, Field: "stdoutJson", Profiles: []string{profile}, Message: fmt.Sprintf("%s/%s: stdout is not valid JSON", profile, probe)})
		return
	}
	decoder := json.NewDecoder(bytes.NewBufferString(receipt.Stdout))
	decoder.UseNumber()
	if err := decoder.Decode(&parsed); err != nil {
		*findings = append(*findings, model.Finding{Code: "THS-EXPECT-002", Probe: probe, Field: "stdoutJson", Profiles: []string{profile}, Message: fmt.Sprintf("%s/%s: stdout is not valid JSON", profile, probe)})
		return
	}
	for _, expectation := range expectations {
		value, found := getPath(parsed, expectation.Path)
		actualType := jsonType(value, found)
		if actualType != string(expectation.Type) {
			*findings = append(*findings, model.Finding{Code: "THS-EXPECT-002", Probe: probe, Field: "stdoutJson", Profiles: []string{profile}, Message: fmt.Sprintf("%s/%s: JSON path %s expected %s, observed %s", profile, probe, expectation.Path, expectation.Type, actualType), Expected: string(expectation.Type), Actual: actualType})
		}
	}
}

func getPath(value any, path string) (any, bool) {
	segments := strings.Split(path, ".")
	if strings.HasPrefix(path, "/") {
		var ok bool
		segments, ok = jsonPointer(path)
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
				current = float64(len(typed))
				continue
			}
			index, ok := arrayIndex(segment, len(typed))
			if !ok {
				return nil, false
			}
			current = typed[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func jsonPointer(path string) ([]string, bool) {
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

func arrayIndex(value string, length int) (int, bool) {
	if value == "" || (len(value) > 1 && value[0] == '0') {
		return 0, false
	}
	index := 0
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, false
		}
		index = index*10 + int(char-'0')
	}
	return index, index >= 0 && index < length
}

func jsonType(value any, found bool) string {
	if !found {
		return "undefined"
	}
	if value == nil {
		return "null"
	}
	switch value.(type) {
	case string:
		return "string"
	case float64, json.Number:
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "undefined"
	}
}

func equalIntPointer(left, right *int) bool {
	return reflect.DeepEqual(exitValue(left), exitValue(right))
}
func exitValue(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}
func displayExit(value *int) string {
	if value == nil {
		return "null"
	}
	return fmt.Sprint(*value)
}
