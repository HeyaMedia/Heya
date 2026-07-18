//go:build linux

package diagnostics

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

func readHostCPU() hostCPUSample {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return hostCPUSample{}
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return hostCPUSample{}
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return hostCPUSample{}
	}
	values := make([]float64, 0, len(fields)-1)
	var total float64
	for _, field := range fields[1:] {
		value, parseErr := strconv.ParseFloat(field, 64)
		if parseErr != nil {
			return hostCPUSample{}
		}
		values = append(values, value)
		total += value
	}
	idle := values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	return hostCPUSample{busy: total - idle, total: total, available: total > 0, metric: "cpu_utilization"}
}
