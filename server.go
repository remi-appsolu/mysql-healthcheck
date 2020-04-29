package main

import (
	"context"
	"net"
	"net/http"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// HTTPServerHandler encapsulates all required objects to manage an HTTP server instance.
type HTTPServerHandler struct {
	config    *viper.Viper
	dbHandler *DBHandler
	server    *http.Server
}

// NewHTTPServerHandler creates a new HTTPServerHandler with the supplied config and dbHandlers.
func NewHTTPServerHandler(config *viper.Viper, dbHandler *DBHandler) *HTTPServerHandler {
	instance := new(HTTPServerHandler)
	instance.config = config
	instance.dbHandler = dbHandler
	return instance
}

// StartServer creates and configures a new instance of an HTTP server to handle health check requests.
func (s *HTTPServerHandler) StartServer() {
	socket := net.JoinHostPort(s.config.GetString("http.addr"), s.config.GetString("http.port"))
	path := s.config.GetString("http.path")

	logrus.Debugf("Registering health check endpoint at URI path %s", path)

	router := http.NewServeMux()
	router.HandleFunc(path, s.serveHTTPHealthCheck)

	s.server = &http.Server{
		Addr:    socket,
		Handler: router,
	}

	logrus.Info("Starting HTTP server.")

	if err := s.server.ListenAndServe(); err != http.ErrServerClosed {
		logrus.Fatalf("Error opening HTTP socket: %v", err)
	}
}

// StopServer signals to the running HTTP server to complete existing requests and shut down gracefully.
func (s *HTTPServerHandler) StopServer() {
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()

	s.server.SetKeepAlivesEnabled(false)

	if err := s.server.Shutdown(ctx); err != nil {
		logrus.Fatalf("Could not gracefully shutdown the HTTP server: %v", err)
	}

	logrus.Info("HTTP server stopped.")
}

func (s *HTTPServerHandler) serveHTTPHealthCheck(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path != s.config.GetString("http.path") {
		http.NotFound(w, req)
		return
	}

	logrus.Debugf("Processing health check request from %s", req.RemoteAddr)
	w.Header().Add("Connection", "close")

	ready, msg := RunStatusCheck(s.dbHandler)
	if !ready {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	_, err := w.Write([]byte(msg))
	if err != nil {
		logrus.Errorf("Error writing data to HTTP response: %v", err)
	}
}
