package agent

import (
	"cmp"

	"golang.org/x/sys/unix"
)

// The sysctl calls return nil with their error; cmp.Or swaps in a zero
// value, so a failed call reads as a zero start or boot time.

func procStartTime(pid int) (procStart, error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	kp = cmp.Or(kp, new(unix.KinfoProc))
	return procStart{Sec: kp.Proc.P_starttime.Sec, Usec: int64(kp.Proc.P_starttime.Usec)}, err
}

func bootTime() (int64, error) {
	tv, err := unix.SysctlTimeval("kern.boottime")
	return cmp.Or(tv, new(unix.Timeval)).Sec, err
}
