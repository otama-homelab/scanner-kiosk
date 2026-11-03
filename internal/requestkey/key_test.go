package requestkey

import (
	"errors"
	"fmt"
	"testing"
)

func TestKeyAgeBoundariesAndClockFence(t *testing.T) {
	key := fmt.Sprintf("rq1_%013d_0123456789abcdef0123456789abcdef", 1000)
	for _, test := range []struct {
		now, floor int64
		want       error
	}{{1000, 0, nil}, {1000 + Lifetime.Milliseconds() - 1, 0, nil}, {1000 + Lifetime.Milliseconds(), 0, ErrExpired}, {1000, 1001, ErrClock}, {999, 0, nil}, {0, 0, nil}} {
		expiry, aged, err := ValidateNew(key, test.now, test.floor)
		if !aged || !errors.Is(err, test.want) || err == nil && expiry != 1000+Lifetime.Milliseconds() {
			t.Fatal(test, expiry, aged, err)
		}
	}
	if _, _, err := ValidateNew(fmt.Sprintf("rq1_%013d_0123456789abcdef0123456789abcdef", FutureSkew.Milliseconds()+1), 0, 0); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	for _, bad := range []string{"rq1_bad", "rq1_0000000001000_0123456789ABCDEF0123456789abcdef", "rq1_0000000001000_0123456789abcdef0123456789abcde_"} {
		if _, aged, err := Expiry(bad); !aged || !errors.Is(err, ErrInvalid) {
			t.Fatal("malformed versioned key", bad, err)
		}
	}
	if _, aged, err := ValidateNew("legacy_request_0001", 0, 0); aged || err != nil {
		t.Fatal("legacy key silently aged", err)
	}
}
