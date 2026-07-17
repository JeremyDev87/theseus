package runner

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JeremyDev87/theseus/internal/contract"
	"github.com/JeremyDev87/theseus/internal/model"
	"github.com/JeremyDev87/theseus/internal/report"
)

func TestVerifyReturnsWorkingDirectoryAndCleanupErrors(t *testing.T) {
	originalGetwd, originalRemoveAll := currentWorkingDirectory, removeTemporaryRoot
	currentWorkingDirectory = func() (string, error) { return "", errors.New("cwd unavailable") }
	removeTemporaryRoot = func(string) error { return errors.New("cleanup unavailable") }
	t.Cleanup(func() {
		currentWorkingDirectory, removeTemporaryRoot = originalGetwd, originalRemoveAll
	})

	_, err := Verify("unused", model.Contract{})
	if err == nil || !strings.Contains(err.Error(), "cwd unavailable") || !strings.Contains(err.Error(), "cleanup unavailable") {
		t.Fatalf("Verify error=%v; want working-directory and cleanup failures", err)
	}
}

func fixtureRoot(t *testing.T) string {
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixtures", "parity-package"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestPackInstallAndCompareTwoProfiles(t *testing.T) {
	fixture := fixtureRoot(t)
	input := `{"version":1,"source":{"command":"node","args":["bin/parity-fixture.js"],"cwd":` + quote(fixture) + `},"profiles":{"default":{"installArgs":[]},"noOptional":{"installArgs":["--omit=optional"]}},"probes":[{"id":"help","argv":["--help"],"compare":["exit","stdout","stderr"],"expect":{"exit":0}},{"id":"cwd","argv":["--cwd"],"compare":["exit","stdout","stderr"],"expect":{"exit":0}}]}`
	config, err := contract.Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Verify(fixture, config)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Incomplete) != 0 || len(result.Receipts) != 3 || len(result.Comparison.Findings) != 0 {
		t.Fatalf("result=%#v", result)
	}
	if result.Artifact.PackageName != "theseus-parity-fixture" {
		t.Fatalf("artifact=%#v", result.Artifact)
	}
}

func TestTimeoutIsIncompleteExitTwo(t *testing.T) {
	fixture := fixtureRoot(t)
	input := `{"version":1,"profiles":{"default":{"installArgs":[]},"noOptional":{"installArgs":["--omit=optional"]}},"probes":[{"id":"hang","argv":["--hang"],"timeoutMs":100,"compare":["exit","stdout","stderr"]}]}`
	config, err := contract.Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Verify(fixture, config)
	if err != nil {
		t.Fatal(err)
	}
	final := report.Create(fixture, result)
	if len(result.Incomplete) != 2 || final.Status != "incomplete" || final.ExitCode != 2 {
		t.Fatalf("incomplete=%#v report=%#v", result.Incomplete, final)
	}
}

func TestSanitizeDiagnosticCoversUnixAndWindowsNPMLogs(t *testing.T) {
	input := "/Users/test/.npm/_logs/2026-debug.log\nC:\\Users\\test\\AppData\\Local\\npm-cache\\_logs\\2026-debug.log"
	got := sanitizeDiagnostic(input, "/tmp/unrelated")
	if got != "<npm-log>\n<npm-log>" {
		t.Fatalf("diagnostic path leak: %q", got)
	}
}

func TestResolveBinMatchesManifestFiltering(t *testing.T) {
	if _, err := resolveBin(packageManifest{Bin: json.RawMessage(`null`)}, "fixture", ""); err == nil {
		t.Fatal("null bin must not resolve")
	}
	manifest := packageManifest{Bin: json.RawMessage(`{"valid":"bin.js","ignored":42}`)}
	name, err := resolveBin(manifest, "fixture", "")
	if err != nil || name != "valid" {
		t.Fatalf("filtered bin resolution: name=%q err=%v", name, err)
	}
}

func TestPortableNormalizerSubset(t *testing.T) {
	configs := []model.NormalizerConfig{{ID: "numbers", Fields: []string{"stdout"}, Pattern: `[0-9]+`, Flags: "g", Replacement: "<n>"}}
	got := normalize("value 123 and 456\r\n", "stdout", "probe", configs, nil)
	if got != "value <n> and <n>\n" {
		t.Fatalf("global normalization drift: %q", got)
	}
	configs[0].Flags = ""
	configs[0].Pattern = `([0-9]+)`
	configs[0].Replacement = "$$[$1]"
	got = normalize("123 456", "stdout", "probe", configs, nil)
	if got != "$[123] 456" {
		t.Fatalf("single normalization drift: %q", got)
	}
	got = normalize("/tmp/subject/bin\n", "stdout", "probe", nil, []string{"/tmp/subject"})
	if got != "<subject-root>/bin\n" {
		t.Fatalf("automatic root normalization drift: %q", got)
	}
}

func quote(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}
