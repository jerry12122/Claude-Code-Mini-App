package sysmon

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// 解析函式不加平台 build tag：只吃 io.Reader，讓非 Linux 機器上也能跑單元測試。

// parseProcStat 解析 /proc/stat 的彙總列 `cpu user nice system idle iowait irq softirq steal ...`。
// idle 含 iowait；total 取前八欄（guest 已算在 user 內，不重複加）。舊核心可能只有前四欄，缺的視為 0。
func parseProcStat(r io.Reader) (cpuTimes, error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 0 || f[0] != "cpu" {
			continue
		}
		if len(f) < 5 {
			return cpuTimes{}, errors.New("sysmon: /proc/stat cpu 列欄位不足")
		}
		var v [8]uint64
		for i := 0; i < len(v) && i+1 < len(f); i++ {
			n, err := strconv.ParseUint(f[i+1], 10, 64)
			if err != nil {
				return cpuTimes{}, fmt.Errorf("sysmon: /proc/stat 欄位 %q: %w", f[i+1], err)
			}
			v[i] = n
		}
		var total uint64
		for _, n := range v {
			total += n
		}
		return cpuTimes{idle: v[3] + v[4], total: total}, nil
	}
	if err := sc.Err(); err != nil {
		return cpuTimes{}, err
	}
	return cpuTimes{}, errors.New("sysmon: /proc/stat 找不到 cpu 彙總列")
}

// parseMeminfo 解析 /proc/meminfo。已用 = MemTotal − MemAvailable（與 free 的 used 一致，不把可回收快取算進去）。
func parseMeminfo(r io.Reader) (memory, error) {
	var total, avail uint64
	var haveTotal, haveAvail bool
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		key, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok || (key != "MemTotal" && key != "MemAvailable") {
			continue
		}
		f := strings.Fields(rest)
		if len(f) == 0 {
			return memory{}, fmt.Errorf("sysmon: /proc/meminfo %s 沒有數值", key)
		}
		n, err := strconv.ParseUint(f[0], 10, 64)
		if err != nil {
			return memory{}, fmt.Errorf("sysmon: /proc/meminfo %s: %w", key, err)
		}
		n *= 1024 // 單位為 kB
		if key == "MemTotal" {
			total, haveTotal = n, true
		} else {
			avail, haveAvail = n, true
		}
	}
	if err := sc.Err(); err != nil {
		return memory{}, err
	}
	if !haveTotal || !haveAvail || total == 0 {
		return memory{}, errors.New("sysmon: /proc/meminfo 缺 MemTotal 或 MemAvailable")
	}
	var used uint64
	if avail < total {
		used = total - avail
	}
	return memory{used: used, total: total}, nil
}
