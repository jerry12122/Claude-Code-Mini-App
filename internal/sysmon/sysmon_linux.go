//go:build linux

package sysmon

import "os"

func readSystem() (cpuTimes, memory, error) {
	stat, err := os.Open("/proc/stat")
	if err != nil {
		return cpuTimes{}, memory{}, err
	}
	defer stat.Close()
	c, err := parseProcStat(stat)
	if err != nil {
		return cpuTimes{}, memory{}, err
	}

	info, err := os.Open("/proc/meminfo")
	if err != nil {
		return cpuTimes{}, memory{}, err
	}
	defer info.Close()
	m, err := parseMeminfo(info)
	if err != nil {
		return cpuTimes{}, memory{}, err
	}
	return c, m, nil
}
