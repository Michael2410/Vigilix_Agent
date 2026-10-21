package collector

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"vigilix-agent/internal/httprunner"
	"vigilix-agent/internal/model"
	"vigilix-agent/internal/probes/ssh"
	"vigilix-agent/internal/probes/winrm"
)

type Config struct {
	ServerURL string
	Token     string
	Version   string
}

type BaseJob struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type HostPollJob struct {
	ID              string             `json:"id"`
	Type            string             `json:"type"`
	HostID          string             `json:"hostId"`
	OS              string             `json:"os"`
	Address         string             `json:"address"`
	Port            int                `json:"port"`
	TLS             bool               `json:"tls"`
	IntervalS       int                `json:"intervalS"`
	TimeoutMs       int                `json:"timeoutMs"`
	WatchedServices []string           `json:"watchedServices"`
	SSHHostKey      *string            `json:"sshHostKey"`
	Credential      ssh.CredentialInfo `json:"credential"`
}

type JobsResponse struct {
	Version int               `json:"version"`
	Jobs    []json.RawMessage `json:"jobs"`
}

type HeartbeatPayload struct {
	Schema       int            `json:"schema"`
	Version      string         `json:"version"`
	UptimeS      int            `json:"uptimeS"`
	JobsTotal    int            `json:"jobsTotal"`
	JobsLate     int            `json:"jobsLate"`
	BufferBytes  int            `json:"bufferBytes"`
	ErrorsByCode map[string]int `json:"errorsByCode"`
}

type IngestPayload struct {
	Schema      int                     `json:"schema"`
	BatchID     string                  `json:"batchId"`
	Source      string                  `json:"source"`
	SentAt      string                  `json:"sentAt"`
	Hosts       []model.HostPayload     `json:"hosts"`
	HttpResults []httprunner.HttpResult `json:"httpResults"`
}

type Scheduler struct {
	cfg        Config
	client     *http.Client
	logger     *slog.Logger
	startTime  time.Time
	lastEtag   string
	jobsMu     sync.RWMutex
	activeJobs map[string]context.CancelFunc
	errCounts  map[string]int
	errMu      sync.Mutex

	trackersMu sync.Mutex
	trackers   map[string]*DeltaTracker
}

func NewScheduler(cfg Config, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		cfg: cfg,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		logger:     logger,
		startTime:  time.Now(),
		activeJobs: make(map[string]context.CancelFunc),
		errCounts:  make(map[string]int),
		trackers:   make(map[string]*DeltaTracker),
	}
}

func (s *Scheduler) Start(ctx context.Context) error {
	s.logger.Info("Iniciando scheduler de colector...", "server", s.cfg.ServerURL)

	// Sincronización inicial de jobs
	s.syncJobs(ctx)

	jobsTicker := time.NewTicker(30 * time.Second)
	defer jobsTicker.Stop()

	heartbeatTicker := time.NewTicker(15 * time.Second)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("Deteniendo scheduler de colector...")
			s.stopAllJobs()
			return nil
		case <-jobsTicker.C:
			s.syncJobs(ctx)
		case <-heartbeatTicker.C:
			s.sendHeartbeat(ctx)
		}
	}
}

func (s *Scheduler) stopAllJobs() {
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()
	for _, cancel := range s.activeJobs {
		cancel()
	}
	s.activeJobs = make(map[string]context.CancelFunc)
}

func (s *Scheduler) getDeltaTracker(hostID string) *DeltaTracker {
	s.trackersMu.Lock()
	defer s.trackersMu.Unlock()
	if dt, exists := s.trackers[hostID]; exists {
		return dt
	}
	dt := NewDeltaTracker()
	s.trackers[hostID] = dt
	return dt
}

func (s *Scheduler) syncJobs(ctx context.Context) {
	url := fmt.Sprintf("%s/v1/collector/jobs", stringsTrimSlash(s.cfg.ServerURL))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		s.logger.Error("Error creando request para jobs", "error", err)
		return
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.cfg.Token))
	if s.lastEtag != "" {
		req.Header.Set("If-None-Match", s.lastEtag)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		s.logger.Error("Error consultando jobs del colector", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		s.logger.Debug("Jobs sin cambios (304 Not Modified)")
		return
	}

	if resp.StatusCode != http.StatusOK {
		s.logger.Warn("Respuesta no esperada consultando jobs", "statusCode", resp.StatusCode)
		return
	}

	s.lastEtag = resp.Header.Get("ETag")

	var jobsResp JobsResponse
	if err := json.NewDecoder(resp.Body).Decode(&jobsResp); err != nil {
		s.logger.Error("Error decodificando respuesta de jobs", "error", err)
		return
	}

	s.logger.Info("Jobs actualizados desde la API", "version", jobsResp.Version, "total", len(jobsResp.Jobs))
	s.applyJobs(ctx, jobsResp.Jobs)
}

func (s *Scheduler) applyJobs(parentCtx context.Context, rawJobs []json.RawMessage) {
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()

	currentIds := make(map[string]bool)

	for _, raw := range rawJobs {
		var base BaseJob
		if err := json.Unmarshal(raw, &base); err != nil {
			s.logger.Error("Error analizando tipo de job", "error", err)
			continue
		}

		currentIds[base.ID] = true
		if _, exists := s.activeJobs[base.ID]; exists {
			continue // Ya está corriendo
		}

		jobCtx, cancel := context.WithCancel(parentCtx)
		s.activeJobs[base.ID] = cancel

		switch base.Type {
		case "http_check":
			var httpJob httprunner.HttpJob
			if err := json.Unmarshal(raw, &httpJob); err == nil {
				go s.runHttpJobLoop(jobCtx, httpJob)
			}
		case "host_poll":
			var hostJob HostPollJob
			if err := json.Unmarshal(raw, &hostJob); err == nil {
				go s.runHostPollJobLoop(jobCtx, hostJob)
			}
		default:
			s.logger.Warn("Tipo de job desconocido", "id", base.ID, "type", base.Type)
		}
	}

	// Cancelar jobs que ya no estén asignados
	for id, cancel := range s.activeJobs {
		if !currentIds[id] {
			cancel()
			delete(s.activeJobs, id)
			s.logger.Info("Job eliminado del colector", "jobId", id)
		}
	}
}

func (s *Scheduler) runHttpJobLoop(ctx context.Context, job httprunner.HttpJob) {
	interval := time.Duration(job.IntervalS) * time.Second
	if interval <= 0 {
		interval = 60 * time.Second
	}

	s.logger.Info("Iniciando runner para check HTTP", "checkId", job.CheckID, "url", job.URL, "intervalS", job.IntervalS)

	// Ejecución inmediata inicial
	s.executeHttpAndSend(ctx, job)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.executeHttpAndSend(ctx, job)
		}
	}
}

func (s *Scheduler) executeHttpAndSend(ctx context.Context, job httprunner.HttpJob) {
	result := httprunner.ExecuteHttpCheck(ctx, job)

	if !result.OK && result.Error != nil {
		s.errMu.Lock()
		s.errCounts[result.Error.Code]++
		s.errMu.Unlock()
	}

	s.logger.Debug("Resultado de check HTTP", "checkId", job.CheckID, "ok", result.OK, "status", result.StatusCode, "latencyMs", result.LatencyMs)

	if err := s.sendHttpIngest(ctx, result); err != nil {
		s.logger.Error("Error enviando resultado HTTP a ingest", "checkId", job.CheckID, "error", err)
	}
}

func (s *Scheduler) runHostPollJobLoop(ctx context.Context, job HostPollJob) {
	interval := time.Duration(job.IntervalS) * time.Second
	if interval <= 0 {
		interval = 60 * time.Second
	}

	s.logger.Info("Iniciando runner para sondeo sin agente", "hostId", job.HostID, "os", job.OS, "address", job.Address, "intervalS", job.IntervalS)

	// Ejecución inmediata inicial
	s.executeHostPollAndSend(ctx, job)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.executeHostPollAndSend(ctx, job)
		}
	}
}

func (s *Scheduler) executeHostPollAndSend(ctx context.Context, job HostPollJob) {
	pollCtx, cancel := context.WithTimeout(ctx, time.Duration(job.TimeoutMs)*time.Millisecond)
	defer cancel()

	now := time.Now().UTC()
	payload := model.HostPayload{
		HostID:      job.HostID,
		CollectedAt: now,
		Metrics:     make([]model.MetricItem, 0),
		Services:    make([]model.ServiceStatus, 0),
	}

	if job.OS == "linux" {
		sshJob := ssh.SSHJob{
			HostID:          job.HostID,
			Address:         job.Address,
			Port:            job.Port,
			TimeoutMs:       job.TimeoutMs,
			WatchedServices: job.WatchedServices,
			SSHHostKey:      job.SSHHostKey,
			Credential:      job.Credential,
		}

		res := ssh.ExecuteSSHPoll(pollCtx, sshJob)

		// TOFU: Reportar clave SSH detectada por primera vez
		if res.NewHostKey != "" {
			go s.reportSshHostKey(ctx, job.HostID, res.NewHostKey)
		}

		if res.Error != nil {
			s.errMu.Lock()
			s.errCounts[res.Error.Code]++
			s.errMu.Unlock()

			payload.Error = res.Error
			s.logger.Warn("Error en sondeo SSH", "hostId", job.HostID, "code", res.Error.Code, "msg", res.Error.Message)
		} else if res.Metrics != nil {
			dt := s.getDeltaTracker(job.HostID)

			var cpuPct *float64
			if res.Metrics.CPUTicks != nil {
				if rate, ok := dt.CalculateCPURate(res.Metrics.CPUTicks.Total(), res.Metrics.CPUTicks.IdleAll(), now); ok {
					cpuPct = &rate
				}
			}

			rxBps := make(map[string]float64)
			txBps := make(map[string]float64)
			for _, netDev := range res.Metrics.NetDevs {
				if rates, ok := dt.CalculateNetRates(netDev.Interface, netDev.RxBytes, netDev.TxBytes, now); ok {
					rxBps[netDev.Interface] = rates.RxBps
					txBps[netDev.Interface] = rates.TxBps
				}
			}

			payload.Metrics = ssh.FormatMetrics(res.Metrics, cpuPct, rxBps, txBps)
			payload.Inventory = res.Metrics.Inventory
			payload.Services = res.Metrics.Services
		}
	} else if job.OS == "windows" {
		winrmJob := winrm.WinRMJob{
			HostID:          job.HostID,
			Address:         job.Address,
			Port:            job.Port,
			TLS:             job.TLS,
			TimeoutMs:       job.TimeoutMs,
			WatchedServices: job.WatchedServices,
			Credential: winrm.CredentialInfo{
				Kind:     job.Credential.Kind,
				Username: job.Credential.Username,
				Secret:   job.Credential.Secret,
			},
		}

		res := winrm.ExecuteWinRMPoll(pollCtx, winrmJob)

		if res.Error != nil {
			s.errMu.Lock()
			s.errCounts[res.Error.Code]++
			s.errMu.Unlock()

			payload.Error = res.Error
			s.logger.Warn("Error en sondeo WinRM", "hostId", job.HostID, "code", res.Error.Code, "msg", res.Error.Message)
		} else {
			payload.Metrics = res.Metrics
			payload.Inventory = res.Inventory
			payload.Services = res.Services
		}
	}

	if payload.Metrics == nil {
		payload.Metrics = make([]model.MetricItem, 0)
	}
	if payload.Services == nil {
		payload.Services = make([]model.ServiceStatus, 0)
	}

	if err := s.sendHostIngest(ctx, payload); err != nil {
		s.logger.Error("Error enviando métricas de host a ingest", "hostId", job.HostID, "error", err)
	} else {
		s.logger.Debug("Métricas de host enviadas con éxito", "hostId", job.HostID, "metrics", len(payload.Metrics))
	}
}

func (s *Scheduler) reportSshHostKey(ctx context.Context, hostID, fingerprint string) {
	url := fmt.Sprintf("%s/v1/collector/ssh-host-key", stringsTrimSlash(s.cfg.ServerURL))
	body, _ := json.Marshal(map[string]string{
		"hostId":      hostID,
		"fingerprint": fingerprint,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		s.logger.Error("Error creando request para registrar SSH HostKey", "error", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.cfg.Token))

	resp, err := s.client.Do(req)
	if err != nil {
		s.logger.Error("Error reportando SSH HostKey TOFU a la API", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		s.logger.Info("Fingerprint SSH HostKey registrado vía TOFU exitosamente", "hostId", hostID, "fingerprint", fingerprint)
	} else {
		s.logger.Warn("API respondió código inesperado al registrar HostKey", "status", resp.StatusCode)
	}
}

func (s *Scheduler) sendHttpIngest(ctx context.Context, result httprunner.HttpResult) error {
	batchID := generateUUIDv4()
	payload := IngestPayload{
		Schema:      1,
		BatchID:     batchID,
		Source:      "agentless",
		SentAt:      time.Now().UTC().Format(time.RFC3339Nano),
		Hosts:       []model.HostPayload{},
		HttpResults: []httprunner.HttpResult{result},
	}

	return s.doIngestPost(ctx, payload)
}

func (s *Scheduler) sendHostIngest(ctx context.Context, hostPayload model.HostPayload) error {
	batchID := generateUUIDv4()
	payload := IngestPayload{
		Schema:      1,
		BatchID:     batchID,
		Source:      "agentless",
		SentAt:      time.Now().UTC().Format(time.RFC3339Nano),
		Hosts:       []model.HostPayload{hostPayload},
		HttpResults: []httprunner.HttpResult{},
	}

	return s.doIngestPost(ctx, payload)
}

func (s *Scheduler) doIngestPost(ctx context.Context, payload IngestPayload) error {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/v1/ingest", stringsTrimSlash(s.cfg.ServerURL))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.cfg.Token))

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ingesta respondió status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (s *Scheduler) sendHeartbeat(ctx context.Context) {
	uptimeS := int(time.Since(s.startTime).Seconds())

	s.jobsMu.RLock()
	totalJobs := len(s.activeJobs)
	s.jobsMu.RUnlock()

	s.errMu.Lock()
	errorsMap := make(map[string]int)
	for k, v := range s.errCounts {
		errorsMap[k] = v
	}
	s.errMu.Unlock()

	payload := HeartbeatPayload{
		Schema:       1,
		Version:      s.cfg.Version,
		UptimeS:      uptimeS,
		JobsTotal:    totalJobs,
		JobsLate:     0,
		BufferBytes:  0,
		ErrorsByCode: errorsMap,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		s.logger.Error("Error serializando heartbeat", "error", err)
		return
	}

	url := fmt.Sprintf("%s/v1/collector/heartbeat", stringsTrimSlash(s.cfg.ServerURL))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		s.logger.Error("Error creando request heartbeat", "error", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.cfg.Token))

	resp, err := s.client.Do(req)
	if err != nil {
		s.logger.Error("Error enviando heartbeat del colector", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		s.logger.Debug("Heartbeat enviado exitosamente", "uptimeS", uptimeS, "jobs", totalJobs)
	} else {
		s.logger.Warn("Heartbeat respondió status inesperado", "status", resp.StatusCode)
	}
}

func stringsTrimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func generateUUIDv4() string {
	var uuid [16]byte
	_, _ = rand.Read(uuid[:])
	uuid[6] = (uuid[6] & 0x0f) | 0x40 // Version 4
	uuid[8] = (uuid[8] & 0x3f) | 0x80 // RFC 4122 variant

	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(uuid[0:4]),
		hex.EncodeToString(uuid[4:6]),
		hex.EncodeToString(uuid[6:8]),
		hex.EncodeToString(uuid[8:10]),
		hex.EncodeToString(uuid[10:16]),
	)
}
