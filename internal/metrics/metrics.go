// Package metrics samples CPU / memory data straight from the Windows API.
//
// The Python edition used psutil. Here the same numbers are produced by
// GetSystemTimes, NtQuerySystemInformation, GlobalMemoryStatusEx,
// GetPerformanceInfo and CallNtPowerInformation, which avoids the interpreter
// and the psutil extension module entirely.
package metrics

import (
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Columns is the fixed CSV column order. New fields may only be appended.
var Columns = []string{
	"timestamp_iso",
	"timestamp_unix_ms",
	"cpu_percent",
	"cpu_percent_cores",
	"cpu_freq_mhz",
	"mem_percent",
	"mem_used_bytes",
	"mem_available_bytes",
	"mem_total_bytes",
	"swap_percent",
	"swap_used_bytes",
	"swap_total_bytes",
}

// CoresSeparator joins the per-core percentages in the CSV.
const CoresSeparator = "|"

// Sample is one recorded row.
type Sample struct {
	TimeISO    string
	TimeUnixMs int64

	CPUPercent *float64
	Cores      []float64
	CPUFreqMHz *float64

	MemPercent   *float64
	MemUsed      *uint64
	MemAvailable *uint64
	MemTotal     *uint64

	SwapPercent *float64
	SwapUsed    *uint64
	SwapTotal   *uint64
}

// CPU returns the overall CPU usage, or nil when unavailable.
func (s *Sample) CPU() *float64 { return s.CPUPercent }

// Mem returns the memory usage percentage, or nil when unavailable.
func (s *Sample) Mem() *float64 { return s.MemPercent }

// Timestamp is the wall clock time of the sample.
func (s *Sample) Timestamp() time.Time { return time.UnixMilli(s.TimeUnixMs) }

// Row renders the sample exactly like the Python CSV writer.
func (s *Sample) Row() []string {
	row := make([]string, 0, len(Columns))
	row = append(row,
		s.TimeISO,
		strconv.FormatInt(s.TimeUnixMs, 10),
		formatFloat(s.CPUPercent),
		formatCores(s.Cores),
		formatFloat(s.CPUFreqMHz),
		formatFloat(s.MemPercent),
		formatUint(s.MemUsed),
		formatUint(s.MemAvailable),
		formatUint(s.MemTotal),
		formatFloat(s.SwapPercent),
		formatUint(s.SwapUsed),
		formatUint(s.SwapTotal),
	)
	return row
}

func formatFloat(value *float64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(*value, 'f', 1, 64)
}

func formatUint(value *uint64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatUint(*value, 10)
}

func formatCores(cores []float64) string {
	if len(cores) == 0 {
		return ""
	}
	parts := make([]string, len(cores))
	for i, v := range cores {
		parts[i] = strconv.FormatFloat(v, 'f', 1, 64)
	}
	return strings.Join(parts, CoresSeparator)
}

// ---- Win32 bindings -------------------------------------------------------

var (
	modkernel32 = windows.NewLazySystemDLL("kernel32.dll")
	modntdll    = windows.NewLazySystemDLL("ntdll.dll")
	modpowrprof = windows.NewLazySystemDLL("powrprof.dll")
	modpsapi    = windows.NewLazySystemDLL("psapi.dll")

	procGetSystemTimes           = modkernel32.NewProc("GetSystemTimes")
	procGlobalMemoryStatusEx     = modkernel32.NewProc("GlobalMemoryStatusEx")
	procGetPerformanceInfo       = modpsapi.NewProc("GetPerformanceInfo")
	procGetActiveProcessorCount  = modkernel32.NewProc("GetActiveProcessorCount")
	procNtQuerySystemInformation = modntdll.NewProc("NtQuerySystemInformation")
	procCallNtPowerInformation   = modpowrprof.NewProc("CallNtPowerInformation")
)

const (
	allProcessorGroups                    = 0xFFFF
	systemProcessorPerformanceInfo        = 8
	processorInformationPowerLevel        = 11
	errInsufficientBuffer          uint32 = 0xC0000023
)

type filetime struct {
	Low  uint32
	High uint32
}

func (f filetime) u64() uint64 { return uint64(f.High)<<32 | uint64(f.Low) }

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

type performanceInfo struct {
	CB                uint32
	_                 uint32
	CommitTotal       uintptr
	CommitLimit       uintptr
	CommitPeak        uintptr
	PhysicalTotal     uintptr
	PhysicalAvailable uintptr
	SystemCache       uintptr
	KernelTotal       uintptr
	KernelPaged       uintptr
	KernelNonpaged    uintptr
	PageSize          uintptr
	HandleCount       uint32
	ProcessCount      uint32
	ThreadCount       uint32
}

type processorPerformance struct {
	IdleTime       int64
	KernelTime     int64
	UserTime       int64
	DpcTime        int64
	InterruptTime  int64
	InterruptCount uint32
}

type processorPower struct {
	Number           uint32
	MaxMhz           uint32
	CurrentMhz       uint32
	MhzLimit         uint32
	MaxIdleState     uint32
	CurrentIdleState uint32
}

type cpuTimes struct {
	idle   uint64
	kernel uint64
	user   uint64
}

// Collector keeps the previous CPU counters so deltas can be computed.
// It is safe for concurrent use.
type Collector struct {
	mu        sync.Mutex
	prevTotal cpuTimes
	prevCores []cpuTimes
	coreCount int
}

// NewCollector returns a collector with an empty baseline.
func NewCollector() *Collector {
	return &Collector{}
}

// WarmUp primes the CPU counters. The first psutil-style reading is always
// meaningless because there is no previous sample, so it is discarded.
func (c *Collector) WarmUp() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readTotal()
	c.readCores()
}

// Collect reads every enabled metric. Individual failures only clear that
// metric; the rest of the row is still recorded.
func (c *Collector) Collect(enableCores, enableFreq, enableSwap bool) *Sample {
	now := time.Now()
	sample := &Sample{
		TimeISO:    now.Format("2006-01-02T15:04:05.000"),
		TimeUnixMs: now.UnixMilli(),
	}

	c.mu.Lock()
	sample.CPUPercent = c.readTotal()
	if enableCores {
		sample.Cores = c.readCores()
	}
	c.mu.Unlock()

	if enableFreq {
		sample.CPUFreqMHz = readFrequency()
	}
	sample.MemPercent, sample.MemUsed, sample.MemAvailable, sample.MemTotal = readMemory()
	if enableSwap {
		sample.SwapPercent, sample.SwapUsed, sample.SwapTotal = readSwap()
	}
	return sample
}

func (c *Collector) readTotal() *float64 {
	var idle, kernel, user filetime
	if r, _, _ := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	); r == 0 {
		return nil
	}
	cur := cpuTimes{idle: idle.u64(), kernel: kernel.u64(), user: user.u64()}
	prev := c.prevTotal
	c.prevTotal = cur
	if prev.kernel == 0 && prev.user == 0 && prev.idle == 0 {
		return ptrFloat(0)
	}
	totalDelta := float64((cur.kernel - prev.kernel) + (cur.user - prev.user))
	if totalDelta <= 0 {
		return ptrFloat(0)
	}
	busyDelta := totalDelta - float64(cur.idle-prev.idle)
	return ptrFloat(round1(100 * busyDelta / totalDelta))
}

func (c *Collector) readCores() []float64 {
	if c.coreCount == 0 {
		c.coreCount = coreCount()
	}
	if c.coreCount <= 0 {
		return nil
	}

	buf := make([]processorPerformance, c.coreCount)
	var returned uint32
	status, _, _ := procNtQuerySystemInformation.Call(
		systemProcessorPerformanceInfo,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf))*unsafe.Sizeof(buf[0]),
		uintptr(unsafe.Pointer(&returned)),
	)
	if uint32(status) == errInsufficientBuffer {
		return nil
	}
	if uint32(status) != 0 {
		return nil
	}

	count := int(returned) / int(unsafe.Sizeof(buf[0]))
	if count <= 0 || count > len(buf) {
		count = len(buf)
	}

	if cap(c.prevCores) < count {
		c.prevCores = make([]cpuTimes, count)
	}
	c.prevCores = c.prevCores[:count]

	values := make([]float64, count)
	for i := 0; i < count; i++ {
		cur := cpuTimes{
			idle:   uint64(buf[i].IdleTime),
			kernel: uint64(buf[i].KernelTime),
			user:   uint64(buf[i].UserTime),
		}
		prev := c.prevCores[i]
		c.prevCores[i] = cur
		total := float64((cur.kernel - prev.kernel) + (cur.user - prev.user))
		if prev.kernel == 0 && prev.user == 0 {
			values[i] = 0
			continue
		}
		if total <= 0 {
			values[i] = 0
			continue
		}
		busy := total - float64(cur.idle-prev.idle)
		values[i] = round1(100 * busy / total)
	}
	return values
}

func readFrequency() *float64 {
	if err := procCallNtPowerInformation.Find(); err != nil {
		return nil
	}
	count := coreCount()
	if count <= 0 {
		return nil
	}
	buf := make([]processorPower, count)
	status, _, _ := procCallNtPowerInformation.Call(
		processorInformationPowerLevel,
		0,
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf))*unsafe.Sizeof(buf[0]),
	)
	if uint32(status) != 0 {
		return nil
	}
	best := uint32(0)
	for _, info := range buf {
		if info.Number >= uint32(len(buf)) {
			continue
		}
		if info.CurrentMhz > best {
			best = info.CurrentMhz
		}
	}
	if best == 0 {
		return nil
	}
	return ptrFloat(float64(best))
}

func readMemory() (percent *float64, used, available, total *uint64) {
	var status memoryStatusEx
	status.Length = uint32(unsafe.Sizeof(status))
	if r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status))); r == 0 {
		return nil, nil, nil, nil
	}
	totalValue := status.TotalPhys
	availableValue := status.AvailPhys
	usedValue := totalValue - availableValue
	var pct float64
	if totalValue > 0 {
		pct = round1(100 * float64(usedValue) / float64(totalValue))
	}
	return ptrFloat(pct), ptrUint(usedValue), ptrUint(availableValue), ptrUint(totalValue)
}

func readSwap() (percent *float64, used, total *uint64) {
	if err := procGetPerformanceInfo.Find(); err != nil {
		return nil, nil, nil
	}
	var info performanceInfo
	info.CB = uint32(unsafe.Sizeof(info))
	if r, _, _ := procGetPerformanceInfo.Call(uintptr(unsafe.Pointer(&info)), uintptr(info.CB)); r == 0 {
		return nil, nil, nil
	}
	pageSize := uint64(info.PageSize)
	commitLimit := uint64(info.CommitLimit)
	commitTotal := uint64(info.CommitTotal)
	physicalTotal := uint64(info.PhysicalTotal)
	physicalAvailable := uint64(info.PhysicalAvailable)
	if commitLimit <= physicalTotal {
		return nil, nil, nil
	}
	totalValue := (commitLimit - physicalTotal) * pageSize
	usedValue := (commitTotal - (physicalTotal - physicalAvailable)) * pageSize
	if usedValue > totalValue {
		usedValue = totalValue
	}
	var pct float64
	if totalValue > 0 {
		pct = round1(100 * float64(usedValue) / float64(totalValue))
	}
	return ptrFloat(pct), ptrUint(usedValue), ptrUint(totalValue)
}

func coreCount() int {
	if r, _, _ := procGetActiveProcessorCount.Call(allProcessorGroups); r > 0 {
		return int(r)
	}
	return 0
}

func ptrFloat(v float64) *float64 { return &v }
func ptrUint(v uint64) *uint64    { return &v }

func round1(v float64) float64 { return math.Round(v*10) / 10 }
