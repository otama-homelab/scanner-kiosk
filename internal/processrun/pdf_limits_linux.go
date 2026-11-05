//go:build linux

package processrun

import (
	"errors"
	"golang.org/x/sys/unix"
	"runtime/debug"
)

// ApplyPDFLimits installs the fixed parser profile for an internal PDF child.
// It changes the entire calling process; never call from an API or worker.
func ApplyPDFLimits(outputBytes int64) error {
	if outputBytes < 1 || outputBytes > 2<<30 {
		return errors.New("invalid PDF child output bound")
	}
	for resource, limit := range map[int]unix.Rlimit{
		unix.RLIMIT_CORE:   {Cur: 0, Max: 0},
		unix.RLIMIT_CPU:    {Cur: 30, Max: 31},
		unix.RLIMIT_DATA:   {Cur: 512 << 20, Max: 512 << 20},
		unix.RLIMIT_AS:     {Cur: 2 << 30, Max: 2 << 30},
		unix.RLIMIT_FSIZE:  {Cur: uint64(outputBytes), Max: uint64(outputBytes)},
		unix.RLIMIT_NOFILE: {Cur: 32, Max: 32},
	} {
		if err := unix.Setrlimit(resource, &limit); err != nil {
			return err
		}
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return err
	}
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return err
	}
	debug.SetMemoryLimit(256 << 20)
	debug.SetMaxThreads(16)
	return nil
}
