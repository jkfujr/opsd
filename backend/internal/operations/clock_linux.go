package operations

import (
	"golang.org/x/sys/unix"
	"os"
	"strings"
)

func SystemClock() (string, int64, error) {
	b, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if e != nil {
		return "", 0, e
	}
	var ts unix.Timespec
	if e = unix.ClockGettime(unix.CLOCK_BOOTTIME, &ts); e != nil {
		return "", 0, e
	}
	return strings.TrimSpace(string(b)), ts.Nano(), nil
}
