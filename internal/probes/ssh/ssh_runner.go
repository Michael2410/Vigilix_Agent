package ssh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"vigilix-agent/internal/model"
	"vigilix-agent/scripts/linux"
)

type CredentialInfo struct {
	Kind     string `json:"kind"`
	Username string `json:"username"`
	Secret   string `json:"secret"`
}

type SSHJob struct {
	HostID          string         `json:"hostId"`
	Address         string         `json:"address"`
	Port            int            `json:"port"`
	TimeoutMs       int            `json:"timeoutMs"`
	WatchedServices []string       `json:"watchedServices"`
	SSHHostKey      *string        `json:"sshHostKey"`
	Credential      CredentialInfo `json:"credential"`
}

type SSHPollResult struct {
	HostID         string
	Metrics        *ParsedLinuxMetrics
	NewHostKey     string
	Error          *model.HostError
	CollectedAt    time.Time
}

var ErrHostKeyMismatch = errors.New("HOST_KEY_MISMATCH")

func ExecuteSSHPoll(ctx context.Context, job SSHJob) SSHPollResult {
	result := SSHPollResult{
		HostID:      job.HostID,
		CollectedAt: time.Now().UTC(),
	}

	timeout := time.Duration(job.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 20 * time.Second
	}

	var authMethods []ssh.AuthMethod
	switch job.Credential.Kind {
	case "ssh_password":
		authMethods = append(authMethods, ssh.Password(job.Credential.Secret))
	case "ssh_key":
		signer, err := ssh.ParsePrivateKey([]byte(job.Credential.Secret))
		if err != nil {
			result.Error = &model.HostError{
				Code:    "AUTH_FAILED",
				Message: fmt.Sprintf("Error parseando llave privada SSH: %v", err),
			}
			return result
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	default:
		result.Error = &model.HostError{
			Code:    "AUTH_FAILED",
			Message: fmt.Sprintf("Tipo de credencial no soportado para SSH: %s", job.Credential.Kind),
		}
		return result
	}

	var detectedKey string
	var hostKeyMismatch bool

	hostKeyCallback := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		fp := ssh.FingerprintSHA256(key)
		if job.SSHHostKey == nil || *job.SSHHostKey == "" {
			// TOFU: Primera conexión, guardar y reportar
			detectedKey = fp
			return nil
		}
		if *job.SSHHostKey != fp {
			hostKeyMismatch = true
			return fmt.Errorf("%w: esperado %s, detectado %s", ErrHostKeyMismatch, *job.SSHHostKey, fp)
		}
		return nil
	}

	clientConfig := &ssh.ClientConfig{
		User:            job.Credential.Username,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         timeout,
	}

	port := job.Port
	if port <= 0 {
		port = 22
	}
	targetAddr := fmt.Sprintf("%s:%d", job.Address, port)

	// Conectar respetando el contexto
	dialer := net.Dialer{Timeout: timeout}
	netConn, err := dialer.DialContext(ctx, "tcp", targetAddr)
	if err != nil {
		result.Error = classifyNetError(err)
		return result
	}
	defer netConn.Close()

	// SSH handshake
	sshConn, chans, reqs, err := ssh.NewClientConn(netConn, targetAddr, clientConfig)
	if err != nil {
		if hostKeyMismatch || errors.Is(err, ErrHostKeyMismatch) || strings.Contains(err.Error(), "HOST_KEY_MISMATCH") {
			result.Error = &model.HostError{
				Code:    "HOST_KEY_MISMATCH",
				Message: fmt.Sprintf("Fingerprint de host key no coincide: %v", err),
			}
			return result
		}
		if strings.Contains(err.Error(), "unable to authenticate") || strings.Contains(err.Error(), "authentication failed") {
			result.Error = &model.HostError{
				Code:    "AUTH_FAILED",
				Message: fmt.Sprintf("Autenticación SSH fallida para el usuario %s: %v", job.Credential.Username, err),
			}
			return result
		}
		result.Error = classifyNetError(err)
		return result
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close()

	if detectedKey != "" {
		result.NewHostKey = detectedKey
	}

	// Ejecutar script metrics.sh embebido
	session, err := client.NewSession()
	if err != nil {
		result.Error = &model.HostError{
			Code:    "TIMEOUT",
			Message: fmt.Sprintf("Error abriendo sesión SSH: %v", err),
		}
		return result
	}
	defer session.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf
	session.Stdin = strings.NewReader(linux.MetricsScript)

	// sh -s -- svc1 svc2 ...
	cmd := "sh -s --"
	for _, svc := range job.WatchedServices {
		cmd += " " + shellEscape(svc)
	}

	done := make(chan error, 1)
	go func() {
		done <- session.Run(cmd)
	}()

	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGKILL)
		result.Error = &model.HostError{
			Code:    "TIMEOUT",
			Message: "Operación de sondeo SSH cancelada por timeout de contexto",
		}
		return result
	case err := <-done:
		if err != nil {
			result.Error = &model.HostError{
				Code:    "PARSE_ERROR",
				Message: fmt.Sprintf("Error ejecutando script de métricas remoto: %v, stderr: %s", err, stderrBuf.String()),
			}
			return result
		}
	}

	parsed, err := ParseLinuxMetrics(stdoutBuf.String())
	if err != nil {
		result.Error = &model.HostError{
			Code:    "PARSE_ERROR",
			Message: fmt.Sprintf("Error parseando métricas de Linux: %v", err),
		}
		return result
	}

	result.Metrics = parsed
	return result
}

func classifyNetError(err error) *model.HostError {
	if err == nil {
		return nil
	}
	errStr := err.Error()
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline exceeded") {
		return &model.HostError{
			Code:    "TIMEOUT",
			Message: fmt.Sprintf("Tiempo de espera agotado al conectar por SSH: %v", err),
		}
	}
	return &model.HostError{
		Code:    "UNREACHABLE",
		Message: fmt.Sprintf("No se pudo alcanzar el host por SSH: %v", err),
	}
}

func shellEscape(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
