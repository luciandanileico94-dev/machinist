package controlplane

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"time"
)

func (s *Server) fabrikaSettings(w http.ResponseWriter, r *http.Request, _ string) {
	proxyFabrikaSettings(w, r, fabrikaSettingsURL())
}

func fabrikaSettingsURL() string {
	if target := os.Getenv("FABRIKA_SETTINGS_URL"); target != "" {
		return target
	}
	return "http://127.0.0.1:8420/api/settings"
}

func proxyFabrikaSettings(w http.ResponseWriter, r *http.Request, target string) {
	var body io.Reader
	if r.Method == http.MethodPut {
		limited := http.MaxBytesReader(w, r.Body, 20_000)
		defer limited.Close()
		data, err := io.ReadAll(limited)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		body = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(r.Context(), r.Method, target, body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if r.Method == http.MethodPut {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	defer response.Body.Close()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(response.Body, 20_001))
}
