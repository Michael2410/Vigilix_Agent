package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"vigilix-agent/internal/model"
)

type APIClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewAPIClient(baseURL, apiKey string) *APIClient {
	cleanURL := strings.TrimRight(baseURL, "/")
	return &APIClient{
		baseURL: cleanURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *APIClient) SetAPIKey(apiKey string) {
	c.apiKey = apiKey
}

func (c *APIClient) Enroll(ctx context.Context, req *model.EnrollRequest) (*model.EnrollResponse, error) {
	url := fmt.Sprintf("%s/v1/agents/enroll", c.baseURL)

	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("error serializando enroll request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("error conectando con el servidor en %s: %w", url, err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("servidor respondió con status %d: %s", resp.StatusCode, string(respBytes))
	}

	var enrollResp model.EnrollResponse
	if err := json.Unmarshal(respBytes, &enrollResp); err != nil {
		return nil, fmt.Errorf("error deserializando respuesta de enroll: %w", err)
	}

	return &enrollResp, nil
}

func (c *APIClient) GetConfig(ctx context.Context) (*model.AgentConfigResponse, error) {
	url := fmt.Sprintf("%s/v1/agents/config", c.baseURL)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("error consultando config del agente: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("error obteniendo config (status %d): %s", resp.StatusCode, string(respBytes))
	}

	var cfgResp model.AgentConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&cfgResp); err != nil {
		return nil, fmt.Errorf("error deserializando config response: %w", err)
	}

	return &cfgResp, nil
}

func (c *APIClient) SendIngest(ctx context.Context, payload *model.IngestPayload) error {
	url := fmt.Sprintf("%s/v1/ingest", c.baseURL)

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("error serializando payload de ingesta: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("error enviando ingesta al servidor: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		respBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ingesta rechazada (status %d): %s", resp.StatusCode, string(respBytes))
	}

	return nil
}
