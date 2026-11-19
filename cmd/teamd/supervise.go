package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

// supervisor keeps the portal running inside its container: it starts
// `teamd serve` as a child, and starts it again whenever it ends.
//
// teamd is a sidecar, and a Pod is Ready only while every container in it
// is. A teamd that exited — no admin key mounted yet, a panic, a kill —
// would leave its container restarting under Kubernetes' backoff, the Pod
// not Ready, and magpie's gateway out of its Service for as long. Under a
// supervisor the container is this process, which ends only when it is
// told to: the portal can be down while the gateway goes on serving.
type supervisor struct {
	// start makes the child, fresh each time.
	start func() *exec.Cmd
	// log is where each end of the child, and the wait before the next
	// start, is said.
	log io.Writer
	// first and most bound the wait before a start: a child that dies at
	// once is started again after first, then twice as long each time, to
	// most.
	first, most time.Duration
	// steady is how long a child must have run for its end to be counted
	// as new trouble, starting from first again.
	steady time.Duration
	// grace is how long a child asked to stop has before it is killed.
	grace time.Duration
}

// run starts the child and starts it again until ctx is done; then it
// stops the child and returns.
func (s supervisor) run(ctx context.Context) {
	var wait time.Duration
	for {
		began := time.Now()
		err := s.once(ctx)
		if ctx.Err() != nil {
			return
		}
		wait = s.after(wait, time.Since(began))
		fmt.Fprintf(s.log, "teamd: the portal stopped (%v); starting it again in %s\n", err, wait)
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

// after is the wait before the next start, given the last wait and how
// long the child that just ended ran.
func (s supervisor) after(prev, ran time.Duration) time.Duration {
	if prev == 0 || ran >= s.steady {
		return s.first
	}
	return min(prev*2, s.most)
}

// once runs one child until it ends, or until ctx is done: then the child
// is asked to stop with SIGTERM, as Kubernetes asks a container, and
// killed if it hasn't within grace.
func (s supervisor) once(ctx context.Context) error {
	cmd := s.start()
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			// serve returns 0 only when it was told to stop, and this
			// supervisor didn't tell it
			return errors.New("it exited")
		}
		return err
	case <-ctx.Done():
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	t := time.NewTimer(s.grace)
	defer t.Stop()
	select {
	case <-done:
	case <-t.C:
		_ = cmd.Process.Kill()
		<-done
	}
	return ctx.Err()
}

// superviseServe runs `teamd serve` with args under a supervisor, until
// this process is told to stop.
func superviseServe(args []string, stderr io.Writer) int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(stderr, "teamd: can't find its own binary to supervise: %v\n", err)
		return 1
	}
	// The child takes the same flags, with -supervise turned off: the last
	// of a flag given twice is the one the flag package keeps.
	childArgs := append(append([]string{"serve"}, args...), "-supervise=false")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	s := supervisor{
		start: func() *exec.Cmd {
			cmd := exec.Command(exe, childArgs...)
			cmd.Stdout, cmd.Stderr = os.Stdout, stderr
			return cmd
		},
		log:    stderr,
		first:  time.Second,
		most:   30 * time.Second,
		steady: time.Minute,
		// the portal's own shutdown takes up to 10s; Kubernetes waits 30
		grace: 15 * time.Second,
	}
	s.run(ctx)
	return 0
}
