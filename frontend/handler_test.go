package webapp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestDistHandlerServesAssetsAndSPARoutes(t *testing.T) {
	dist := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<html>Marvo</html>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("export const app = true")},
	}
	handler := distHandler(dist)

	tests := []struct {
		name         string
		path         string
		wantStatus   int
		wantType     string
		wantCache    string
		wantBody     string
		unwantedBody string
	}{
		{
			name:       "immutable asset",
			path:       "/assets/app.js",
			wantStatus: http.StatusOK,
			wantType:   "text/javascript; charset=utf-8",
			wantCache:  "public, max-age=31536000, immutable",
			wantBody:   "export const app = true",
		},
		{
			name:         "missing asset",
			path:         "/assets/missing.js",
			wantStatus:   http.StatusNotFound,
			wantType:     "text/plain; charset=utf-8",
			wantCache:    "no-store",
			unwantedBody: "<html>",
		},
		{
			name:       "spa route",
			path:       "/user/example/admin",
			wantStatus: http.StatusOK,
			wantType:   "text/html; charset=utf-8",
			wantCache:  "no-cache",
			wantBody:   "<html>Marvo</html>",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if contentType := response.Header().Get("Content-Type"); contentType != test.wantType {
				t.Fatalf("Content-Type = %q, want %q", contentType, test.wantType)
			}
			if cache := response.Header().Get("Cache-Control"); cache != test.wantCache {
				t.Fatalf("Cache-Control = %q, want %q", cache, test.wantCache)
			}
			body := response.Body.String()
			if test.wantBody != "" && !strings.Contains(body, test.wantBody) {
				t.Fatalf("body %q does not contain %q", body, test.wantBody)
			}
			if test.unwantedBody != "" && strings.Contains(body, test.unwantedBody) {
				t.Fatalf("body %q unexpectedly contains %q", body, test.unwantedBody)
			}
		})
	}
}

func TestDistHandlerRejectsMutation(t *testing.T) {
	handler := distHandler(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("Marvo")}})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}
