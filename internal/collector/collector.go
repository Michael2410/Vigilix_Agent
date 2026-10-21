package collector

import (
	"context"
	"runtime"
	"strings"
	"time"

	"vigilix-agent/internal/model"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

type Collector struct {
	deltaTracker *DeltaTracker
}

func NewCollector() *Collector {
	return &Collector{
		deltaTracker: NewDeltaTracker(),
	}
}

// CollectInventory recolecta el snapshot de hardware y SO según §5.1
func (c *Collector) CollectInventory(ctx context.Context) (*model.HostInventory, error) {
	hInfo, err := host.InfoWithContext(ctx)
	if err != nil {
		return nil, err
	}

	cpuInfos, _ := cpu.InfoWithContext(ctx)
	cpuModel := "Unknown"
	cpuCores := runtime.NumCPU()
	if len(cpuInfos) > 0 {
		cpuModel = cpuInfos[0].ModelName
		if cpuInfos[0].Cores > 0 {
			cpuCores = int(cpuInfos[0].Cores)
		}
	}

	vMem, _ := mem.VirtualMemoryWithContext(ctx)
	var memTotal uint64
	if vMem != nil {
		memTotal = vMem.Total
	}

	bootTime := time.Unix(int64(hInfo.BootTime), 0).UTC()

	return &model.HostInventory{
		OSName:        hInfo.Platform,
		OSVersion:     hInfo.PlatformVersion,
		CPUModel:      cpuModel,
		CPUCores:      cpuCores,
		MemTotalBytes: memTotal,
		BootTime:      bootTime,
	}, nil
}

// CollectMetrics recolecta todas las métricas según catálogo v1 (§5.2)
func (c *Collector) CollectMetrics(ctx context.Context) ([]model.MetricItem, error) {
	now := time.Now().UTC()
	var items []model.MetricItem

	// 1. CPU Usage Pct
	cpuPcts, err := cpu.PercentWithContext(ctx, 0, false)
	if err == nil && len(cpuPcts) > 0 {
		items = append(items, model.MetricItem{
			Metric: "cpu.usage_pct",
			Value:  cpuPcts[0],
		})
	}

	// 2. CPU Load1 (solo Linux)
	if runtime.GOOS == "linux" {
		loadStat, err := load.AvgWithContext(ctx)
		if err == nil && loadStat != nil {
			items = append(items, model.MetricItem{
				Metric: "cpu.load1",
				Value:  loadStat.Load1,
			})
		}
	}

	// 3. Memoria (RAM y Swap)
	vMem, err := mem.VirtualMemoryWithContext(ctx)
	if err == nil && vMem != nil {
		items = append(items,
			model.MetricItem{
				Metric: "mem.used_pct",
				Value:  vMem.UsedPercent,
			},
			model.MetricItem{
				Metric: "mem.used_bytes",
				Value:  float64(vMem.Used),
			},
		)
	}

	if runtime.GOOS == "linux" {
		swapMem, err := mem.SwapMemoryWithContext(ctx)
		if err == nil && swapMem != nil && swapMem.Total > 0 {
			items = append(items, model.MetricItem{
				Metric: "swap.used_pct",
				Value:  swapMem.UsedPercent,
			})
		}
	}

	// 4. Discos (§5.2: excluir tmpfs, devtmpfs, overlay, squashfs en Linux)
	partitions, err := disk.PartitionsWithContext(ctx, false)
	if err == nil {
		seenMounts := make(map[string]bool)
		for _, part := range partitions {
			fstype := strings.ToLower(part.Fstype)
			mount := part.Mountpoint

			// Filtros de exclusión
			if runtime.GOOS == "linux" {
				if fstype == "tmpfs" || fstype == "devtmpfs" || fstype == "overlay" ||
					fstype == "squashfs" || strings.HasPrefix(mount, "/var/lib/docker") {
					continue
				}
			}

			if seenMounts[mount] {
				continue
			}
			seenMounts[mount] = true

			usage, err := disk.UsageWithContext(ctx, mount)
			if err != nil || usage == nil {
				continue
			}

			dim := mount
			if runtime.GOOS == "windows" {
				dim = part.Device
			}

			items = append(items,
				model.MetricItem{
					Metric:    "disk.used_pct",
					Dimension: dim,
					Value:     usage.UsedPercent,
				},
				model.MetricItem{
					Metric:    "disk.free_bytes",
					Dimension: dim,
					Value:     float64(usage.Free),
				},
			)
		}
	}

	// 5. Red (deltas y tasas rx_bps / tx_bps excluyendo loopback)
	netCounters, err := net.IOCountersWithContext(ctx, true)
	if err == nil {
		for _, counter := range netCounters {
			iface := counter.Name
			if iface == "lo" || strings.HasPrefix(iface, "Loopback") {
				continue
			}

			rates, ok := c.deltaTracker.CalculateNetRates(iface, counter.BytesRecv, counter.BytesSent, now)
			if ok {
				items = append(items,
					model.MetricItem{
						Metric:    "net.rx_bps",
						Dimension: iface,
						Value:     rates.RxBps,
					},
					model.MetricItem{
						Metric:    "net.tx_bps",
						Dimension: iface,
						Value:     rates.TxBps,
					},
				)
			}
		}
	}

	// 6. Uptime
	uptime, err := host.UptimeWithContext(ctx)
	if err == nil {
		items = append(items, model.MetricItem{
			Metric: "system.uptime_s",
			Value:  float64(uptime),
		})
	}

	// 7. Process count
	pids, err := process.PidsWithContext(ctx)
	if err == nil {
		items = append(items, model.MetricItem{
			Metric: "proc.count",
			Value:  float64(len(pids)),
		})
	}

	return items, nil
}
