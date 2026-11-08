package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// procStart identifies one process across pid reuse: when it started, as
// the system reports it (seconds and microseconds on macOS, clock ticks
// since boot on Linux).
type procStart struct {
	Sec  int64 `json:"sec"`
	Usec int64 `json:"usec"`
}

// procRecord is the file that a claude process leaves in run/ while it runs.
// After a crash the next server finds it and ends the process's group.
type procRecord struct {
	PID         int       `json:"pid"` // also the group id: claude runs with Setpgid
	Start       procStart `json:"start"`
	Boot        int64     `json:"boot"` // when the system booted, in seconds
	ServerPID   int       `json:"serverPid"`
	ServerStart procStart `json:"serverStart"`
}

// groupStopWait is how long the sweep waits after SIGTERM before SIGKILL.
const groupStopWait time.Duration = 2e9 // 2 s, in nanoseconds

func (r *Runner) procRecordPath(pid int) string {
	return filepath.Join(r.runDir(), "claude-"+strconv.Itoa(pid)+".json")
}

// recordProcess writes the record of a claude process that just started.
// It writes a temp file and renames it, so a crash leaves no partial record.
func (r *Runner) recordProcess(pid int) {
	if err := r.writeProcRecord(pid); err != nil {
		r.log.Warn("record claude process", "pid", pid, "err", err)
	}
}

func (r *Runner) writeProcRecord(pid int) error {
	rec, err := newProcRecord(pid)
	if err != nil {
		return err
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	path := r.procRecordPath(pid)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path+".tmp", data, 0o600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

func newProcRecord(pid int) (procRecord, error) {
	start, err := procStartTime(pid)
	if err != nil {
		return procRecord{}, err
	}
	boot, err := bootTime()
	if err != nil {
		return procRecord{}, err
	}
	server, err := procStartTime(os.Getpid())
	if err != nil {
		return procRecord{}, err
	}
	return procRecord{PID: pid, Start: start, Boot: boot, ServerPID: os.Getpid(), ServerStart: server}, nil
}

// forgetProcess removes the record of a claude process that has exited.
func (r *Runner) forgetProcess(pid int) { r.removeRecord(r.procRecordPath(pid)) }

// stopLeftoverProcesses ends the process groups of claude processes that a
// server which is gone left running, for example after a crash or a hard
// kill. It runs at startup, before this server starts any process.
func (r *Runner) stopLeftoverProcesses() {
	paths, err := filepath.Glob(filepath.Join(r.runDir(), "claude-*.json"))
	if err != nil {
		r.log.Warn("find claude process records", "err", err)
		return
	}
	for _, path := range paths {
		r.sweepRecord(path)
	}
}

func (r *Runner) sweepRecord(path string) {
	var rec procRecord
	data, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(data, &rec)
	}
	if err != nil {
		r.log.Warn("read claude process record", "path", path, "err", err)
		r.removeRecord(path)
		return
	}
	switch leftoverAction(rec) {
	case keepRecord:
		return
	case stopGroup:
		r.log.Warn("stop claude process left by an earlier server", "pid", rec.PID)
		stopProcessGroup(rec.PID)
	}
	r.removeRecord(path)
}

func (r *Runner) removeRecord(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		r.log.Warn("remove claude process record", "path", path, "err", err)
	}
}

type sweepAction int

const (
	dropRecord sweepAction = iota // nothing of it runs, or it is not ours to signal
	keepRecord                    // its server still runs and owns it
	stopGroup                     // its server is gone and its group is left
)

// leftoverAction decides what the sweep does with a record. It signals only
// a group that this boot's dead server started: never pid 1, this server or
// its group, and never a reused pid.
func leftoverAction(rec procRecord) sweepAction {
	if rec.PID <= 1 || rec.PID == os.Getpid() || rec.PID == syscall.Getpgrp() {
		return dropRecord
	}
	if boot, err := bootTime(); err != nil || boot != rec.Boot {
		return dropRecord
	}
	if start, err := procStartTime(rec.ServerPID); err == nil && start == rec.ServerStart {
		return keepRecord
	}
	start, err := procStartTime(rec.PID)
	if err == nil && start != rec.Start {
		return dropRecord // another process has the pid now
	}
	// While a group with this id lives, its id cannot go to a new process,
	// so a live group whose leader exited is still the recorded one.
	if err == nil || syscall.Kill(-rec.PID, 0) == nil {
		return stopGroup
	}
	return dropRecord
}

// stopProcessGroup sends SIGTERM to the group, and SIGKILL if it is still
// there after groupStopWait.
func stopProcessGroup(pgid int) {
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	deadline := time.Now().Add(groupStopWait)
	for time.Now().Before(deadline) {
		if syscall.Kill(-pgid, 0) != nil {
			return
		}
		time.Sleep(groupPoll)
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}

// groupPoll is how often stopProcessGroup checks whether the group is gone.
const groupPoll time.Duration = 50e6 // 50 ms, in nanoseconds

// parseProcStat reads the start time, field 22, from /proc/<pid>/stat. The
// command name in field 2 can hold spaces and parentheses, so the fields
// are counted after its last ")".
func parseProcStat(stat string, err error) (procStart, error) {
	if err != nil {
		return procStart{}, err
	}
	_, rest, ok := strings.Cut(stat[strings.LastIndexByte(stat, ')')+1:], " ")
	fields := strings.Fields(rest)
	if !ok || len(fields) < 20 {
		return procStart{}, fmt.Errorf("unexpected /proc stat %q", stat)
	}
	ticks, err := strconv.ParseInt(fields[19], 10, 64)
	return procStart{Sec: ticks}, err
}

// parseBootTime reads the btime line of /proc/stat.
func parseBootTime(stat string, err error) (int64, error) {
	if err != nil {
		return 0, err
	}
	for line := range strings.Lines(stat) {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			return strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		}
	}
	return 0, errors.New("no btime in /proc/stat")
}
