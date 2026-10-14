//go:build linux

package collector

import (
	"encoding/binary"
	"os"
	"strconv"
	"strings"
	"sync"
)

// AT_CLKTCK is the same userspace clock frequency used by sysconf(_SC_CLK_TCK).
// Read it once from our own auxiliary vector, without CGo or a subprocess.
var clockTicks = sync.OnceValue(func() uint64 {
	data, err := os.ReadFile("/proc/self/auxv")
	if err != nil {
		return 0
	}
	return auxClockTicks(data, strconv.IntSize/8, binary.NativeEndian)
})

func auxClockTicks(data []byte, wordSize int, order binary.ByteOrder) uint64 {
	if (wordSize != 4 && wordSize != 8) || len(data)%(2*wordSize) != 0 {
		return 0
	}
	word := func(b []byte) uint64 {
		if wordSize == 4 {
			return uint64(order.Uint32(b))
		}
		return order.Uint64(b)
	}
	for offset := 0; offset < len(data); offset += 2 * wordSize {
		kind := word(data[offset:])
		if kind == 0 { // AT_NULL
			break
		}
		if kind == 17 { // AT_CLKTCK
			return word(data[offset+wordSize:])
		}
	}
	return 0
}

func parseCPUTime(stat string, frequency uint64) (float64, bool) {
	_, rest, ok := strings.CutLast(stat, ")")
	fields := strings.Fields(rest)
	if !ok || len(fields) <= 12 || frequency == 0 {
		return 0, false
	}
	user, userErr := strconv.ParseUint(fields[11], 10, 64)     // field 14: utime
	system, systemErr := strconv.ParseUint(fields[12], 10, 64) // field 15: stime
	if userErr != nil || systemErr != nil {
		return 0, false
	}
	return (float64(user) + float64(system)) / float64(frequency), true
}
