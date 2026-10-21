package ssh

import (
	"bufio"
	"math"
	"strconv"
	"strings"
	"time"

	"vigilix-agent/internal/model"
)

type CPUTicks struct {
	User    uint64
	Nice    uint64
	System  uint64
	Idle    uint64
	IOWait  uint64
	IRQ     uint64
	SoftIRQ uint64
	Steal   uint64
}

func (c CPUTicks) Total() uint64 {
	return c.User + c.Nice + c.System + c.Idle + c.IOWait + c.IRQ + c.SoftIRQ + c.Steal
}

func (c CPUTicks) IdleAll() uint64 {
	return c.Idle + c.IOWait
}

type NetDevStats struct {
	Interface string
	RxBytes   uint64
	TxBytes   uint64
}

type DiskStats struct {
	MountPoint string
	TotalBytes uint64
	FreeBytes  uint64
	UsedPct    float64
}

type ParsedLinuxMetrics struct {
	CPUTicks   *CPUTicks
	Load1      float64
	UptimeS    float64
	MemTotal   uint64
	MemUsed    uint64
	MemUsedPct float64
	SwapTotal  uint64
	SwapUsed   uint64
	SwapPct    float64
	Disks      []DiskStats
	NetDevs    []NetDevStats
	Services   []model.ServiceStatus
	Inventory  *model.HostInventory
}

func ParseLinuxMetrics(output string) (*ParsedLinuxMetrics, error) {
	sections := splitSections(output)
	res := &ParsedLinuxMetrics{
		Inventory: &model.HostInventory{},
	}

	// 1. stat (CPU ticks)
	if statContent, ok := sections["stat"]; ok {
		scanner := bufio.NewScanner(strings.NewReader(statContent))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "cpu ") {
				fields := strings.Fields(line)
				if len(fields) >= 8 {
					u, _ := strconv.ParseUint(fields[1], 10, 64)
					n, _ := strconv.ParseUint(fields[2], 10, 64)
					s, _ := strconv.ParseUint(fields[3], 10, 64)
					i, _ := strconv.ParseUint(fields[4], 10, 64)
					io, _ := strconv.ParseUint(fields[5], 10, 64)
					irq, _ := strconv.ParseUint(fields[6], 10, 64)
					sirq, _ := strconv.ParseUint(fields[7], 10, 64)
					var steal uint64
					if len(fields) >= 9 {
						steal, _ = strconv.ParseUint(fields[8], 10, 64)
					}
					res.CPUTicks = &CPUTicks{
						User:    u,
						Nice:    n,
						System:  s,
						Idle:    i,
						IOWait:  io,
						IRQ:     irq,
						SoftIRQ: sirq,
						Steal:   steal,
					}
				}
				break
			}
		}
	}

	// 2. meminfo
	if memContent, ok := sections["meminfo"]; ok {
		var memTotal, memFree, memAvail, buffers, cached, swapTotal, swapFree uint64
		scanner := bufio.NewScanner(strings.NewReader(memContent))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			valStr := strings.TrimSpace(parts[1])
			valFields := strings.Fields(valStr)
			if len(valFields) == 0 {
				continue
			}
			valKB, _ := strconv.ParseUint(valFields[0], 10, 64)
			valBytes := valKB * 1024

			switch key {
			case "MemTotal":
				memTotal = valBytes
			case "MemFree":
				memFree = valBytes
			case "MemAvailable":
				memAvail = valBytes
			case "Buffers":
				buffers = valBytes
			case "Cached":
				cached = valBytes
			case "SwapTotal":
				swapTotal = valBytes
			case "SwapFree":
				swapFree = valBytes
			}
		}

		if memTotal > 0 {
			res.MemTotal = memTotal
			res.Inventory.MemTotalBytes = memTotal
			if memAvail > 0 {
				res.MemUsed = memTotal - memAvail
			} else {
				res.MemUsed = memTotal - (memFree + buffers + cached)
			}
			res.MemUsedPct = math.Round((float64(res.MemUsed)/float64(memTotal))*1000) / 10
		}

		if swapTotal > 0 {
			res.SwapTotal = swapTotal
			res.SwapUsed = swapTotal - swapFree
			res.SwapPct = math.Round((float64(res.SwapUsed)/float64(swapTotal))*1000) / 10
		}
	}

	// 3. loadavg
	if loadContent, ok := sections["loadavg"]; ok {
		fields := strings.Fields(loadContent)
		if len(fields) >= 1 {
			res.Load1, _ = strconv.ParseFloat(fields[0], 64)
		}
	}

	// 4. uptime
	if uptimeContent, ok := sections["uptime"]; ok {
		fields := strings.Fields(uptimeContent)
		if len(fields) >= 1 {
			res.UptimeS, _ = strconv.ParseFloat(fields[0], 64)
			if res.UptimeS > 0 {
				res.Inventory.BootTime = time.Now().UTC().Add(-time.Duration(res.UptimeS) * time.Second)
			}
		}
	}

	// 5. netdev
	if netContent, ok := sections["netdev"]; ok {
		scanner := bufio.NewScanner(strings.NewReader(netContent))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.Contains(line, ":") {
				continue
			}
			parts := strings.SplitN(line, ":", 2)
			iface := strings.TrimSpace(parts[0])
			if iface == "lo" {
				continue // Excluir loopback según §5.2
			}
			fields := strings.Fields(parts[1])
			if len(fields) >= 9 {
				rxBytes, _ := strconv.ParseUint(fields[0], 10, 64)
				txBytes, _ := strconv.ParseUint(fields[8], 10, 64)
				res.NetDevs = append(res.NetDevs, NetDevStats{
					Interface: iface,
					RxBytes:   rxBytes,
					TxBytes:   txBytes,
				})
			}
		}
	}

	// 6. df
	if dfContent, ok := sections["df"]; ok {
		scanner := bufio.NewScanner(strings.NewReader(dfContent))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "Filesystem") || line == "" {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 6 {
				size, _ := strconv.ParseUint(fields[1], 10, 64)
				avail, _ := strconv.ParseUint(fields[3], 10, 64)
				pctStr := strings.TrimSuffix(fields[4], "%")
				pct, _ := strconv.ParseFloat(pctStr, 64)
				mount := fields[5]

				res.Disks = append(res.Disks, DiskStats{
					MountPoint: mount,
					TotalBytes: size,
					FreeBytes:  avail,
					UsedPct:    pct,
				})
			}
		}
	}

	// 7. os
	if osContent, ok := sections["os"]; ok {
		scanner := bufio.NewScanner(strings.NewReader(osContent))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			k := strings.TrimSpace(parts[0])
			v := strings.Trim(strings.TrimSpace(parts[1]), `"`)
			if k == "PRETTY_NAME" {
				res.Inventory.OSName = v
			} else if k == "VERSION_ID" {
				res.Inventory.OSVersion = v
			}
		}
	}

	// 8. cpuinfo
	if cpuContent, ok := sections["cpuinfo"]; ok {
		scanner := bufio.NewScanner(strings.NewReader(cpuContent))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "model name") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					res.Inventory.CPUModel = strings.TrimSpace(parts[1])
				}
			} else if strings.HasPrefix(line, "cores:") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					cores, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
					res.Inventory.CPUCores = cores
				}
			}
		}
	}

	// 9. services
	if svcContent, ok := sections["services"]; ok {
		scanner := bufio.NewScanner(strings.NewReader(svcContent))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				name := fields[0]
				rawStatus := fields[1]
				status := normalizeLinuxServiceStatus(rawStatus)
				res.Services = append(res.Services, model.ServiceStatus{
					Name:   name,
					Status: status,
				})
			}
		}
	}

	return res, nil
}

func normalizeLinuxServiceStatus(raw string) string {
	switch strings.ToLower(raw) {
	case "active":
		return "running"
	case "inactive", "failed", "dead":
		return "stopped"
	case "activating":
		return "starting"
	case "deactivating":
		return "stopping"
	default:
		return "unknown"
	}
}

func splitSections(output string) map[string]string {
	sections := make(map[string]string)
	var currentSection string
	var currentLines []string

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "### ") {
			if currentSection != "" {
				sections[currentSection] = strings.Join(currentLines, "\n")
			}
			currentSection = strings.TrimSpace(strings.TrimPrefix(trimmed, "### "))
			currentLines = nil
		} else if currentSection != "" {
			currentLines = append(currentLines, line)
		}
	}

	if currentSection != "" {
		sections[currentSection] = strings.Join(currentLines, "\n")
	}

	return sections
}

func FormatMetrics(parsed *ParsedLinuxMetrics, cpuUsagePct *float64, rxBps, txBps map[string]float64) []model.MetricItem {
	var metrics []model.MetricItem

	if cpuUsagePct != nil {
		metrics = append(metrics, model.MetricItem{
			Metric: "cpu.usage_pct",
			Value:  math.Round(*cpuUsagePct*10) / 10,
		})
	}

	if parsed.Load1 > 0 {
		metrics = append(metrics, model.MetricItem{
			Metric: "cpu.load1",
			Value:  parsed.Load1,
		})
	}

	if parsed.MemTotal > 0 {
		metrics = append(metrics, model.MetricItem{
			Metric: "mem.used_pct",
			Value:  parsed.MemUsedPct,
		})
		metrics = append(metrics, model.MetricItem{
			Metric: "mem.used_bytes",
			Value:  float64(parsed.MemUsed),
		})
	}

	if parsed.SwapTotal > 0 {
		metrics = append(metrics, model.MetricItem{
			Metric: "swap.used_pct",
			Value:  parsed.SwapPct,
		})
	}

	if parsed.UptimeS > 0 {
		metrics = append(metrics, model.MetricItem{
			Metric: "system.uptime_s",
			Value:  parsed.UptimeS,
		})
	}

	for _, d := range parsed.Disks {
		metrics = append(metrics, model.MetricItem{
			Metric:    "disk.used_pct",
			Dimension: d.MountPoint,
			Value:     d.UsedPct,
		})
		metrics = append(metrics, model.MetricItem{
			Metric:    "disk.free_bytes",
			Dimension: d.MountPoint,
			Value:     float64(d.FreeBytes),
		})
	}

	for iface, rx := range rxBps {
		metrics = append(metrics, model.MetricItem{
			Metric:    "net.rx_bps",
			Dimension: iface,
			Value:     math.Round(rx),
		})
	}

	for iface, tx := range txBps {
		metrics = append(metrics, model.MetricItem{
			Metric:    "net.tx_bps",
			Dimension: iface,
			Value:     math.Round(tx),
		})
	}

	return metrics
}
