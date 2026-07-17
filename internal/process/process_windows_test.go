//go:build windows

package process

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsCommandStartsSuspended(t *testing.T) {
	for _, command := range []string{`C:\tools\fixture.exe`, `C:\tools\fixture.cmd`} {
		cmd := windowsCommand(command, []string{"argument"})
		if cmd.SysProcAttr == nil || cmd.SysProcAttr.CreationFlags&windows.CREATE_SUSPENDED == 0 {
			t.Fatalf("%s must start suspended before Job Object assignment", command)
		}
	}
}

func TestCommandScriptArgumentsWithSpaces(t *testing.T) {
	root := filepath.Join(t.TempDir(), "directory with spaces")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "fixture command.cmd")
	// Capture the expanded argument in a quoted SET assignment so cmd.exe does
	// not interpret metacharacters such as '&'. The SET listing writes the
	// stored value without expanding it again, preserving exact argv evidence.
	outputFile := filepath.Join(t.TempDir(), "argv.txt")
	scriptContent := "@echo off\r\n" +
		"set \"THESEUS_ARG=%~1\"\r\n" +
		"> \"" + outputFile + "\" set THESEUS_ARG\r\n" +
		"exit /b 0\r\n"
	if err := os.WriteFile(script, []byte(scriptContent), 0o644); err != nil {
		t.Fatal(err)
	}
	result := Run(script, []string{"hello & world"}, root, 5*time.Second)
	if result.SpawnError != "" || result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatalf("unexpected .cmd result: %#v", result)
	}
	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("output file not created: %v", err)
	}
	got := strings.TrimSpace(string(data))
	want := "THESEUS_ARG=hello & world"
	if got != want {
		t.Fatalf("argument value mismatch: got %q want %q", got, want)
	}
}

func TestTimeoutKillsJobDescendants(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "leaked.txt")
	t.Setenv("THESEUS_PROCESS_MARKER", marker)
	script := `$p = Start-Process powershell.exe -PassThru -ArgumentList '-NoProfile','-NonInteractive','-Command','Start-Sleep -Seconds 2; Set-Content -LiteralPath $env:THESEUS_PROCESS_MARKER -Value leaked'; Wait-Process -Id $p.Id`
	result := Run("powershell.exe", []string{"-NoProfile", "-NonInteractive", "-Command", script}, t.TempDir(), 200*time.Millisecond)
	if !result.TimedOut || result.SpawnError != "" {
		t.Fatalf("unexpected timeout result: %#v", result)
	}
	time.Sleep(3 * time.Second)
	if _, err := os.Stat(marker); err == nil || !os.IsNotExist(err) {
		t.Fatalf("Windows Job Object did not kill descendant; marker stat=%v", err)
	}
}
