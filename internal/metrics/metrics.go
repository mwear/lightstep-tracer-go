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
	Idle   float64
	Steal  float64
	Nice   float64
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

	for _, m := range systemTimes {
		metrics.CPU[m.CPU] = CPU{
			User:   m.User,
			System: m.System,
			Idle:   m.Idle,
			Steal:  m.Steal,
			Nice:   m.Nice,
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
