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
	for _, command := range []string{`C:	ools\fixture.exe`, `C:	ools\fixture.cmd`} {
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
	if err := os.WriteFile(script, []byte("@echo off\r\necho %~1\r\nexit /b 0\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := Run(script, []string{"hello & world"}, root, 5*time.Second)
	if result.SpawnError != "" || result.ExitCode == nil || *result.ExitCode != 0 || strings.TrimSpace(result.Stdout) != "hello & world" {
		t.Fatalf("unexpected .cmd result: %#v", result)
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
