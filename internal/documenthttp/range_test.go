package documenthttp

import "testing"

func TestSingleByteRanges(t *testing.T) {
	for _, test := range []struct {
		raw                 string
		size, start, length int64
		valid               bool
	}{
		{"bytes=0-3", 10, 0, 4, true}, {"bytes=5-", 10, 5, 5, true}, {"bytes=-3", 10, 7, 3, true},
		{"bytes=8-1000", 10, 8, 2, true}, {"bytes=-1000", 10, 0, 10, true}, {"bytes=0-18446744073709551615", 10, 0, 10, true},
		{"bytes=-18446744073709551615", 10, 0, 10, true}, {"bytes=9-9", 10, 9, 1, true},
		{"bytes=10-", 10, 0, 0, false}, {"bytes=3-2", 10, 0, 0, false}, {"bytes=-0", 10, 0, 0, false},
		{"bytes=0-1,4-5", 10, 0, 0, false}, {"items=0-1", 10, 0, 0, false}, {"bytes=", 10, 0, 0, false},
		{"bytes=+1-2", 10, 0, 0, false}, {"bytes= 1-2", 10, 0, 0, false}, {"bytes=1-2 ", 10, 0, 0, false},
		{"bytes=1--2", 10, 0, 0, false}, {"bytes=0-", 0, 0, 0, false}, {"bytes=-3", 0, 0, 0, false},
		{"bytes=18446744073709551616-", 10, 0, 0, false},
	} {
		t.Run(test.raw, func(t *testing.T) {
			start, length, err := byteRange(test.raw, test.size)
			if (err == nil) != test.valid || test.valid && (start != test.start || length != test.length) {
				t.Fatal(start, length, err)
			}
		})
	}
}
