package winrm

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/masterzen/winrm"

	"vigilix-agent/internal/model"
	"vigilix-agent/scripts/windows"
)

type CredentialInfo struct {
	Kind     string `json:"kind"`
	Username string `json:"username"`
	Secret   string `json:"secret"`
}

type WinRMJob struct {
	HostID          string         `json:"hostId"`
	Address         string         `json:"address"`
	Port            int            `json:"port"`
	TLS             bool           `json:"tls"`
	TimeoutMs       int            `json:"timeoutMs"`
	WatchedServices []string       `json:"watchedServices"`
	Credential      CredentialInfo `json:"credential"`
}

type WinRMPollResult struct {
	HostID      string
	Metrics     []model.MetricItem
	Inventory   *model.HostInventory
	Services    []model.ServiceStatus
	Error       *model.HostError
	CollectedAt time.Time
}

func ExecuteWinRMPoll(ctx context.Context, job WinRMJob) WinRMPollResult {
	result := WinRMPollResult{
		HostID:      job.HostID,
		CollectedAt: time.Now().UTC(),
	}

	timeout := time.Duration(job.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 20 * time.Second
	}

	port := job.Port
	if port <= 0 {
		if job.TLS {
			port = 5986
		} else {
			port = 5985
		}
	}

	// Construir endpoint WinRM
	endpoint := winrm.NewEndpoint(
		job.Address,
		port,
		job.TLS,
		true, // InsecureSkipVerify: común para certificados autofirmados WinRM
		nil,
		nil,
		nil,
		timeout,
	)

	params := winrm.DefaultParameters
	params.Timeout = fmt.Sprintf("PT%dS", int(timeout.Seconds()))

	client, err := winrm.NewClientWithParameters(endpoint, job.Credential.Username, job.Credential.Secret, params)
	if err != nil {
		result.Error = &model.HostError{
			Code:    "AUTH_FAILED",
			Message: fmt.Sprintf("Error creando cliente WinRM: %v", err),
		}
		return result
	}

	// Preparar comando PowerShell con script embebido
	var servicesArgs []string
	for _, s := range job.WatchedServices {
		servicesArgs = append(servicesArgs, fmt.Sprintf("'%s'", strings.ReplaceAll(s, "'", "''")))
	}
	svcParam := strings.Join(servicesArgs, ",")

	fullScript := fmt.Sprintf("& {\n%s\n} -Services @(%s)", windows.MetricsScript, svcParam)
	encodedCmd := encodePowershellCommand(fullScript)
	cmdLine := fmt.Sprintf("powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand %s", encodedCmd)

	var stdoutBuf, stderrBuf bytes.Buffer

	// Ejecutar con control de contexto
	done := make(chan error, 1)
	go func() {
		_, runErr := client.RunWithContext(ctx, cmdLine, &stdoutBuf, &stderrBuf)
		done <- runErr
	}()

	select {
	case <-ctx.Done():
		result.Error = &model.HostError{
			Code:    "TIMEOUT",
			Message: "Operación de sondeo WinRM cancelada por timeout de contexto",
		}
		return result
	case err := <-done:
		if err != nil {
			result.Error = classifyWinRMError(err, stderrBuf.String())
			return result
		}
	}

	out, err := ParseWinRMMetrics(stdoutBuf.String())
	if err != nil {
		result.Error = &model.HostError{
			Code:    "PARSE_ERROR",
			Message: fmt.Sprintf("Error parseando JSON de WinRM: %v, salida cruda: %s", err, stdoutBuf.String()),
		}
		return result
	}

	metrics, inv, services := FormatWinRMMetrics(out)
	result.Metrics = metrics
	result.Inventory = inv
	result.Services = services
	return result
}

func encodePowershellCommand(cmd string) string {
	utf16Units := utf16.Encode([]rune(cmd))
	bytesBuf := make([]byte, len(utf16Units)*2)
	for i, u := range utf16Units {
		bytesBuf[i*2] = byte(u)
		bytesBuf[i*2+1] = byte(u >> 8)
	}
	return base64.StdEncoding.EncodeToString(bytesBuf)
}

func classifyWinRMError(err error, stderr string) *model.HostError {
	if err == nil {
		return nil
	}
	errStr := strings.ToLower(err.Error() + " " + stderr)

	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline exceeded") {
		return &model.HostError{
			Code:    "TIMEOUT",
			Message: fmt.Sprintf("Tiempo de espera agotado al conectar por WinRM: %v", err),
		}
	}

	if strings.Contains(errStr, "401") || strings.Contains(errStr, "unauthorized") || strings.Contains(errStr, "authentication failed") || strings.Contains(errStr, "access is denied") {
		return &model.HostError{
			Code:    "AUTH_FAILED",
			Message: fmt.Sprintf("Autenticación WinRM fallida: %v", err),
		}
	}

	if strings.Contains(errStr, "refused") || strings.Contains(errStr, "no route to host") || strings.Contains(errStr, "network is unreachable") || strings.Contains(errStr, "connection reset") {
		return &model.HostError{
			Code:    "UNREACHABLE",
			Message: fmt.Sprintf("Host inalcanzable por WinRM: %v", err),
		}
	}

	return &model.HostError{
		Code:    "PARSE_ERROR",
		Message: fmt.Sprintf("Error en ejecución de comando WinRM: %v, stderr: %s", err, stderr),
	}
}
