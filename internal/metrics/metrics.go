package metrics

import (
	"context"
	"os"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
	"github.com/shirou/gopsutil/net"
	"github.com/shirou/gopsutil/process"
)

type Metrics struct {
	ProcessCPU       ProcessCPU
	CPU              map[string]CPU
	NIC              map[string]NIC
	Memory           Memory
	CPUPercent       float64
	GarbageCollector GarbageCollector
}

type GarbageCollector struct {
	NumGC uint64
}

type ProcessCPU struct {
	User   float64
	System float64
}

type CPU struct {
	User   float64
	System float64
	Usage  float64
	Total  float64
}

type NIC struct {
	BytesReceived uint64
	BytesSent     uint64
}

type Memory struct {
	Available uint64
	Used      uint64
	HeapAlloc uint64
}

func Measure(ctx context.Context, interval time.Duration) (Metrics, error) {
	p, err := process.NewProcess(int32(os.Getpid())) // TODO: cache the process
	if err != nil {
		return Metrics{}, err
	}

	processTimes, err := p.TimesWithContext(ctx) // returns user and system time for process
	if err != nil {
		return Metrics{}, err
	}

	systemTimes, err := cpu.TimesWithContext(ctx, false)
	if err != nil {
		return Metrics{}, err
	}

	percentages, err := cpu.PercentWithContext(ctx, interval, false)
	if err != nil {
		return Metrics{}, err
	}

	netStats, err := net.IOCountersWithContext(ctx, false)
	if err != nil {
		return Metrics{}, err
	}

	var rtm runtime.MemStats
	runtime.ReadMemStats(&rtm)
	gc := GarbageCollector{
		NumGC: uint64(rtm.NumGC),
	}

	memStats, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return Metrics{}, err
	}

	metrics := Metrics{
		ProcessCPU: ProcessCPU{
			User:   processTimes.User,
			System: processTimes.System,
		},
		CPU: make(map[string]CPU),
		NIC: make(map[string]NIC),
		Memory: Memory{
			Available: memStats.Available,
			Used:      memStats.Used,
			HeapAlloc: rtm.HeapAlloc,
		},
		GarbageCollector: gc,
	}

	for _, t := range systemTimes {
		usage := t.User + t.System + t.Nice + t.Iowait + t.Irq + t.Softirq + t.Steal
		metrics.CPU[t.CPU] = CPU{
			User:   t.User,
			System: t.System,
			Usage:  usage,
			Total:  usage + t.Idle,
		}
	}

	for _, p := range percentages {
		metrics.CPUPercent = p
	}

	for _, counters := range netStats {
		metrics.NIC[counters.Name] = NIC{
			BytesReceived: counters.BytesRecv,
			BytesSent:     counters.BytesSent,
		}
	}

	return metrics, nil
}
