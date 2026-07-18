//go:build !linux && !darwin

package diagnostics

func readHostCPU() hostCPUSample { return hostCPUSample{} }
