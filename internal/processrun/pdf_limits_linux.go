//go:build linux

package processrun

import (
	"errors"
	"golang.org/x/sys/unix"
	"runtime/debug"
)

const (
	PDFImportMemory   int64 = 512 << 20
	PDFAssemblyMemory int64 = 1280 << 20
)

// ApplyPDFLimits installs a fixed parser profile for an internal PDF child.
// It changes the entire calling process; never call from an API or worker.
func ApplyPDFLimits(outputBytes, dataBytes int64) error {
	if outputBytes < 1 || outputBytes > 2<<30 || (dataBytes != PDFImportMemory && dataBytes != PDFAssemblyMemory) {
		return errors.New("invalid PDF child output bound")
	}
	addressBytes := uint64(2 << 30)
	if dataBytes == PDFAssemblyMemory {
		addressBytes = 3 << 30 // Go reserves heap arenas in addition to live raster/PDF data.
	}
	for resource, limit := range map[int]unix.Rlimit{
		unix.RLIMIT_CORE:   {Cur: 0, Max: 0},
		unix.RLIMIT_CPU:    {Cur: 30, Max: 31},
		unix.RLIMIT_DATA:   {Cur: uint64(dataBytes), Max: uint64(dataBytes)},
		unix.RLIMIT_AS:     {Cur: addressBytes, Max: addressBytes},
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
	softMemory := dataBytes / 2
	if dataBytes == PDFAssemblyMemory {
		// A 600dpi bitmap and its PDF representation can be live together.
		softMemory = 768 << 20
	}
	debug.SetMemoryLimit(softMemory)
	debug.SetMaxThreads(16)
	return nil
}
