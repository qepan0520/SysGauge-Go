// Package history reads recorded CSV files.
//
// Parsing is deliberately forgiving: short rows (a power loss can truncate the
// last line), unparseable timestamps and blank lines are skipped and counted
// instead of aborting the load.
package history

import (
	"encoding/csv"
	"errors"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// BigFileRows is the size beyond which the UI mentions downsampling.
const BigFileRows = 500_000

// BytesPerGB converts byte counts for the summary.
const BytesPerGB = 1024 * 1024 * 1024

// Data is a parsed CSV file.
type Data struct {
	Times []float64
	CPU   []float64 // NaN when the cell was empty or invalid
	Mem   []float64

	Count   int
	Skipped int

	Start    time.Time
	End      time.Time
	Duration time.Duration

	CPUCount int
	CPUMean  float64
	CPUMin   float64
	CPUMax   float64

	MemCount int
	MemMean  float64
	MemMin   float64
	MemMax   float64

	MemUsedMean float64
	MemTotalGB  float64
}

// ErrEmpty is returned for a file without any content.
var ErrEmpty = errors.New("empty file")

// ErrNoTimestamp is returned when the header has no timestamp columns.
var ErrNoTimestamp = errors.New("missing timestamp columns")

// Parse reads a SysGauge CSV file.
func Parse(path string) (*Data, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = true

	header, err := reader.Read()
	if err == io.EOF {
		return nil, ErrEmpty
	}
	if err != nil {
		return nil, err
	}

	index := make(map[string]int, len(header))
	for i, name := range header {
		index[strings.TrimSpace(strings.TrimPrefix(name, "\ufeff"))] = i
	}
	msIndex, hasMS := index["timestamp_unix_ms"]
	isoIndex, hasISO := index["timestamp_iso"]
	if !hasMS && !hasISO {
		return nil, ErrNoTimestamp
	}

	data := &Data{}
	width := len(header)
	var cpuSum, memSum, memUsedSum float64
	var memUsedCount int
	var memTotal uint64

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if isBlank(record) {
			continue
		}
		if len(record) < width {
			data.Skipped++
			continue
		}

		stamp, ok := parseTimestamp(record, msIndex, hasMS, isoIndex, hasISO)
		if !ok {
			data.Skipped++
			continue
		}

		cpu, cpuOK := parseFloat(at(record, index, "cpu_percent"))
		mem, memOK := parseFloat(at(record, index, "mem_percent"))

		if used, ok := parseFloat(at(record, index, "mem_used_bytes")); ok {
			memUsedSum += used
			memUsedCount++
		}
		if total, ok := parseFloat(at(record, index, "mem_total_bytes")); ok && total > 0 {
			memTotal = uint64(total)
		}

		data.Times = append(data.Times, stamp)
		if cpuOK {
			data.CPU = append(data.CPU, cpu)
			if data.CPUCount == 0 {
				data.CPUMin, data.CPUMax = cpu, cpu
			} else {
				data.CPUMin = math.Min(data.CPUMin, cpu)
				data.CPUMax = math.Max(data.CPUMax, cpu)
			}
			cpuSum += cpu
			data.CPUCount++
		} else {
			data.CPU = append(data.CPU, math.NaN())
		}
		if memOK {
			data.Mem = append(data.Mem, mem)
			if data.MemCount == 0 {
				data.MemMin, data.MemMax = mem, mem
			} else {
				data.MemMin = math.Min(data.MemMin, mem)
				data.MemMax = math.Max(data.MemMax, mem)
			}
			memSum += mem
			data.MemCount++
		} else {
			data.Mem = append(data.Mem, math.NaN())
		}
		data.Count++
	}

	if data.Count > 0 {
		first := data.Times[0]
		last := data.Times[len(data.Times)-1]
		data.Start = time.UnixMilli(int64(first * 1000))
		data.End = time.UnixMilli(int64(last * 1000))
		data.Duration = time.Duration((last - first) * float64(time.Second))
	}
	if data.CPUCount > 0 {
		data.CPUMean = cpuSum / float64(data.CPUCount)
	}
	if data.MemCount > 0 {
		data.MemMean = memSum / float64(data.MemCount)
	}
	if memUsedCount > 0 {
		data.MemUsedMean = memUsedSum / float64(memUsedCount)
	}
	if memTotal > 0 {
		data.MemTotalGB = float64(memTotal) / BytesPerGB
	}
	return data, nil
}

func isBlank(record []string) bool {
	for _, cell := range record {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

func at(record []string, index map[string]int, name string) string {
	pos, ok := index[name]
	if !ok || pos >= len(record) {
		return ""
	}
	return record[pos]
}

func parseTimestamp(record []string, msIndex int, hasMS bool, isoIndex int, hasISO bool) (float64, bool) {
	if hasMS && msIndex < len(record) {
		if raw := strings.TrimSpace(record[msIndex]); raw != "" {
			if value, err := strconv.ParseFloat(raw, 64); err == nil {
				return value / 1000, true
			}
		}
	}
	if hasISO && isoIndex < len(record) {
		if raw := strings.TrimSpace(record[isoIndex]); raw != "" {
			if stamp, ok := parseISOTime(raw); ok {
				return float64(stamp.UnixMilli()) / 1000, true
			}
		}
	}
	return 0, false
}

var isoLayouts = []string{
	"2006-01-02T15:04:05.000",
	"2006-01-02T15:04:05.000000",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05.000",
	"2006-01-02 15:04:05",
	time.RFC3339Nano,
}

func parseISOTime(raw string) (time.Time, bool) {
	for _, layout := range isoLayouts {
		if stamp, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return stamp, true
		}
	}
	return time.Time{}, false
}

func parseFloat(raw string) (float64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}
