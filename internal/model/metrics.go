package model

import "time"

type MetricItem struct {
	Metric    string  `json:"m"`
	Dimension string  `json:"d,omitempty"`
	Value     float64 `json:"v"`
}

type ServiceStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"` // running, stopped, starting, stopping, unknown
}

type HostInventory struct {
	OSName        string    `json:"osName,omitempty"`
	OSVersion     string    `json:"osVersion,omitempty"`
	CPUModel      string    `json:"cpuModel,omitempty"`
	CPUCores      int       `json:"cpuCores,omitempty"`
	MemTotalBytes uint64    `json:"memTotalBytes,omitempty"`
	BootTime      time.Time `json:"bootTime,omitempty"`
}

type HostError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type HostPayload struct {
	HostID      string          `json:"hostId"`
	CollectedAt time.Time       `json:"collectedAt"`
	Inventory   *HostInventory  `json:"inventory,omitempty"`
	Metrics     []MetricItem    `json:"metrics"`
	Services    []ServiceStatus `json:"services,omitempty"`
	Error       *HostError      `json:"error,omitempty"`
}

type IngestPayload struct {
	Schema      int           `json:"schema"`
	BatchID     string        `json:"batchId"`
	Source      string        `json:"source"` // "agent"
	SentAt      time.Time     `json:"sentAt"`
	Hosts       []HostPayload `json:"hosts"`
	HTTPResults []interface{} `json:"httpResults"`
}

type EnrollRequest struct {
	Token        string `json:"token"`
	Hostname     string `json:"hostname"`
	OS           string `json:"os"`
	AgentVersion string `json:"agentVersion"`
}

type EnrollResponse struct {
	AgentID   string `json:"agentId"`
	HostID    string `json:"hostId"`
	APIKey    string `json:"apiKey"`
	IntervalS int    `json:"intervalS"`
}

type AgentConfigResponse struct {
	ConfigVersion   int      `json:"configVersion"`
	IntervalS       int      `json:"intervalS"`
	WatchedServices []string `json:"watchedServices"`
}
