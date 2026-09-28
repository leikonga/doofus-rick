package selbst

import (
	"fmt"
	"runtime/metrics"
	"time"
)

const (
	goroutinesMetric = "/sched/goroutines:goroutines"
	heapMetric       = "/memory/classes/heap/objects:bytes"
	gcCyclesMetric   = "/gc/cycles/total:gc-cycles"
)

type vitals struct {
	uptime     time.Duration
	goroutines uint64
	heapBytes  uint64
	gcCycles   uint64
	deploy     string
}

func Vitals(now time.Time, deploy string) string {
	samples := []metrics.Sample{{Name: goroutinesMetric}, {Name: heapMetric}, {Name: gcCyclesMetric}}
	metrics.Read(samples)
	return vitals{
		uptime:     now.Sub(bootTime),
		goroutines: uint64Value(samples[0]),
		heapBytes:  uint64Value(samples[1]),
		gcCycles:   uint64Value(samples[2]),
		deploy:     deploy,
	}.String()
}

func uint64Value(s metrics.Sample) uint64 {
	if s.Value.Kind() != metrics.KindUint64 {
		return 0
	}
	return s.Value.Uint64()
}

func (v vitals) String() string {
	return fmt.Sprintf("<vitals>uptime=%s goroutines=%d heap_mb=%d gc_cycles=%d deploy=%s</vitals>",
		formatUptime(v.uptime), v.goroutines, v.heapBytes>>20, v.gcCycles, v.deploy)
}

func formatUptime(d time.Duration) string {
	minutes := int(d / time.Minute)
	days, hours, mins := minutes/(24*60), minutes/60%24, minutes%60
	if days > 0 {
		return fmt.Sprintf("%dd%dh%dm", days, hours, mins)
	}
	return fmt.Sprintf("%dh%dm", hours, mins)
}
