package httprunner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExecuteHttpCheck_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","data":"healthy"}`))
	}))
	defer ts.Close()

	job := HttpJob{
		CheckID:         "check-1",
		URL:             ts.URL,
		Method:          "GET",
		TimeoutMs:       2000,
		ExpectedStatus:  []int{200},
		Keyword:         `"status":"ok"`,
		FollowRedirects: true,
		VerifyTLS:       false,
	}

	result := ExecuteHttpCheck(context.Background(), job)

	if !result.OK {
		t.Fatalf("se esperaba OK=true, obtenido false, error: %v", result.Error)
	}
	if result.StatusCode != 200 {
		t.Errorf("se esperaba status 200, obtenido %d", result.StatusCode)
	}
	if result.Timings == nil {
		t.Errorf("se esperaban timings no nulos")
	}
}

func TestExecuteHttpCheck_UnexpectedStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`Internal Server Error`))
	}))
	defer ts.Close()

	job := HttpJob{
		CheckID:        "check-2",
		URL:            ts.URL,
		Method:         "GET",
		TimeoutMs:      2000,
		ExpectedStatus: []int{200},
	}

	result := ExecuteHttpCheck(context.Background(), job)

	if result.OK {
		t.Fatalf("se esperaba OK=false para status 500")
	}
	if result.Error == nil || result.Error.Code != "UNEXPECTED_STATUS" {
		t.Errorf("se esperaba código UNEXPECTED_STATUS, obtenido: %v", result.Error)
	}
}

func TestExecuteHttpCheck_KeywordNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"error":"maintenance"}`))
	}))
	defer ts.Close()

	job := HttpJob{
		CheckID:        "check-3",
		URL:            ts.URL,
		Method:         "GET",
		TimeoutMs:      2000,
		ExpectedStatus: []int{200},
		Keyword:        `"status":"ok"`,
	}

	result := ExecuteHttpCheck(context.Background(), job)

	if result.OK {
		t.Fatalf("se esperaba OK=false por falta de keyword")
	}
	if result.Error == nil || result.Error.Code != "KEYWORD_NOT_FOUND" {
		t.Errorf("se esperaba código KEYWORD_NOT_FOUND, obtenido: %v", result.Error)
	}
}

func TestExecuteHttpCheck_Timeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	job := HttpJob{
		CheckID:   "check-4",
		URL:       ts.URL,
		Method:    "GET",
		TimeoutMs: 100, // Menor al sleep de 300ms
	}

	result := ExecuteHttpCheck(context.Background(), job)

	if result.OK {
		t.Fatalf("se esperaba OK=false por timeout")
	}
	if result.Error == nil || result.Error.Code != "TIMEOUT" {
		t.Errorf("se esperaba código TIMEOUT, obtenido: %v", result.Error)
	}
}
