package httprunner

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"time"
)

type HttpJob struct {
	ID              string            `json:"id"`
	CheckID         string            `json:"checkId"`
	URL             string            `json:"url"`
	Method          string            `json:"method"`
	Headers         map[string]string `json:"headers,omitempty"`
	Body            string            `json:"body,omitempty"`
	IntervalS       int               `json:"intervalS"`
	TimeoutMs       int               `json:"timeoutMs"`
	ExpectedStatus  []int             `json:"expectedStatus"`
	Keyword         string            `json:"keyword,omitempty"`
	FollowRedirects bool              `json:"followRedirects"`
	VerifyTLS       bool              `json:"verifyTls"`
}

type HttpTimings struct {
	DNSMs     int `json:"dnsMs"`
	ConnectMs int `json:"connectMs"`
	TLSMs     int `json:"tlsMs"`
	TTFBMs    int `json:"ttfbMs"`
}

type HttpError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type HttpResult struct {
	CheckID       string       `json:"checkId"`
	At            string       `json:"at"`
	OK            bool         `json:"ok"`
	StatusCode    int          `json:"statusCode"`
	LatencyMs     int          `json:"latencyMs"`
	Timings       *HttpTimings `json:"timings,omitempty"`
	CertExpiresAt *string      `json:"certExpiresAt,omitempty"`
	Error         *HttpError   `json:"error,omitempty"`
}

func ExecuteHttpCheck(ctx context.Context, job HttpJob) HttpResult {
	now := time.Now().UTC()
	result := HttpResult{
		CheckID: job.CheckID,
		At:      now.Format(time.RFC3339Nano),
	}

	timeout := time.Duration(job.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var dnsStart, dnsDone, connStart, connDone, tlsStart, tlsDone, firstByte time.Time
	var certExpiry *time.Time

	trace := &httptrace.ClientTrace{
		DNSStart: func(_ httptrace.DNSStartInfo) {
			dnsStart = time.Now()
		},
		DNSDone: func(_ httptrace.DNSDoneInfo) {
			dnsDone = time.Now()
		},
		ConnectStart: func(_, _ string) {
			connStart = time.Now()
		},
		ConnectDone: func(_, _ string, _ error) {
			connDone = time.Now()
		},
		TLSHandshakeStart: func() {
			tlsStart = time.Now()
		},
		TLSHandshakeDone: func(state tls.ConnectionState, _ error) {
			tlsDone = time.Now()
			if len(state.PeerCertificates) > 0 {
				exp := state.PeerCertificates[0].NotAfter.UTC()
				certExpiry = &exp
			}
		},
		GotFirstResponseByte: func() {
			firstByte = time.Now()
		},
	}

	method := strings.ToUpper(strings.TrimSpace(job.Method))
	if method == "" {
		method = http.MethodGet
	}

	var bodyReader io.Reader
	if job.Body != "" && (method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch) {
		bodyReader = strings.NewReader(job.Body)
	}

	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(reqCtx, trace), method, job.URL, bodyReader)
	if err != nil {
		result.OK = false
		result.Error = &HttpError{
			Code:    "PARSE_ERROR",
			Message: fmt.Sprintf("URL o petición inválida: %v", err),
		}
		return result
	}

	for k, v := range job.Headers {
		req.Header.Set(k, v)
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   timeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   timeout,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: !job.VerifyTLS,
		},
	}

	client := &http.Client{
		Transport: transport,
	}

	if !job.FollowRedirects {
		client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}
	} else {
		client.CheckRedirect = func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("demasiadas redirecciones (máximo 5)")
			}
			return nil
		}
	}

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)
	result.LatencyMs = int(latency.Milliseconds())

	// Timings detallados
	timings := &HttpTimings{}
	if !dnsStart.IsZero() && !dnsDone.IsZero() {
		timings.DNSMs = int(dnsDone.Sub(dnsStart).Milliseconds())
	}
	if !connStart.IsZero() && !connDone.IsZero() {
		timings.ConnectMs = int(connDone.Sub(connStart).Milliseconds())
	}
	if !tlsStart.IsZero() && !tlsDone.IsZero() {
		timings.TLSMs = int(tlsDone.Sub(tlsStart).Milliseconds())
	}
	if !firstByte.IsZero() {
		timings.TTFBMs = int(firstByte.Sub(start).Milliseconds())
	}
	result.Timings = timings

	if certExpiry != nil {
		expStr := certExpiry.Format(time.RFC3339)
		result.CertExpiresAt = &expStr
	}

	if err != nil {
		result.OK = false
		result.Error = classifyError(err, reqCtx)
		return result
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode

	// Validar expectedStatus
	if len(job.ExpectedStatus) > 0 {
		statusMatches := false
		for _, exp := range job.ExpectedStatus {
			if resp.StatusCode == exp {
				statusMatches = true
				break
			}
		}
		if !statusMatches {
			result.OK = false
			result.Error = &HttpError{
				Code:    "UNEXPECTED_STATUS",
				Message: fmt.Sprintf("Código HTTP %d no esperado", resp.StatusCode),
			}
			return result
		}
	} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.OK = false
		result.Error = &HttpError{
			Code:    "UNEXPECTED_STATUS",
			Message: fmt.Sprintf("Código HTTP %d no exitoso", resp.StatusCode),
		}
		return result
	}

	// Validar keyword si fue configurada (máximo 1 MB leído)
	if strings.TrimSpace(job.Keyword) != "" {
		bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
		if readErr != nil {
			result.OK = false
			result.Error = &HttpError{
				Code:    "KEYWORD_NOT_FOUND",
				Message: fmt.Sprintf("Error leyendo cuerpo de respuesta: %v", readErr),
			}
			return result
		}
		if !strings.Contains(string(bodyBytes), job.Keyword) {
			result.OK = false
			result.Error = &HttpError{
				Code:    "KEYWORD_NOT_FOUND",
				Message: fmt.Sprintf("Keyword '%s' no encontrada en respuesta", job.Keyword),
			}
			return result
		}
	}

	result.OK = true
	return result
}

func classifyError(err error, ctx context.Context) *HttpError {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || osIsTimeout(err) {
		return &HttpError{
			Code:    "TIMEOUT",
			Message: "La petición excedió el tiempo límite configurado",
		}
	}

	errMsg := err.Error()

	if strings.Contains(errMsg, "certificate") || strings.Contains(errMsg, "tls:") || strings.Contains(errMsg, "handshake") {
		return &HttpError{
			Code:    "TLS_ERROR",
			Message: fmt.Sprintf("Error en negociación TLS/SSL: %v", err),
		}
	}

	if strings.Contains(errMsg, "no such host") || strings.Contains(errMsg, "lookup") {
		return &HttpError{
			Code:    "DNS_ERROR",
			Message: fmt.Sprintf("Error resolviendo nombre DNS: %v", err),
		}
	}

	if strings.Contains(errMsg, "connection refused") || strings.Contains(errMsg, "refused") {
		return &HttpError{
			Code:    "CONNECTION_REFUSED",
			Message: "Conexión rechazada por el servidor destino",
		}
	}

	return &HttpError{
		Code:    "NETWORK_ERROR",
		Message: errMsg,
	}
}

func osIsTimeout(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}
