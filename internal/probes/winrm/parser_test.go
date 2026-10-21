package winrm

import (
	"os"
	"testing"
)

func TestParseWinRMMetrics(t *testing.T) {
	content, err := os.ReadFile("../../../testdata/winrm_sample.json")
	if err != nil {
		t.Fatalf("No se pudo leer el archivo fixture: %v", err)
	}

	out, err := ParseWinRMMetrics(string(content))
	if err != nil {
		t.Fatalf("ParseWinRMMetrics falló: %v", err)
	}

	if out.OSName != "Microsoft Windows Server 2022 Datacenter" {
		t.Errorf("OSName inesperado: %s", out.OSName)
	}
	if out.CPUUsagePct != 28.5 {
		t.Errorf("CPUUsagePct esperado 28.5, obtenido %f", out.CPUUsagePct)
	}
	if len(out.Disks) != 2 {
		t.Fatalf("Esperaba 2 discos, obtuve %d", len(out.Disks))
	}
	if out.Disks[0].Device != "C:" || out.Disks[0].SizeBytes != 107374182400 {
		t.Errorf("Disco C: inesperado: %+v", out.Disks[0])
	}

	metrics, inv, services := FormatWinRMMetrics(out)
	if inv.CPUCores != 16 {
		t.Errorf("CPUCores esperado 16, obtenido %d", inv.CPUCores)
	}
	if inv.MemTotalBytes != 33554432*1024 {
		t.Errorf("MemTotalBytes esperado %d, obtenido %d", 33554432*1024, inv.MemTotalBytes)
	}

	// Verificar métricas formateadas
	foundCPU := false
	foundMemPct := false
	foundDiskC := false
	for _, m := range metrics {
		if m.Metric == "cpu.usage_pct" && m.Value == 28.5 {
			foundCPU = true
		}
		if m.Metric == "mem.used_pct" && m.Value > 0 {
			foundMemPct = true
		}
		if m.Metric == "disk.used_pct" && m.Dimension == "C:" && m.Value == 60 {
			foundDiskC = true
		}
	}

	if !foundCPU {
		t.Errorf("No se encontró métrica cpu.usage_pct")
	}
	if !foundMemPct {
		t.Errorf("No se encontró métrica mem.used_pct")
	}
	if !foundDiskC {
		t.Errorf("No se encontró métrica disk.used_pct C:")
	}

	// Servicios
	if len(services) != 2 {
		t.Fatalf("Esperaba 2 servicios, obtuve %d", len(services))
	}
	if services[0].Name != "MSSQLSERVER" || services[0].Status != "running" {
		t.Errorf("Servicio 0 inesperado: %+v", services[0])
	}
	if services[1].Name != "W3SVC" || services[1].Status != "stopped" {
		t.Errorf("Servicio 1 inesperado: %+v", services[1])
	}
}
