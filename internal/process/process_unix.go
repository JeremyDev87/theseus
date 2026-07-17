//go:build !windows

package process

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

func Run(command string, args []string, cwd string, timeout time.Duration) Result {
	started := time.Now()
	cmd := exec.Command(command, args...)
	cmd.Dir = cwd
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		return Result{Stdout: stdout.String(), Stderr: stderr.String(), SpawnError: err.Error(), DurationMS: time.Since(started).Milliseconds()}
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var waitErr error
	timedOut := false
	cleanupError := ""
	select {
	case waitErr = <-done:
	case <-timer.C:
		timedOut = true
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			cleanupError = fmt.Sprintf("unable to kill process group: %v", err)
			_ = cmd.Process.Kill()
		}
		waitErr = <-done
	}
	result := Result{Stdout: stdout.String(), Stderr: stderr.String(), TimedOut: timedOut, DurationMS: time.Since(started).Milliseconds()}
	if cmd.ProcessState != nil {
		status, signaled := cmd.ProcessState.Sys().(syscall.WaitStatus)
		if signaled && status.Signaled() {
			name := signalName(status.Signal())
			result.Signal = &name
		} else {
			code := cmd.ProcessState.ExitCode()
			result.ExitCode = &code
		}
	}
	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) {
		result.SpawnError = waitErr.Error()
	}
	if cleanupError != "" {
		result.SpawnError = cleanupError
	}
	return result
}

func signalName(signal syscall.Signal) string {
	switch signal {
	case syscall.SIGKILL:
		return "SIGKILL"
	case syscall.SIGTERM:
		return "SIGTERM"
	case syscall.SIGINT:
		return "SIGINT"
	case syscall.SIGABRT:
		return "SIGABRT"
	case syscall.SIGSEGV:
		return "SIGSEGV"
	default:
		return signal.String()
	}
}
