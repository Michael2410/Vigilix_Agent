package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"vigilix-agent/internal/client"
	"vigilix-agent/internal/collector"
	"vigilix-agent/internal/config"
	"vigilix-agent/internal/model"
)

const (
	AgentVersion  = "0.1.0"
	DefaultCfgPath = "config.yaml"
)

func generateUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func main() {
	var (
		configPath string
		enrollCmd  bool
		serverURL  string
		token      string
	)

	flag.StringVar(&configPath, "config", DefaultCfgPath, "Ruta al archivo de configuración config.yaml")
	flag.BoolVar(&enrollCmd, "enroll", false, "Ejecutar enrolamiento del agente")
	flag.StringVar(&serverURL, "server", "", "URL del servidor API (ej. http://localhost/api)")
	flag.StringVar(&token, "token", "", "Token de enrolamiento único generado en el servidor")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Subcomando de Enrolamiento
	if enrollCmd || (len(os.Args) > 1 && os.Args[1] == "enroll") {
		runEnroll(configPath, serverURL, token)
		return
	}

	// Modo Normal / Daemon
	runDaemon(configPath)
}

func runEnroll(configPath, serverURL, token string) {
	if serverURL == "" {
		serverURL = "http://localhost/api"
	}
	if token == "" {
		slog.Error("El parámetro -token es obligatorio para enrolar el agente")
		os.Exit(1)
	}

	hostname, _ := os.Hostname()
	slog.Info("Iniciando enrolamiento del agente...",
		"server", serverURL,
		"hostname", hostname,
		"os", runtime.GOOS,
		"version", AgentVersion,
	)

	apiClient := client.NewAPIClient(serverURL, "")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	resp, err := apiClient.Enroll(ctx, &model.EnrollRequest{
		Token:        token,
		Hostname:     hostname,
		OS:           runtime.GOOS,
		AgentVersion: AgentVersion,
	})
	if err != nil {
		slog.Error("Fallo al enrolar el agente", "error", err)
		os.Exit(1)
	}

	cfg := &config.Config{
		ServerURL: serverURL,
		AgentID:   resp.AgentID,
		HostID:    resp.HostID,
		APIKey:    resp.APIKey,
		IntervalS: resp.IntervalS,
	}

	if err := config.SaveConfig(configPath, cfg); err != nil {
		slog.Error("Error guardando configuración tras enrolamiento", "error", err)
		os.Exit(1)
	}

	slog.Info("Agente enrolado exitosamente y configuración guardada.",
		"hostId", resp.HostID,
		"agentId", resp.AgentID,
		"intervalS", resp.IntervalS,
		"configFile", configPath,
	)
}

func runDaemon(configPath string) {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		slog.Error("Error cargando configuración", "error", err)
		os.Exit(1)
	}

	if cfg.APIKey == "" || cfg.HostID == "" {
		slog.Error("El agente no está enrolado. Ejecute 'vigilix-agent -enroll -token <token>' primero.")
		os.Exit(1)
	}

	slog.Info("Iniciando Vigilix Agent...",
		"hostId", cfg.HostID,
		"server", cfg.ServerURL,
		"intervalS", cfg.IntervalS,
	)

	apiClient := client.NewAPIClient(cfg.ServerURL, cfg.APIKey)
	metricCollector := collector.NewCollector()

	// Enviar snapshot de inventario inicial
	ctxInit, cancelInit := context.WithTimeout(context.Background(), 10*time.Second)
	inv, _ := metricCollector.CollectInventory(ctxInit)
	cancelInit()

	interval := time.Duration(cfg.IntervalS) * time.Second
	if interval < 10*time.Second {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Primer ciclo inmediato
	sendSample(apiClient, metricCollector, cfg.HostID, inv)
	inv = nil // Solo enviar inventario al arrancar o periódicamente

	for {
		select {
		case <-sigChan:
			slog.Info("Deteniendo Vigilix Agent por señal de término...")
			return
		case <-ticker.C:
			sendSample(apiClient, metricCollector, cfg.HostID, inv)
		}
	}
}

func sendSample(apiClient *client.APIClient, metricCollector *collector.Collector, hostID string, inv *model.HostInventory) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	metrics, err := metricCollector.CollectMetrics(ctx)
	if err != nil {
		slog.Warn("Error recolectando métricas", "error", err)
		return
	}

	now := time.Now().UTC()
	payload := &model.IngestPayload{
		Schema:  1,
		BatchID: generateUUID(),
		Source:  "agent",
		SentAt:  now,
		Hosts: []model.HostPayload{
			{
				HostID:      hostID,
				CollectedAt: now,
				Inventory:   inv,
				Metrics:     metrics,
			},
		},
		HTTPResults: []interface{}{},
	}

	if err := apiClient.SendIngest(ctx, payload); err != nil {
		slog.Error("Error enviando métricas al servidor", "error", err)
	} else {
		slog.Info("Métricas enviadas con éxito",
			"batchId", payload.BatchID,
			"metricsCount", len(metrics),
		)
	}
}
