package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/JeremyDev87/theseus/internal/model"
)

type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }

var (
	profileNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
	digestPattern      = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type wireContract struct {
	Version       json.RawMessage    `json:"version"`
	Bin           json.RawMessage    `json:"bin"`
	Source        json.RawMessage    `json:"source"`
	Profiles      json.RawMessage    `json:"profiles"`
	Probes        []wireProbe        `json:"probes"`
	Normalizers   []wireNormalizer   `json:"normalizers"`
	AllowedDeltas []wireAllowedDelta `json:"allowedDeltas"`
}

type wireSource struct {
	Command string          `json:"command"`
	Args    []string        `json:"args"`
	CWD     json.RawMessage `json:"cwd"`
}

type wireProbe struct {
	ID        string          `json:"id"`
	Argv      []string        `json:"argv"`
	TimeoutMS json.RawMessage `json:"timeoutMs"`
	Compare   []string        `json:"compare"`
	Expect    json.RawMessage `json:"expect"`
}

type wireExpectation struct {
	Exit       json.RawMessage `json:"exit"`
	StdoutJSON json.RawMessage `json:"stdoutJson"`
}

type wireJSONExpectation struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

type wireNormalizer struct {
	ID          string          `json:"id"`
	Probe       json.RawMessage `json:"probe"`
	Fields      []string        `json:"fields"`
	Pattern     *string         `json:"pattern"`
	Flags       json.RawMessage `json:"flags"`
	Replacement *string         `json:"replacement"`
}

type wireAllowedDelta struct {
	Probe      string          `json:"probe"`
	Between    []string        `json:"between"`
	Field      string          `json:"field"`
	Reason     string          `json:"reason"`
	From       json.RawMessage `json:"from"`
	To         json.RawMessage `json:"to"`
	FromSHA256 string          `json:"fromSha256"`
	ToSHA256   string          `json:"toSha256"`
}

func Parse(data []byte) (model.Contract, error) {
	var wire wireContract
	if err := strictDecode(data, &wire); err != nil {
		return model.Contract{}, &Error{Message: "contract: " + err.Error()}
	}
	version, err := parseInteger(wire.Version)
	if err != nil || version != 1 {
		return model.Contract{}, fail("contract.version must be 1")
	}
	profiles, order, err := parseProfiles(wire.Profiles)
	if err != nil {
		return model.Contract{}, err
	}
	if len(profiles) < 2 {
		return model.Contract{}, fail("profiles must declare at least two installation profiles")
	}
	result := model.Contract{
		Version:       1,
		Profiles:      profiles,
		ProfileOrder:  order,
		Probes:        []model.ProbeConfig{},
		Normalizers:   []model.NormalizerConfig{},
		AllowedDeltas: []model.AllowedDelta{},
	}
	if len(wire.Bin) > 0 {
		var bin string
		if string(wire.Bin) == "null" || strictDecode(wire.Bin, &bin) != nil || strings.TrimSpace(bin) == "" {
			return model.Contract{}, fail("bin must not be empty")
		}
		result.Bin = bin
	}
	if len(wire.Source) > 0 {
		if string(wire.Source) == "null" {
			return model.Contract{}, fail("source must be an object")
		}
		var source wireSource
		if err := strictDecode(wire.Source, &source); err != nil {
			return model.Contract{}, fail("source: " + err.Error())
		}
		if strings.TrimSpace(source.Command) == "" {
			return model.Contract{}, fail("source.command must not be empty")
		}
		if _, exists := profiles["source"]; exists {
			return model.Contract{}, fail("profiles.source is reserved when a source subject is declared")
		}
		cwd := "."
		if len(source.CWD) > 0 {
			if string(source.CWD) == "null" || strictDecode(source.CWD, &cwd) != nil || strings.TrimSpace(cwd) == "" {
				return model.Contract{}, fail("source.cwd must not be empty")
			}
		}
		result.Source = &model.SourceConfig{Command: source.Command, Args: nonNil(source.Args), CWD: cwd}
	}
	if len(wire.Probes) == 0 {
		return model.Contract{}, fail("probes must be a non-empty array")
	}
	probeIDs := map[string]bool{}
	for index, probe := range wire.Probes {
		path := fmt.Sprintf("probes[%d]", index)
		if strings.TrimSpace(probe.ID) == "" {
			return model.Contract{}, fail(path + ".id must not be empty")
		}
		if probeIDs[probe.ID] {
			return model.Contract{}, fail("duplicate probe id: " + probe.ID)
		}
		probeIDs[probe.ID] = true
		timeout := 10_000
		if len(probe.TimeoutMS) > 0 {
			parsedTimeout, err := parseInteger(probe.TimeoutMS)
			if err != nil {
				return model.Contract{}, fail(path + ".timeoutMs must be an integer")
			}
			timeout = parsedTimeout
			if timeout <= 0 {
				return model.Contract{}, fail(path + ".timeoutMs must be positive")
			}
		}
		compare := probe.Compare
		if compare == nil {
			compare = []string{"exit", "stdout", "stderr"}
		}
		if len(compare) == 0 {
			return model.Contract{}, fail(path + ".compare must contain only exit, stdout, stderr, or runtime")
		}
		seenFields := map[string]bool{}
		fields := make([]model.CompareField, 0, len(compare))
		for _, field := range compare {
			if !validField(field) {
				return model.Contract{}, fail(path + ".compare must contain only exit, stdout, stderr, or runtime")
			}
			if seenFields[field] {
				return model.Contract{}, fail(path + ".compare contains duplicates")
			}
			seenFields[field] = true
			fields = append(fields, model.CompareField(field))
		}
		config := model.ProbeConfig{ID: probe.ID, Argv: nonNil(probe.Argv), TimeoutMS: timeout, Compare: fields}
		if len(probe.Expect) > 0 {
			if string(probe.Expect) == "null" {
				return model.Contract{}, fail(path + ".expect must be an object")
			}
			expect, err := parseExpectation(probe.Expect, path+".expect")
			if err != nil {
				return model.Contract{}, err
			}
			config.Expect = expect
		}
		result.Probes = append(result.Probes, config)
	}
	for index, normalizer := range wire.Normalizers {
		path := fmt.Sprintf("normalizers[%d]", index)
		if strings.TrimSpace(normalizer.ID) == "" {
			return model.Contract{}, fail(path + ".id must not be empty")
		}
		probeName := ""
		if len(normalizer.Probe) > 0 {
			if string(normalizer.Probe) == "null" || strictDecode(normalizer.Probe, &probeName) != nil || strings.TrimSpace(probeName) == "" {
				return model.Contract{}, fail(path + ".probe must not be empty")
			}
			if !probeIDs[probeName] {
				return model.Contract{}, fail(path + ".probe references unknown probe " + probeName)
			}
		}
		if len(normalizer.Fields) == 0 {
			return model.Contract{}, fail(path + ".fields must contain stdout or stderr")
		}
		fieldSeen := map[string]bool{}
		fields := []string{}
		for _, field := range normalizer.Fields {
			if field != "stdout" && field != "stderr" {
				return model.Contract{}, fail(path + ".fields must contain stdout or stderr")
			}
			if !fieldSeen[field] {
				fieldSeen[field] = true
				fields = append(fields, field)
			}
		}
		if normalizer.Pattern == nil {
			return model.Contract{}, fail(path + ".pattern must be a string")
		}
		if normalizer.Replacement == nil {
			return model.Contract{}, fail(path + ".replacement must be a string")
		}
		flags := "g"
		if len(normalizer.Flags) > 0 {
			if string(normalizer.Flags) == "null" || strictDecode(normalizer.Flags, &flags) != nil {
				return model.Contract{}, fail(path + ".flags must be a string")
			}
		}
		prefix, err := regexPrefix(flags)
		if err != nil {
			return model.Contract{}, fail(path + ": " + err.Error())
		}
		compiled, err := regexp.Compile(prefix + *normalizer.Pattern)
		if err != nil {
			return model.Contract{}, fail(path + ": invalid regular expression: " + err.Error())
		}
		if err := validatePortableNormalizer(*normalizer.Pattern, *normalizer.Replacement, compiled.NumSubexp()); err != nil {
			return model.Contract{}, fail(path + ": " + err.Error())
		}
		result.Normalizers = append(result.Normalizers, model.NormalizerConfig{ID: normalizer.ID, Probe: probeName, Fields: fields, Pattern: *normalizer.Pattern, Flags: flags, Replacement: *normalizer.Replacement})
	}
	profileNames := map[string]bool{}
	for name := range profiles {
		profileNames[name] = true
	}
	if result.Source != nil {
		profileNames["source"] = true
	}
	seenDeltas := map[string]bool{}
	for index, delta := range wire.AllowedDeltas {
		path := fmt.Sprintf("allowedDeltas[%d]", index)
		if !probeIDs[delta.Probe] {
			return model.Contract{}, fail(path + ".probe references unknown probe " + delta.Probe)
		}
		if len(delta.Between) != 2 || delta.Between[0] == delta.Between[1] {
			return model.Contract{}, fail(path + ".between must contain two different profiles")
		}
		for _, name := range delta.Between {
			if !profileNames[name] {
				return model.Contract{}, fail(path + ".between references unknown profile " + name)
			}
		}
		if !validField(delta.Field) {
			return model.Contract{}, fail(path + ".field is invalid")
		}
		if strings.TrimSpace(delta.Reason) == "" {
			return model.Contract{}, fail(path + ".reason must not be empty")
		}
		item := model.AllowedDelta{Probe: delta.Probe, Between: [2]string{delta.Between[0], delta.Between[1]}, Field: model.CompareField(delta.Field), Reason: delta.Reason}
		if delta.Field == "exit" {
			if len(delta.From) == 0 || len(delta.To) == 0 {
				return model.Contract{}, fail(path + ": exit delta requires from and to")
			}
			item.FromSet, item.ToSet = true, true
			if string(delta.From) != "null" {
				value, err := parseInteger(delta.From)
				if err != nil {
					return model.Contract{}, fail(path + ".from must be an integer or null")
				}
				item.From = &value
			}
			if string(delta.To) != "null" {
				value, err := parseInteger(delta.To)
				if err != nil {
					return model.Contract{}, fail(path + ".to must be an integer or null")
				}
				item.To = &value
			}
			if delta.FromSHA256 != "" || delta.ToSHA256 != "" {
				return model.Contract{}, fail(path + ": exit delta cannot use digests")
			}
		} else {
			if !digestPattern.MatchString(delta.FromSHA256) || !digestPattern.MatchString(delta.ToSHA256) {
				return model.Contract{}, fail(path + ": non-exit delta requires lowercase fromSha256 and toSha256 digests")
			}
			if len(delta.From) > 0 || len(delta.To) > 0 {
				return model.Contract{}, fail(path + ": non-exit delta must use fromSha256 and toSha256")
			}
			item.FromSHA256, item.ToSHA256 = delta.FromSHA256, delta.ToSHA256
		}
		pair := []string{item.Between[0], item.Between[1]}
		sort.Strings(pair)
		key := strings.Join([]string{item.Probe, pair[0], pair[1], string(item.Field)}, "\x00")
		if seenDeltas[key] {
			return model.Contract{}, fail(path + ": duplicate allowed delta")
		}
		seenDeltas[key] = true
		result.AllowedDeltas = append(result.AllowedDeltas, item)
	}
	return result, nil
}

func parseProfiles(raw json.RawMessage) (map[string]model.ProfileConfig, []string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil, fail("profiles must be an object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, nil, fail("profiles must be an object")
	}
	profiles := map[string]model.ProfileConfig{}
	order := []string{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, nil, fail("profiles must be an object")
		}
		name := token.(string)
		if _, exists := profiles[name]; exists {
			return nil, nil, fail("duplicate profile name: " + name)
		}
		if !profileNamePattern.MatchString(name) {
			return nil, nil, fail("profiles." + name + ": invalid profile name")
		}
		var rawProfile json.RawMessage
		if err := decoder.Decode(&rawProfile); err != nil {
			return nil, nil, fail("profiles." + name + ": " + err.Error())
		}
		var config model.ProfileConfig
		if string(rawProfile) == "null" {
			return nil, nil, fail("profiles." + name + " must be an object")
		}
		if err := strictDecode(rawProfile, &config); err != nil {
			return nil, nil, fail("profiles." + name + ": " + err.Error())
		}
		config.InstallArgs = nonNil(config.InstallArgs)
		profiles[name] = config
		order = append(order, name)
	}
	if _, err := decoder.Token(); err != nil {
		return nil, nil, fail("profiles must be an object")
	}
	return profiles, order, nil
}

func parseExpectation(raw json.RawMessage, path string) (*model.ProbeExpectation, error) {
	var wire wireExpectation
	if err := strictDecode(raw, &wire); err != nil {
		return nil, fail(path + ": " + err.Error())
	}
	result := &model.ProbeExpectation{}
	if len(wire.Exit) > 0 {
		if string(wire.Exit) == "null" {
			return nil, fail(path + ".exit must be an integer")
		}
		value, err := parseInteger(wire.Exit)
		if err != nil {
			return nil, fail(path + ".exit must be an integer")
		}
		result.Exit = &value
	}
	if len(wire.StdoutJSON) > 0 {
		if string(wire.StdoutJSON) == "null" {
			return nil, fail(path + ".stdoutJson must be a non-empty array")
		}
		var stdoutJSON []wireJSONExpectation
		if err := strictDecode(wire.StdoutJSON, &stdoutJSON); err != nil || len(stdoutJSON) == 0 {
			return nil, fail(path + ".stdoutJson must be a non-empty array")
		}
		for index, item := range stdoutJSON {
			if strings.TrimSpace(item.Path) == "" {
				return nil, fail(fmt.Sprintf("%s.stdoutJson[%d].path must not be empty", path, index))
			}
			if !validJSONType(item.Type) {
				return nil, fail(fmt.Sprintf("%s.stdoutJson[%d].type is invalid", path, index))
			}
			result.StdoutJSON = append(result.StdoutJSON, model.JSONPathExpectation{Path: item.Path, Type: model.JSONValueType(item.Type)})
		}
	}
	if result.Exit == nil && len(result.StdoutJSON) == 0 {
		return nil, fail(path + " must declare exit or stdoutJson")
	}
	return result, nil
}

func parseInteger(raw json.RawMessage) (int, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, fmt.Errorf("not an integer")
	}
	value, err := strconv.ParseFloat(string(raw), 64)
	if err != nil || math.IsInf(value, 0) || math.IsNaN(value) || math.Trunc(value) != value {
		return 0, fmt.Errorf("not an integer")
	}
	limit := math.Ldexp(1, strconv.IntSize-1)
	if value >= limit || value < -limit {
		return 0, fmt.Errorf("integer is out of range")
	}
	return int(value), nil
}

func strictDecode(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func validatePortableNormalizer(pattern, replacement string, captureCount int) error {
	for index := 0; index < len(pattern); index++ {
		switch pattern[index] {
		case '\\':
			if index+1 >= len(pattern) {
				continue
			}
			index++
			character := pattern[index]
			if strings.ContainsRune("AzQEpP0123456789", rune(character)) {
				return fmt.Errorf("non-portable normalizer pattern escape \\%c", character)
			}
		case '(':
			if index+1 < len(pattern) && pattern[index+1] == '?' {
				return fmt.Errorf("non-portable normalizer pattern group starting with (?")
			}
		case '[':
			if strings.HasPrefix(pattern[index:], "[[:") {
				return fmt.Errorf("non-portable POSIX character class")
			}
		}
	}
	for index := 0; index < len(replacement); index++ {
		if replacement[index] != '$' {
			continue
		}
		if index+1 >= len(replacement) {
			return fmt.Errorf("non-portable replacement token $")
		}
		next := replacement[index+1]
		if next == '$' {
			index++
			continue
		}
		if next >= '1' && next <= '9' {
			if index+2 < len(replacement) && replacement[index+2] >= '0' && replacement[index+2] <= '9' {
				return fmt.Errorf("non-portable multi-digit replacement token")
			}
			capture := int(next - '0')
			if capture > captureCount {
				return fmt.Errorf("replacement references missing capture $%d", capture)
			}
			index++
			continue
		}
		return fmt.Errorf("non-portable replacement token $%c", next)
	}
	return nil
}

func regexPrefix(flags string) (string, error) {
	seen := map[rune]bool{}
	modes := ""
	for _, flag := range flags {
		if seen[flag] {
			return "", fmt.Errorf("normalizer flags contain duplicate %c", flag)
		}
		seen[flag] = true
		switch flag {
		case 'g':
		case 'i', 'm', 's':
			modes += string(flag)
		default:
			return "", fmt.Errorf("unsupported normalizer flag %c; allowed flags are g, i, m, s", flag)
		}
	}
	if modes == "" {
		return "", nil
	}
	return "(?" + modes + ")", nil
}

func RegexPrefix(flags string) (string, error) { return regexPrefix(flags) }
func HasGlobal(flags string) bool              { return strings.ContainsRune(flags, 'g') }

func validField(value string) bool {
	return value == "exit" || value == "stdout" || value == "stderr" || value == "runtime"
}

func validJSONType(value string) bool {
	return value == "string" || value == "number" || value == "boolean" || value == "object" || value == "array" || value == "null"
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func fail(message string) error { return &Error{Message: message} }
