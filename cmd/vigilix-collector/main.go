package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"vigilix-agent/internal/collector"
)

var (
	version = "0.2.0"
)

func main() {
	serverFlag := flag.String("server", "", "URL base de la API de Vigilix (ej: http://localhost/api)")
	tokenFlag := flag.String("token", "", "Token secreto de autenticación del colector")
	versionFlag := flag.Bool("version", false, "Imprimir versión del binario")

	flag.Parse()

	if *versionFlag {
		fmt.Printf("vigilix-collector v%s\n", version)
		os.Exit(0)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	serverURL := *serverFlag
	if serverURL == "" {
		serverURL = os.Getenv("VIGILIX_SERVER")
	}
	if serverURL == "" {
		serverURL = "http://localhost/api"
	}

	token := *tokenFlag
	if token == "" {
		token = os.Getenv("VIGILIX_COLLECTOR_TOKEN")
	}

	if token == "" {
		logger.Error("Token de colector obligatorio. Usa -token o la variable VIGILIX_COLLECTOR_TOKEN")
		flag.Usage()
		os.Exit(1)
	}

	logger.Info("Iniciando Vigilix Collector",
		"version", version,
		"server", serverURL,
	)

	cfg := collector.Config{
		ServerURL: serverURL,
		Token:     token,
		Version:   version,
	}

	sched := collector.NewScheduler(cfg, logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := sched.Start(ctx); err != nil {
		logger.Error("Error en scheduler de colector", "error", err)
		os.Exit(1)
	}

	logger.Info("Vigilix Collector finalizado correctamente")
}
