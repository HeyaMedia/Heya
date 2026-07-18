//go:build !linux && !darwin

package diagnostics

func readProcessCPUSeconds() (float64, bool) { return 0, false }
