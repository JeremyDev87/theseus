//go:build windows

package process

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func Run(command string, args []string, cwd string, timeout time.Duration) Result {
	started := time.Now()
	actualCommand, actualArgs := command, args
	lower := strings.ToLower(command)
	if strings.HasSuffix(lower, ".cmd") || strings.HasSuffix(lower, ".bat") {
		actualCommand = "cmd.exe"
		actualArgs = append([]string{"/d", "/s", "/c", command}, args...)
	}
	cmd := exec.Command(actualCommand, actualArgs...)
	cmd.Dir = cwd
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		return Result{Stdout: stdout.String(), Stderr: stderr.String(), SpawnError: err.Error(), DurationMS: time.Since(started).Milliseconds()}
	}
	job, jobErr := createKillJob(uint32(cmd.Process.Pid))
	if jobErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return Result{
			Stdout: stdout.String(), Stderr: stderr.String(),
			SpawnError: fmt.Sprintf("unable to establish Windows Job Object: %v", jobErr),
			DurationMS: time.Since(started).Milliseconds(),
		}
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
		if err := windows.TerminateJobObject(job, 1); err != nil {
			cleanupError = fmt.Sprintf("unable to terminate Windows Job Object: %v", err)
			_ = cmd.Process.Kill()
		}
		waitErr = <-done
	}
	_ = windows.CloseHandle(job)
	result := Result{Stdout: stdout.String(), Stderr: stderr.String(), TimedOut: timedOut, DurationMS: time.Since(started).Milliseconds()}
	if cmd.ProcessState != nil {
		code := cmd.ProcessState.ExitCode()
		result.ExitCode = &code
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

func createKillJob(pid uint32) (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	processHandle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	defer windows.CloseHandle(processHandle)
	if err := windows.AssignProcessToJobObject(job, processHandle); err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}
