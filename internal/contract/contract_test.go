package contract

import (
	"strings"
	"testing"
)

const valid = `{
  "version": 1,
  "profiles": {
    "default": {"installArgs": []},
    "noOptional": {"installArgs": ["--omit=optional"]}
  },
  "probes": [{"id":"help","argv":["--help"],"timeoutMs":5000,"compare":["exit","stdout","stderr","runtime"],"expect":{"exit":0}}]
}`

func TestParseVersionedFailClosedContract(t *testing.T) {
	parsed, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Version != 1 || len(parsed.Probes) != 1 {
		t.Fatalf("unexpected parse: %#v", parsed)
	}
	if strings.Join(parsed.ProfileOrder, ",") != "default,noOptional" {
		t.Fatalf("profile order drift: %v", parsed.ProfileOrder)
	}
}

func TestRejectsUnknownFields(t *testing.T) {
	for _, input := range []string{
		strings.Replace(valid, `"version": 1,`, `"version": 1, "silent": true,`, 1),
		strings.Replace(valid, `{"installArgs": []}`, `{"installArgs": [], "fallback": true}`, 1),
	} {
		if _, err := Parse([]byte(input)); err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("expected unknown-field rejection, got %v", err)
		}
	}
}

func TestRejectsInvalidAndNonPortableNormalizers(t *testing.T) {
	base := strings.TrimSuffix(valid, "\n}")
	for _, normalizer := range []string{
		`,"normalizers":[{"id":"bad","fields":["stdout"],"pattern":"[","replacement":""}]}`,
		`,"normalizers":[{"id":"bad","fields":["stdout"],"pattern":"x","flags":"y","replacement":""}]}`,
		`,"normalizers":[{"id":"bad","fields":["stdout"],"pattern":"\\Afoo","replacement":""}]}`,
		`,"normalizers":[{"id":"bad","fields":["stdout"],"pattern":"(?i)foo","replacement":""}]}`,
		`,"normalizers":[{"id":"bad","fields":["stdout"],"pattern":"[[:alpha:]]+","replacement":""}]}`,
		`,"normalizers":[{"id":"bad","fields":["stdout"],"pattern":"(x)","replacement":"$&"}]}`,
		`,"normalizers":[{"id":"bad","fields":["stdout"],"pattern":"(x)","replacement":"$0"}]}`,
		`,"normalizers":[{"id":"bad","fields":["stdout"],"pattern":"(x)","replacement":"$2"}]}`,
	} {
		if _, err := Parse([]byte(base + normalizer)); err == nil {
			t.Fatalf("expected normalizer rejection for %s", normalizer)
		}
	}
}

func TestAcceptsPortableCaptureReplacement(t *testing.T) {
	base := strings.TrimSuffix(valid, "\n}")
	input := base + `,"normalizers":[{"id":"capture","fields":["stdout"],"pattern":"([0-9]+)","replacement":"[$1]"}]}`
	if _, err := Parse([]byte(input)); err != nil {
		t.Fatalf("expected portable normalizer acceptance: %v", err)
	}
}

func TestRejectsNullExpectationAndReversedDeltaDuplicate(t *testing.T) {
	nullExit := strings.Replace(valid, `"expect":{"exit":0}`, `"expect":{"exit":null,"stdoutJson":[{"path":"x","type":"string"}]}`, 1)
	if _, err := Parse([]byte(nullExit)); err == nil {
		t.Fatal("expected null exit rejection")
	}
	base := strings.TrimSuffix(valid, "\n}")
	input := base + `,"allowedDeltas":[
	  {"probe":"help","between":["default","noOptional"],"field":"exit","from":0,"to":1,"reason":"one"},
	  {"probe":"help","between":["noOptional","default"],"field":"exit","from":1,"to":0,"reason":"same"}
	]}`
	if _, err := Parse([]byte(input)); err == nil || !strings.Contains(err.Error(), "duplicate allowed delta") {
		t.Fatalf("expected duplicate rejection, got %v", err)
	}
}

func TestRejectsDuplicateJSONExpectationIdentity(t *testing.T) {
	input := strings.Replace(valid, `"expect":{"exit":0}`, `"expect":{"stdoutJson":[{"path":"meta.version","type":"string"},{"path":"meta.version","type":"string"}]}`, 1)
	if _, err := Parse([]byte(input)); err == nil || !strings.Contains(err.Error(), "duplicate stdoutJson expectation") {
		t.Fatalf("expected duplicate stdoutJson identity rejection, got %v", err)
	}
}

func TestAcceptsJSONNumericSpellingsThatAreIntegers(t *testing.T) {
	input := strings.Replace(valid, `"version": 1`, `"version": 1.0`, 1)
	input = strings.Replace(input, `"timeoutMs":5000`, `"timeoutMs":5e3`, 1)
	input = strings.Replace(input, `"expect":{"exit":0}`, `"expect":{"exit":0.0}`, 1)
	contract, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if contract.Version != 1 || contract.Probes[0].TimeoutMS != 5000 || contract.Probes[0].Expect == nil || contract.Probes[0].Expect.Exit == nil || *contract.Probes[0].Expect.Exit != 0 {
		t.Fatalf("unexpected numeric parsing %#v", contract)
	}
}

func TestRejectsNullWhereTypeScriptContractRequiresAValue(t *testing.T) {
	base := strings.TrimSuffix(valid, "\n}")
	cases := []string{
		strings.Replace(valid, `"version": 1,`, `"version": 1, "source": null,`, 1),
		strings.Replace(valid, `"default": {"installArgs": []}`, `"default": null`, 1),
		strings.Replace(valid, `"timeoutMs":5000`, `"timeoutMs":null`, 1),
		strings.Replace(valid, `"expect":{"exit":0}`, `"expect":null`, 1),
		strings.Replace(valid, `"expect":{"exit":0}`, `"expect":{"exit":0,"stdoutJson":null}`, 1),
		base + `,"normalizers":[{"id":"null-probe","probe":null,"fields":["stdout"],"pattern":"x","replacement":""}]}`,
		base + `,"normalizers":[{"id":"null-flags","fields":["stdout"],"pattern":"x","flags":null,"replacement":""}]}`,
	}
	for _, input := range cases {
		if _, err := Parse([]byte(input)); err == nil {
			t.Fatalf("expected null rejection for %s", input)
		}
	}
}

func TestRejectsMissingNormalizerValuesAndExplicitEmptyPaths(t *testing.T) {
	base := strings.TrimSuffix(valid, "\n}")
	for _, input := range []string{
		strings.Replace(valid, `"version": 1,`, `"version": 1, "bin": "",`, 1),
		strings.Replace(valid, `"version": 1,`, `"version": 1, "bin": null,`, 1),
		strings.Replace(valid, `"version": 1,`, `"version": 1, "source":{"command":"node","cwd":""},`, 1),
		strings.Replace(valid, `"version": 1,`, `"version": 1, "source":{"command":"node","cwd":null},`, 1),
		base + `,"normalizers":[{"id":"missing-pattern","fields":["stdout"],"replacement":""}]}`,
		base + `,"normalizers":[{"id":"missing-replacement","fields":["stdout"],"pattern":"x"}]}`,
	} {
		if _, err := Parse([]byte(input)); err == nil {
			t.Fatalf("expected fail-closed rejection for %s", input)
		}
	}
}

func TestRejectsDuplicateProfileNames(t *testing.T) {
	input := strings.Replace(valid, `"default": {"installArgs": []},`, `"default":{"installArgs":[]},"default":{"installArgs":["--legacy-peer-deps"]},`, 1)
	if _, err := Parse([]byte(input)); err == nil || !strings.Contains(err.Error(), "duplicate profile name") {
		t.Fatalf("expected duplicate profile rejection, got %v", err)
	}
}

func TestReservesSourceProfileName(t *testing.T) {
	input := strings.Replace(valid, `"profiles": {`, `"source":{"command":"node","args":[],"cwd":"."},"profiles": {"source":{"installArgs":[]},`, 1)
	if _, err := Parse([]byte(input)); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("expected reserved-name rejection, got %v", err)
	}
}
