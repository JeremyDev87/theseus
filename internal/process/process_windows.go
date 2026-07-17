//go:build windows

package process

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func Run(command string, args []string, cwd string, timeout time.Duration) Result {
	started := time.Now()
	cmd := windowsCommand(command, args)
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
	if resumeErr := resumeMainThread(uint32(cmd.Process.Pid)); resumeErr != nil {
		_ = windows.TerminateJobObject(job, 1)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = windows.CloseHandle(job)
		return Result{
			Stdout: stdout.String(), Stderr: stderr.String(),
			SpawnError: fmt.Sprintf("unable to resume process after Windows Job Object assignment: %v", resumeErr),
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

func windowsCommand(command string, args []string) *exec.Cmd {
	lower := strings.ToLower(command)
	if !strings.HasSuffix(lower, ".cmd") && !strings.HasSuffix(lower, ".bat") {
		cmd := exec.Command(command, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
		return cmd
	}
	comspec := os.Getenv("COMSPEC")
	if comspec == "" {
		if systemRoot := os.Getenv("SystemRoot"); systemRoot != "" {
			comspec = filepath.Join(systemRoot, "System32", "cmd.exe")
		} else {
			comspec = "cmd.exe"
		}
	}
	cmd := exec.Command(comspec)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:       syscall.EscapeArg(comspec) + " " + cmdBatchArguments(command, args),
		CreationFlags: windows.CREATE_SUSPENDED,
	}
	return cmd
}

func resumeMainThread(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	if err := windows.Thread32First(snapshot, &entry); err != nil {
		return err
	}
	for {
		if entry.OwnerProcessID == pid {
			thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
			if err != nil {
				return err
			}
			previous, resumeErr := windows.ResumeThread(thread)
			_ = windows.CloseHandle(thread)
			if resumeErr != nil {
				return resumeErr
			}
			if previous != 1 {
				return fmt.Errorf("unexpected primary thread suspend count %d", previous)
			}
			return nil
		}
		if err := windows.Thread32Next(snapshot, &entry); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				return fmt.Errorf("primary thread not found for process %d", pid)
			}
			return err
		}
	}
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
