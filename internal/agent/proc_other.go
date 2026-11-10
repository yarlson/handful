//go:build !darwin && !linux

package agent

import "errors"

// Other systems have no start times to check, so the sweep signals nothing.
func procStartTime(int) (procStart, error) { return procStart{}, errors.ErrUnsupported }

func bootTime() (int64, error) { return 0, errors.ErrUnsupported }
