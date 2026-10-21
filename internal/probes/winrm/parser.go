package winrm

import (
	"encoding/json"
	"math"
	"strings"
	"time"

	"vigilix-agent/internal/model"
)

type WinRMJSONDisk struct {
	Device    string `json:"device"`
	SizeBytes int64  `json:"size_bytes"`
	FreeBytes int64  `json:"free_bytes"`
}

type WinRMJSONInterface struct {
	Name  string `json:"name"`
	RxBps int64  `json:"rx_bps"`
	TxBps int64  `json:"tx_bps"`
}

type WinRMJSONService struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type WinRMJSONOutput struct {
	CPUUsagePct float64              `json:"cpu_usage_pct"`
	MemTotalKB  int64                `json:"mem_total_kb"`
	MemFreeKB   int64                `json:"mem_free_kb"`
	OSName      string               `json:"os_name"`
	OSVersion   string               `json:"os_version"`
	CPUModel    string               `json:"cpu_model"`
	CPUCores    int                  `json:"cpu_cores"`
	BootTime    *string              `json:"boot_time"`
	Disks       []WinRMJSONDisk      `json:"disks"`
	Interfaces  []WinRMJSONInterface `json:"interfaces"`
	Services    []WinRMJSONService   `json:"services"`
}

func ParseWinRMMetrics(jsonStr string) (*WinRMJSONOutput, error) {
	// Limpiar posibles líneas adicionales o prefijos antes del JSON
	trimmed := strings.TrimSpace(jsonStr)
	startIdx := strings.Index(trimmed, "{")
	if startIdx != -1 {
		trimmed = trimmed[startIdx:]
	}
	lastIdx := strings.LastIndex(trimmed, "}")
	if lastIdx != -1 {
		trimmed = trimmed[:lastIdx+1]
	}

	var out WinRMJSONOutput
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func FormatWinRMMetrics(out *WinRMJSONOutput) ([]model.MetricItem, *model.HostInventory, []model.ServiceStatus) {
	var metrics []model.MetricItem
	inv := &model.HostInventory{
		OSName:    out.OSName,
		OSVersion: out.OSVersion,
		CPUModel:  out.CPUModel,
		CPUCores:  out.CPUCores,
	}

	if out.BootTime != nil && *out.BootTime != "" {
		if t, err := time.Parse(time.RFC3339, *out.BootTime); err == nil {
			inv.BootTime = t
			uptimeS := time.Since(t).Seconds()
			if uptimeS > 0 {
				metrics = append(metrics, model.MetricItem{
					Metric: "system.uptime_s",
					Value:  math.Round(uptimeS),
				})
			}
		}
	}

	// 1. CPU
	metrics = append(metrics, model.MetricItem{
		Metric: "cpu.usage_pct",
		Value:  math.Round(out.CPUUsagePct*10) / 10,
	})

	// 2. RAM
	if out.MemTotalKB > 0 {
		totalBytes := uint64(out.MemTotalKB) * 1024
		inv.MemTotalBytes = totalBytes
		freeBytes := uint64(out.MemFreeKB) * 1024
		usedBytes := totalBytes - freeBytes
		usedPct := (float64(usedBytes) / float64(totalBytes)) * 100.0

		metrics = append(metrics, model.MetricItem{
			Metric: "mem.used_pct",
			Value:  math.Round(usedPct*10) / 10,
		})
		metrics = append(metrics, model.MetricItem{
			Metric: "mem.used_bytes",
			Value:  float64(usedBytes),
		})
	}

	// 3. Discos
	for _, d := range out.Disks {
		if d.SizeBytes > 0 {
			usedBytes := d.SizeBytes - d.FreeBytes
			usedPct := (float64(usedBytes) / float64(d.SizeBytes)) * 100.0
			metrics = append(metrics, model.MetricItem{
				Metric:    "disk.used_pct",
				Dimension: d.Device,
				Value:     math.Round(usedPct*10) / 10,
			})
			metrics = append(metrics, model.MetricItem{
				Metric:    "disk.free_bytes",
				Dimension: d.Device,
				Value:     float64(d.FreeBytes),
			})
		}
	}

	// 4. Interfaces de red
	for _, iface := range out.Interfaces {
		metrics = append(metrics, model.MetricItem{
			Metric:    "net.rx_bps",
			Dimension: iface.Name,
			Value:     float64(iface.RxBps),
		})
		metrics = append(metrics, model.MetricItem{
			Metric:    "net.tx_bps",
			Dimension: iface.Name,
			Value:     float64(iface.TxBps),
		})
	}

	// 5. Servicios
	var services []model.ServiceStatus
	for _, s := range out.Services {
		services = append(services, model.ServiceStatus{
			Name:   s.Name,
			Status: s.Status,
		})
	}

	return metrics, inv, services
}
