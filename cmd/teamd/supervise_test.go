package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestSupervisedChild is the child the supervisor tests start: this test
// binary again, told by the environment what to be. Run any other way it
// does nothing.
func TestSupervisedChild(t *testing.T) {
	switch os.Getenv("TEAMD_TEST_CHILD") {
	case "exit":
		// a portal that dies, as one with no admin key does
		os.Exit(3)
	case "hang":
		// a portal that runs until it is told to stop, and says it was
		// told: the exit code is how the test knows it got SIGTERM and was
		// not killed
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGTERM)
		os.Stdout.WriteString("up\n")
		<-stop
		os.Exit(7)
	}
}

// children counts the children a supervisor starts and keeps each one, so
// a test can wait for a start and read how each ended.
type children struct {
	mode string
	mu   sync.Mutex
	cmds []*exec.Cmd
	up   chan struct{} // a "hang" child has its handler in place
	more chan struct{} // one more child was started
}

func newChildren(mode string) *children {
	return &children{mode: mode, up: make(chan struct{}, 16), more: make(chan struct{}, 16)}
}

func (c *children) start() *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestSupervisedChild$")
	cmd.Env = append(os.Environ(), "TEAMD_TEST_CHILD="+c.mode)
	cmd.Stdout = signalWriter{c.up}
	c.mu.Lock()
	c.cmds = append(c.cmds, cmd)
	c.mu.Unlock()
	// never blocking: the supervisor goes on starting children after the
	// test has seen the ones it waits for
	select {
	case c.more <- struct{}{}:
	default:
	}
	return cmd
}

func (c *children) all() []*exec.Cmd {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*exec.Cmd(nil), c.cmds...)
}

// signalWriter says on its channel when the child wrote "up".
type signalWriter struct{ up chan struct{} }

func (w signalWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("up")) {
		select {
		case w.up <- struct{}{}:
		default:
		}
	}
	return len(p), nil
}

// supervising runs s until the test ends, and waits for it to return, so
// no child outlives the test.
func supervising(t *testing.T, s supervisor) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.run(ctx)
	}()
	stop = func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Minute):
			t.Fatal("the supervisor didn't return a minute after it was told to stop")
		}
	}
	t.Cleanup(stop)
	return stop
}

func waitFor(t *testing.T, ch chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Minute):
		t.Fatalf("no %s within a minute", what)
	}
}

// TestSupervisorStartsTheChildAgain: a portal that dies is started again,
// each time, and the supervisor itself goes on. This is what keeps the
// container up, and the Pod's gateway with it, while teamd can't run.
func TestSupervisorStartsTheChildAgain(t *testing.T) {
	c := newChildren("exit")
	var log bytes.Buffer
	var logMu sync.Mutex
	stop := supervising(t, supervisor{
		start: c.start, log: lockedWriter{&logMu, &log},
		first: time.Millisecond, most: 5 * time.Millisecond, steady: time.Hour, grace: time.Second,
	})
	for i := 0; i < 3; i++ {
		waitFor(t, c.more, "start of the child")
	}
	stop()
	logMu.Lock()
	defer logMu.Unlock()
	if !strings.Contains(log.String(), "exit status 3") {
		t.Errorf("the log doesn't say how the child ended:\n%s", log.String())
	}
}

// TestSupervisorStopsItsChild: told to stop, the supervisor asks its child
// to stop the way Kubernetes asks a container (SIGTERM, so the portal
// finishes what it is answering), waits for it, and starts no other.
func TestSupervisorStopsItsChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no SIGTERM to send; the supervisor kills the child after its grace there")
	}
	c := newChildren("hang")
	stop := supervising(t, supervisor{
		start: c.start, log: &bytes.Buffer{},
		first: time.Millisecond, most: time.Millisecond, steady: time.Hour, grace: time.Minute,
	})
	waitFor(t, c.up, "child with its SIGTERM handler")
	stop()
	cmds := c.all()
	if len(cmds) != 1 {
		t.Fatalf("%d children were started, want 1: a child stopped on purpose is not started again", len(cmds))
	}
	st := cmds[0].ProcessState
	if st == nil {
		t.Fatal("the supervisor returned with its child still running")
	}
	if st.ExitCode() != 7 {
		t.Errorf("the child ended %v, want exit 7: it was killed rather than asked to stop", st)
	}
}

// TestSupervisorBacksOff: a portal that dies at once is started again
// after a wait that doubles to a cap, so a teamd that can't start costs a
// line a cap's time in the log, not a loop; one that ran a while before it
// died starts from the shortest wait again.
func TestSupervisorBacksOff(t *testing.T) {
	s := supervisor{first: time.Second, most: 8 * time.Second, steady: time.Minute}
	var got []time.Duration
	var wait time.Duration
	for i := 0; i < 6; i++ {
		wait = s.after(wait, time.Millisecond)
		got = append(got, wait)
	}
	want := []time.Duration{1, 2, 4, 8, 8, 8}
	for i := range want {
		if got[i] != want[i]*time.Second {
			t.Fatalf("waits after quick deaths were %v, want %v seconds", got, want)
		}
	}
	if w := s.after(wait, 2*time.Minute); w != time.Second {
		t.Errorf("a child that ran two minutes is started again after %s, want the first wait", w)
	}
}

type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
