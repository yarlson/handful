package agent

import (
	"fmt"
	"os"
)

func procStartTime(pid int) (procStart, error) {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	return parseProcStat(string(stat), err)
}

func bootTime() (int64, error) {
	stat, err := os.ReadFile("/proc/stat")
	return parseBootTime(string(stat), err)
}
