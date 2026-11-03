package documenthttp

import (
	"errors"
	"strconv"
	"strings"
)

var errRange = errors.New("invalid byte range")

// byteRange accepts a single bytes range and returns its bounded start/length.
// Values are unsigned until bounded by the trusted file size to avoid overflow.
func byteRange(raw string, size int64) (int64, int64, error) {
	if size <= 0 || len(raw) > 128 || !strings.HasPrefix(raw, "bytes=") {
		return 0, 0, errRange
	}
	fields := strings.Split(strings.TrimPrefix(raw, "bytes="), "-")
	if len(fields) != 2 {
		return 0, 0, errRange
	}
	number := func(s string) (uint64, error) {
		if s == "" {
			return 0, errRange
		}
		for _, c := range s {
			if c < '0' || c > '9' {
				return 0, errRange
			}
		}
		return strconv.ParseUint(s, 10, 64)
	}
	if fields[0] == "" {
		n, err := number(fields[1])
		if err != nil || n == 0 {
			return 0, 0, errRange
		}
		if n > uint64(size) {
			n = uint64(size)
		}
		return size - int64(n), int64(n), nil
	}
	first, err := number(fields[0])
	if err != nil || first >= uint64(size) {
		return 0, 0, errRange
	}
	last := uint64(size - 1)
	if fields[1] != "" {
		n, err := number(fields[1])
		if err != nil || n < first {
			return 0, 0, errRange
		}
		last = min(last, n)
	}
	return int64(first), int64(last - first + 1), nil
}
