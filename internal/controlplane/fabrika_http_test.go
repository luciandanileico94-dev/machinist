package controlplane

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProxyFabrikaSettings(t *testing.T) {
	for _, test := range []struct {
		name, method, input, wantBody string
		status, wantStatus            int
	}{
		{name: "reads settings", method: http.MethodGet, status: http.StatusOK, wantStatus: http.StatusOK, wantBody: `{"stages":{}}`},
		{name: "saves settings", method: http.MethodPut, input: `{"router":{}}`, status: http.StatusUnprocessableEntity, wantStatus: http.StatusUnprocessableEntity, wantBody: `{"error":"invalid settings"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != test.method || r.URL.Path != "/api/settings" {
					t.Errorf("upstream request = %s %s", r.Method, r.URL.Path)
				}
				if test.method == http.MethodPut {
					body, err := io.ReadAll(r.Body)
					if err != nil || string(body) != test.input || r.Header.Get("Content-Type") != "application/json" {
						t.Errorf("upstream body/header = %q / %q (err %v)", body, r.Header.Get("Content-Type"), err)
					}
				}
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.wantBody)
			}))
			defer upstream.Close()

			request := httptest.NewRequest(test.method, "/api/v1/fabrika/settings", strings.NewReader(test.input))
			response := httptest.NewRecorder()
			proxyFabrikaSettings(response, request, upstream.URL+"/api/settings")
			if response.Code != test.wantStatus || strings.TrimSpace(response.Body.String()) != test.wantBody {
				t.Fatalf("response = %d %q", response.Code, response.Body.String())
			}
		})
	}
}
