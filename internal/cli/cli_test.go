package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func invoke(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	exit := Main(args, &stdout, &stderr)
	return exit, stdout.String(), stderr.String()
}

func TestHelpVersionAndErrors(t *testing.T) {
	exit, out, errout := invoke("--help")
	wantHelp := "Theseus 0.1.0\n\nDistribution parity tests for packaged CLIs.\n\nUsage:\n  theseus verify <package|directory|tgz> --contract <file> [--format text|json]\n  theseus --help\n  theseus --version\n\nExit codes:\n  0  all declared contracts hold\n  1  behavior drift\n  2  contract, pack, install, or probe incomplete\n"
	if exit != 0 || out != wantHelp || errout != "" {
		t.Fatalf("help %d %q %q", exit, out, errout)
	}
	exit, out, errout = invoke("--version")
	if exit != 0 || out != "0.1.0\n" || errout != "" {
		t.Fatalf("version %d %q %q", exit, out, errout)
	}
	exit, _, errout = invoke("wat")
	if exit != 2 || !strings.Contains(errout, "unknown command wat") {
		t.Fatalf("unknown %d %q", exit, errout)
	}
	exit, _, errout = invoke("verify", ".", "--format", "yaml")
	if exit != 2 || !strings.Contains(errout, "--format must be text or json") {
		t.Fatalf("format %d %q", exit, errout)
	}
}

func TestMissingContractJSONIncomplete(t *testing.T) {
	exit, out, errout := invoke("verify", ".", "--contract", filepath.Join(t.TempDir(), "missing.json"), "--format", "json")
	if exit != 2 || errout != "" {
		t.Fatalf("exit=%d stderr=%q", exit, errout)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["schemaVersion"] != float64(1) || parsed["status"] != "incomplete" || parsed["exitCode"] != float64(2) {
		t.Fatalf("report=%v", parsed)
	}
	tool, ok := parsed["tool"].(map[string]any)
	if !ok || tool["name"] != "theseus" || tool["version"] != "0.1.0" {
		t.Fatalf("tool=%v", parsed["tool"])
	}
	errorBody, ok := parsed["error"].(map[string]any)
	if !ok || errorBody["code"] != "THS-INCOMPLETE-001" {
		t.Fatalf("error=%v", parsed["error"])
	}
}
