package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// startGroup starts a command as the leader of its own process group, as
// claude runs, and reaps it in the background.
func startGroup(t *testing.T, name string, args ...string) (*exec.Cmd, <-chan struct{}) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	return cmd, exited
}

func writeRecord(t *testing.T, r *Runner, rec procRecord) string {
	t.Helper()
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	path := r.procRecordPath(rec.PID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// goneServer marks a record as left by a server that no longer runs: the
// pid is this test's, but the start time is not.
func goneServer(rec procRecord) procRecord {
	rec.ServerPID, rec.ServerStart = os.Getpid(), procStart{Sec: 1}
	return rec
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestSweepStopsTheGroupOfAClaudeProcessThatAGoneServerLeft(t *testing.T) {
	t.Parallel()
	r := newHarness(t, "reply").runner
	cmd, exited := startGroup(t, "sleep", "60")
	rec, err := newProcRecord(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	path := writeRecord(t, r, goneServer(rec))
	r.stopLeftoverProcesses()
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("the leftover process still runs after the sweep")
	}
	if exists(path) {
		t.Fatal("the record of the stopped process is still there")
	}
}

func TestSweepStopsAGroupWhoseLeaderExited(t *testing.T) {
	t.Parallel()
	r := newHarness(t, "reply").runner
	cmd := exec.CommandContext(t.Context(), "sh", "-c", "sleep 60 >/dev/null 2>&1 & echo $!")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	pgid := cmd.Process.Pid
	t.Cleanup(func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })
	child, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatal(err)
	}
	boot, err := bootTime()
	if err != nil {
		t.Fatal(err)
	}
	// The leader has exited and was reaped; its background task is left.
	path := writeRecord(t, r, goneServer(procRecord{PID: pgid, Start: procStart{Sec: 7}, Boot: boot}))
	r.stopLeftoverProcesses()
	waitFor(t, "the leader's background task ends", func() bool { return syscall.Kill(child, 0) != nil })
	if exists(path) {
		t.Fatal("the record is still there")
	}
}

func TestSweepLeavesProcessesThatAreNotLeftovers(t *testing.T) {
	t.Parallel()
	r := newHarness(t, "reply").runner
	boot, err := bootTime()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		rec  func(rec procRecord) procRecord
		kept bool
	}{
		{"its server still runs", func(rec procRecord) procRecord { return rec }, true},
		{"the pid has another start time", func(rec procRecord) procRecord { rec = goneServer(rec); rec.Start.Sec++; return rec }, false},
		{"an earlier boot", func(rec procRecord) procRecord { rec = goneServer(rec); rec.Boot = boot - 1; return rec }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd, exited := startGroup(t, "sleep", "60")
			rec, err := newProcRecord(cmd.Process.Pid)
			if err != nil {
				t.Fatal(err)
			}
			path := writeRecord(t, r, tc.rec(rec))
			r.sweepRecord(path)
			select {
			case <-exited:
				t.Fatal("the sweep stopped a process that is not a leftover")
			case <-time.After(300 * time.Millisecond):
			}
			if exists(path) != tc.kept {
				t.Fatalf("record kept = %v; want %v", exists(path), tc.kept)
			}
		})
	}
}

func TestSweepNeverSignalsThisServerOrPidOne(t *testing.T) {
	t.Parallel()
	boot, err := bootTime()
	if err != nil {
		t.Fatal(err)
	}
	self, err := procStartTime(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	for _, pid := range []int{0, 1, os.Getpid(), syscall.Getpgrp()} {
		start := self
		if pid > 0 {
			// With the pid's own start time, only the guard tells it apart.
			if s, err := procStartTime(pid); err == nil {
				start = s
			}
		}
		rec := goneServer(procRecord{PID: pid, Start: start, Boot: boot})
		if got := leftoverAction(rec); got != dropRecord {
			t.Errorf("leftoverAction for pid %d = %v; want dropRecord", pid, got)
		}
	}
	gone := goneServer(procRecord{PID: 999999999, Boot: boot})
	if got := leftoverAction(gone); got != dropRecord {
		t.Errorf("leftoverAction for a pid with no process or group = %v; want dropRecord", got)
	}
}

func TestClaudeProcessIsRecordedWhileItRunsAndForgottenAfter(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "sleep")
	bot := h.newBot(t)
	h.start(t)
	if _, _, err := h.svc.SubmitUserMessage(context.Background(), bot.ID, "work", "n1"); err != nil {
		t.Fatal(err)
	}
	records := func() []string {
		paths, _ := filepath.Glob(filepath.Join(h.runner.runDir(), "claude-*.json"))
		return paths
	}
	waitFor(t, "the running process is recorded", func() bool { return len(records()) == 1 })
	data, err := os.ReadFile(records()[0])
	if err != nil {
		t.Fatal(err)
	}
	var rec procRecord
	if err := json.Unmarshal(data, &rec); err != nil || rec.ServerPID != os.Getpid() || rec.PID <= 1 || rec.Boot == 0 {
		t.Fatalf("record = %s, %v; want the process, this server and the boot", data, err)
	}
	if _, err := h.runner.Interrupt(context.Background(), bot.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the record goes with the process", func() bool { return len(records()) == 0 && !h.runner.Active(bot.ID) })
}

func TestParseProcStatAndBootTime(t *testing.T) {
	t.Parallel()
	// Exactly the fields up to the start time.
	stat := "4242 (my (odd) cmd) S 1 4242 4242 0 -1 4194560 100 0 0 0 1 2 0 0 20 0 1 0 987654"
	if got, err := parseProcStat(stat, nil); err != nil || got != (procStart{Sec: 987654}) {
		t.Errorf("parseProcStat = %+v, %v; want start 987654", got, err)
	}
	for _, bad := range []string{"", "1 (x) S 1 2", "1 (x) S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 x"} {
		if _, err := parseProcStat(bad, nil); err == nil {
			t.Errorf("parseProcStat(%q) succeeded; want an error", bad)
		}
	}
	if _, err := parseProcStat(stat, os.ErrNotExist); err == nil {
		t.Error("parseProcStat with a read error succeeded")
	}
	if got, err := parseBootTime("cpu 1 2 3\nbtime 1762600000\nprocesses 9\n", nil); err != nil || got != 1762600000 {
		t.Errorf("parseBootTime = %d, %v; want 1762600000", got, err)
	}
	if _, err := parseBootTime("cpu 1 2 3\n", nil); err == nil {
		t.Error("parseBootTime without btime succeeded")
	}
	if _, err := parseBootTime("", os.ErrNotExist); err == nil {
		t.Error("parseBootTime with a read error succeeded")
	}
}

// groupWithChild starts sh as a group leader with a sleeping child and
// returns the leader's and the child's pids. With ignoreTerm both ignore
// SIGTERM, which a child inherits.
func groupWithChild(t *testing.T, ignoreTerm bool) (int, int, <-chan struct{}) {
	t.Helper()
	script := "sleep 60 >/dev/null 2>&1 & echo $!; wait"
	if ignoreTerm {
		script = "trap '' TERM; " + script
	}
	cmd := exec.CommandContext(t.Context(), "sh", "-c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pgid := cmd.Process.Pid
	t.Cleanup(func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })
	var line [32]byte
	n, err := out.Read(line[:])
	if err != nil {
		t.Fatal(err)
	}
	child, err := strconv.Atoi(strings.TrimSpace(string(line[:n])))
	if err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	return pgid, child, exited
}

func TestStopProcessGroupEndsTheWholeGroup(t *testing.T) {
	t.Parallel()
	pgid, child, exited := groupWithChild(t, false)
	start := time.Now()
	stopProcessGroup(pgid)
	if took := time.Since(start); took >= groupStopWait {
		t.Fatalf("a group that takes SIGTERM stopped after %v; want before the SIGKILL wait", took)
	}
	<-exited
	waitFor(t, "the child ends", func() bool { return syscall.Kill(child, 0) != nil })
}

func TestStopProcessGroupKillsAGroupThatIgnoresSIGTERM(t *testing.T) {
	t.Parallel()
	pgid, child, exited := groupWithChild(t, true)
	start := time.Now()
	stopProcessGroup(pgid)
	if took := time.Since(start); took < groupStopWait {
		t.Fatalf("stopProcessGroup returned after %v while the group ignored SIGTERM; want it to wait, then SIGKILL", took)
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("the leader still runs after SIGKILL")
	}
	waitFor(t, "the child ends", func() bool { return syscall.Kill(child, 0) != nil })
}

func TestRecordFailuresAreLoggedAndSuccessIsNot(t *testing.T) {
	t.Parallel()
	r := newHarness(t, "reply").runner
	var logs strings.Builder
	r.log = slog.New(slog.NewTextHandler(&logs, nil))
	r.recordProcess(os.Getpid())
	r.forgetProcess(os.Getpid())
	r.forgetProcess(os.Getpid()) // already gone
	if logs.Len() != 0 {
		t.Fatalf("recording and forgetting a process logged: %s", logs.String())
	}
	if err := os.MkdirAll(filepath.Join(r.procRecordPath(os.Getpid()), "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	r.recordProcess(os.Getpid())
	r.forgetProcess(os.Getpid())
	for _, want := range []string{"record claude process", "remove claude process record"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("logs = %s; want %q", logs.String(), want)
		}
	}
}

func TestStopProcessGroupKillsWhatALeaderThatExitedLeft(t *testing.T) {
	t.Parallel()
	cmd := exec.CommandContext(t.Context(), "sh", "-c", "trap '' TERM; sleep 60 >/dev/null 2>&1 & echo $!")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	pgid := cmd.Process.Pid
	t.Cleanup(func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })
	child, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatal(err)
	}
	stopProcessGroup(pgid)
	waitFor(t, "the child that ignores SIGTERM ends", func() bool { return syscall.Kill(child, 0) != nil })
}
