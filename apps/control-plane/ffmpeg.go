package main

import (
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"
)

type Process interface {
	Done() <-chan struct{}
	Err() error
	Signal(os.Signal) error
	Kill() error
}

type ProcessRunner interface {
	Start(args []string) (Process, error)
}

type ExecRunner struct {
	Bin string
}

func (runner ExecRunner) Start(args []string) (Process, error) {
	bin := runner.Bin
	if bin == "" {
		bin = "ffmpeg"
	}

	command := exec.Command(bin, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr

	if err := command.Start(); err != nil {
		return nil, err
	}

	process := &execProcess{command: command, done: make(chan struct{})}
	go func() {
		process.err = command.Wait()
		close(process.done)
	}()
	return process, nil
}

type execProcess struct {
	command *exec.Cmd
	done    chan struct{}
	err     error
}

func (process *execProcess) Done() <-chan struct{} { return process.done }

func (process *execProcess) Err() error { return process.err }

func (process *execProcess) Signal(signal os.Signal) error {
	return process.command.Process.Signal(signal)
}

func (process *execProcess) Kill() error {
	return process.command.Process.Kill()
}

// ffmpegArgs keeps the source-specific flags in one place. The Go control plane
// owns the FFmpeg lifecycle; callers only supply a source and a target URL.
func ffmpegArgs(source BroadcastSource, target string) []string {
	args := []string{"-hide_banner", "-loglevel", "warning"}

	switch source.Type {
	case Camera:
		input := stringValue(source.Path)
		if input == "" {
			input = "0:0"
		}
		args = append(args,
			"-f", "avfoundation",
			"-framerate", "30",
			"-i", input,
			"-c:v", "libx264",
			"-preset", "veryfast",
			"-tune", "zerolatency",
			"-pix_fmt", "yuv420p",
			"-c:a", "aac",
			"-f", "flv",
			target,
		)
	case File:
		if boolValue(source.Loop) {
			args = append(args, "-stream_loop", "-1")
		}
		args = append(args,
			"-re",
			"-i", stringValue(source.Path),
			"-c", "copy",
			"-f", "flv",
			target,
		)
	}

	return args
}

func stopProcess(process Process, timeout time.Duration) {
	if process == nil {
		return
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		log.Printf("stop ffmpeg: %v", err)
	}

	select {
	case <-process.Done():
		return
	case <-time.After(timeout):
		log.Printf("ffmpeg did not stop in %s, killing it", timeout)
	}

	if err := process.Kill(); err != nil {
		log.Printf("kill ffmpeg: %v", err)
	}

	select {
	case <-process.Done():
	case <-time.After(timeout):
		log.Printf("ffmpeg did not exit after kill")
	}
}
