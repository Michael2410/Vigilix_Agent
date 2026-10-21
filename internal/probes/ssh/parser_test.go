package ssh

import (
	"os"
	"testing"
)

func TestParseLinuxMetrics(t *testing.T) {
	content, err := os.ReadFile("../../../testdata/proc_sample.txt")
	if err != nil {
		t.Fatalf("No se pudo leer el archivo fixture: %v", err)
	}

	parsed, err := ParseLinuxMetrics(string(content))
	if err != nil {
		t.Fatalf("ParseLinuxMetrics retornó error inesperado: %v", err)
	}

	// 1. CPU Ticks
	if parsed.CPUTicks == nil {
		t.Fatalf("Esperaba CPUTicks != nil")
	}
	if parsed.CPUTicks.User != 470500 || parsed.CPUTicks.Idle != 36991760 {
		t.Errorf("CPUTicks inesperados: %+v", parsed.CPUTicks)
	}

	// 2. Memoria
	if parsed.MemTotal != 32824960*1024 {
		t.Errorf("MemTotal esperado %d, obtenido %d", 32824960*1024, parsed.MemTotal)
	}
	if parsed.MemUsedPct <= 0 || parsed.MemUsedPct > 100 {
		t.Errorf("MemUsedPct fuera de rango: %f", parsed.MemUsedPct)
	}
	if parsed.SwapTotal != 4194300*1024 {
		t.Errorf("SwapTotal inesperado: %d", parsed.SwapTotal)
	}

	// 3. Load
	if parsed.Load1 != 0.42 {
		t.Errorf("Load1 esperado 0.42, obtenido %f", parsed.Load1)
	}

	// 4. Uptime
	if parsed.UptimeS != 345678.90 {
		t.Errorf("UptimeS esperado 345678.90, obtenido %f", parsed.UptimeS)
	}

	// 5. NetDev (lo debe estar excluido)
	if len(parsed.NetDevs) != 1 {
		t.Fatalf("Esperaba 1 interfaz (excluyendo lo), obtuve %d", len(parsed.NetDevs))
	}
	if parsed.NetDevs[0].Interface != "eth0" || parsed.NetDevs[0].RxBytes != 5000000000 {
		t.Errorf("NetDev inesperado: %+v", parsed.NetDevs[0])
	}

	// 6. Disks
	if len(parsed.Disks) != 2 {
		t.Fatalf("Esperaba 2 discos, obtuve %d", len(parsed.Disks))
	}
	if parsed.Disks[0].MountPoint != "/" || parsed.Disks[0].UsedPct != 53 {
		t.Errorf("Disco 0 inesperado: %+v", parsed.Disks[0])
	}
	if parsed.Disks[1].MountPoint != "/data" || parsed.Disks[1].UsedPct != 85 {
		t.Errorf("Disco 1 inesperado: %+v", parsed.Disks[1])
	}

	// 7. Inventory
	if parsed.Inventory.OSName != "Ubuntu 22.04.3 LTS" {
		t.Errorf("OSName inesperado: %s", parsed.Inventory.OSName)
	}
	if parsed.Inventory.OSVersion != "22.04" {
		t.Errorf("OSVersion inesperado: %s", parsed.Inventory.OSVersion)
	}
	if parsed.Inventory.CPUModel != "AMD EPYC 7763 64-Core Processor" {
		t.Errorf("CPUModel inesperado: %s", parsed.Inventory.CPUModel)
	}
	if parsed.Inventory.CPUCores != 4 {
		t.Errorf("CPUCores esperado 4, obtenido %d", parsed.Inventory.CPUCores)
	}

	// 8. Services
	if len(parsed.Services) != 4 {
		t.Fatalf("Esperaba 4 servicios, obtuve %d", len(parsed.Services))
	}
	expectedSvcs := map[string]string{
		"nginx":           "running",
		"postgresql":      "running",
		"redis-server":    "stopped",
		"unknown-service": "unknown",
	}
	for _, svc := range parsed.Services {
		exp, ok := expectedSvcs[svc.Name]
		if !ok {
			t.Errorf("Servicio inesperado: %s", svc.Name)
		} else if svc.Status != exp {
			t.Errorf("Servicio %s: esperado status %s, obtenido %s", svc.Name, exp, svc.Status)
		}
	}
}
