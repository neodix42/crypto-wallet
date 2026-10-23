package stress

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type latency struct {
	Count           int   `json:"count"`
	P50Micros       int64 `json:"p50Micros"`
	P99Micros       int64 `json:"p99Micros"`
	MaxMicros       int64 `json:"maxMicros"`
	BudgetP99Micros int64 `json:"budgetP99Micros"`
}

func summarize(values []int64, budget int64) latency {
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	if len(values) == 0 {
		return latency{BudgetP99Micros: budget}
	}
	return latency{Count: len(values), P50Micros: values[len(values)/2], P99Micros: values[len(values)*99/100], MaxMicros: values[len(values)-1], BudgetP99Micros: budget}
}
func rss() (uint64, uint64, error) {
	b, e := os.ReadFile("/proc/self/status")
	if e != nil {
		return 0, 0, e
	}
	var current, high uint64
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if fields[0] == "VmRSS:" || fields[0] == "VmHWM:" {
			n, e := strconv.ParseUint(fields[1], 10, 64)
			if e != nil || fields[2] != "kB" {
				return 0, 0, fmt.Errorf("invalid proc resource units")
			}
			if fields[0] == "VmRSS:" {
				current = n * 1024
			} else {
				high = n * 1024
			}
		}
	}
	if current == 0 || high < current {
		return 0, 0, fmt.Errorf("missing kernel RSS/HWM measurements")
	}
	return current, high, nil
}

type resourceMonitor struct {
	mu                      sync.Mutex
	start, peak, end, hwm   uint64
	samples, peakGoroutines int
	err                     error
	stop                    chan struct{}
	done                    chan struct{}
}

func watchResources() *resourceMonitor {
	m := &resourceMonitor{stop: make(chan struct{}), done: make(chan struct{})}
	m.sample()
	m.start = m.end
	go func() {
		defer close(m.done)
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-m.stop:
				m.sample()
				return
			case <-tick.C:
				m.sample()
			}
		}
	}()
	return m
}
func (m *resourceMonitor) sample() {
	current, high, e := rss()
	m.mu.Lock()
	defer m.mu.Unlock()
	if e != nil {
		m.err = e
		return
	}
	m.end = current
	if current > m.peak {
		m.peak = current
	}
	if high > m.hwm {
		m.hwm = high
	}
	m.samples++
	if n := runtime.NumGoroutine(); n > m.peakGoroutines {
		m.peakGoroutines = n
	}
}
func (m *resourceMonitor) finish(r *report) error {
	close(m.stop)
	<-m.done
	m.mu.Lock()
	defer m.mu.Unlock()
	r.StartRSSBytes = m.start
	r.EndRSSBytes = m.end
	r.PeakObservedRSSBytes = m.peak
	r.KernelHighWaterRSSBytes = m.hwm
	r.ResourceSamples = m.samples
	r.PeakGoroutines = m.peakGoroutines
	return m.err
}
