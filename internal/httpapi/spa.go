package httpapi

import (
	"bytes"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

type spaHandler struct {
	api        http.Handler
	files      fs.FS
	fileServer http.Handler
	index      []byte
}

func NewSPAHandler(api http.Handler, files fs.FS) (http.Handler, error) {
	if api == nil || files == nil {
		return nil, errors.New("API handler and web files are required")
	}
	index, err := fs.ReadFile(files, "index.html")
	if err != nil || len(index) == 0 {
		return nil, errors.New("web index is required")
	}
	return &spaHandler{api: api, files: files, fileServer: http.FileServer(http.FS(files)), index: index}, nil
}

func (handler *spaHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead || reservedServerPath(request.URL.Path) {
		handler.api.ServeHTTP(response, request)
		return
	}
	relative := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
	if relative != "." {
		if info, err := fs.Stat(handler.files, relative); err == nil && !info.IsDir() {
			handler.fileServer.ServeHTTP(response, request)
			return
		}
	}
	response.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(response, request, "index.html", time.Time{}, bytes.NewReader(handler.index))
}

func reservedServerPath(requestPath string) bool {
	for _, prefix := range []string{"/api", "/health", "/sub"} {
		if requestPath == prefix || strings.HasPrefix(requestPath, prefix+"/") {
			return true
		}
	}
	return false
}
