package httpapi

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestSPAHandlerServesAssetsAndFrontendFallbackWithoutMaskingAPI(t *testing.T) {
	files := fstest.MapFS{
		"index.html":    {Data: []byte("<main>管理中心</main>")},
		"assets/app.js": {Data: []byte("console.log('app')")},
	}
	api := http.NewServeMux()
	api.HandleFunc("GET /api/known", func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusNoContent) })
	handler, err := NewSPAHandler(api, fs.FS(files))
	if err != nil {
		t.Fatalf("NewSPAHandler() error = %v", err)
	}

	for _, test := range []struct {
		path   string
		status int
		body   string
	}{
		{"/", http.StatusOK, "<main>管理中心</main>"},
		{"/users/123", http.StatusOK, "<main>管理中心</main>"},
		{"/assets/app.js", http.StatusOK, "console.log('app')"},
		{"/api/known", http.StatusNoContent, ""},
		{"/api/missing", http.StatusNotFound, "404 page not found\n"},
		{"/health/missing", http.StatusNotFound, "404 page not found\n"},
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))
		if recorder.Code != test.status || recorder.Body.String() != test.body {
			t.Errorf("GET %s = (%d,%q), want (%d,%q)", test.path, recorder.Code, recorder.Body.String(), test.status, test.body)
		}
	}
}

func TestSPAHandlerRejectsMissingIndex(t *testing.T) {
	if _, err := NewSPAHandler(http.NotFoundHandler(), fstest.MapFS{}); err == nil {
		t.Fatal("NewSPAHandler() error = nil")
	}
	if _, err := NewSPAHandler(nil, fstest.MapFS{"index.html": {Data: []byte("ok")}}); err == nil {
		t.Fatal("nil API error = nil")
	}
}
