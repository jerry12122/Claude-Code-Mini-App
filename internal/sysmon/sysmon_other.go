//go:build !linux && !windows

package sysmon

func readSystem() (cpuTimes, memory, error) { return cpuTimes{}, memory{}, ErrUnsupported }
