package main

import (
	"context"
	"database/sql"
	"flag"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

var (
	db     *sql.DB
	config *viper.Viper
	server *http.Server
	logger = logrus.New()
)

var daemon = flag.Bool("d", false, "Run as a daemon and listen for HTTP connections on a socket")
var verbose = flag.Bool("v", false, "verbose")

func main() {
	logFile, err := os.OpenFile("/var/log/mysql-healthcheck.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err == nil {
		logger.Out = io.MultiWriter(os.Stdout, logFile)
	}

	flag.Parse()

	if *verbose {
		logger.SetLevel(logrus.DebugLevel)
	}

	switch *daemon {
	case true:
		runDaemon()
	default:
		logger.Debug("Running in standalone mode.")
		runStandaloneHealthCheck()
	}
}

func runDaemon() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	go func() {
		for {
			s := <-sigs
			logger.Debugf("Received %s signal", s)
			switch s {
			case syscall.SIGHUP:
				logger.Info("Triggering reload of config, database connections and HTTP server...")
				stopDaemon()
			case syscall.SIGINT, syscall.SIGTERM:
				stopDaemon()
				os.Exit(0)
			}
		}
	}()

	for true {
		LoadConfig()
		OpenDatabase()
		startDaemon()
	}
}

func startDaemon() {
	socket := net.JoinHostPort(config.GetString("http.addr"), config.GetString("http.port"))
	server = createHTTPServer(socket)
	logger.Info("Starting HTTP server.")
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		logger.Fatalf("Error opening HTTP socket: %v", err)
	}
}

func stopDaemon() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	server.SetKeepAlivesEnabled(false)
	if err := server.Shutdown(ctx); err != nil {
		logger.Fatalf("Could not gracefully shutdown the HTTP server: %v\n", err)
	}
	logger.Info("HTTP server stopped.")
}

func createHTTPServer(socket string) *http.Server {
	path := config.GetString("http.path")
	logger.Debugf("Registering health check endpoint at URI path %s", path)
	router := http.NewServeMux()
	router.HandleFunc(path, serveHTTPHealthCheck)
	return &http.Server{
		Addr:    socket,
		Handler: router,
	}
}

func runStandaloneHealthCheck() {
	LoadConfig()
	OpenDatabase()
	logger.Debug("Processing standalone health check.")
	switch RunStatusCheck() {
	case Available:
		logger.Info("MySQL cluster node is ready.")
		os.Exit(0)
	case Unavailable:
		logger.Warn("Could not connect to the MySQL cluster node.")
		os.Exit(1)
	case ReadOnly:
		logger.Warn("MySQL cluster node is read-only.")
		os.Exit(2)
	case NotReady:
		logger.Warn("MySQL cluster node is not ready.")
		os.Exit(3)
	}
}

func serveHTTPHealthCheck(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path != config.GetString("http.path") {
		http.NotFound(w, req)
		return
	}
	logger.Debugf("Processing health check request from %s", req.RemoteAddr)
	w.Header().Add("Connection", "close")
	switch RunStatusCheck() {
	case Available:
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("MySQL cluster node is ready."))
	case Unavailable:
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("Could not connect to the MySQL cluster node."))
	case ReadOnly:
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("MySQL cluster node is read-only."))
	case NotReady:
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("MySQL cluster node is not ready."))
	}
	w.Header().Add("Connection", "close")
}
