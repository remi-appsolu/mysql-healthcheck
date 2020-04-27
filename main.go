package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

var (
	// appName name of this application
	appName = "mysql-healthcheck"
	// version of this application - this will be set using ldflags during compile time
	version = "devel"
	db      *sql.DB
	config  *viper.Viper
	server  *http.Server
	logger  = logrus.New()
)

var daemonMode = flag.Bool("d", false, "Run as a daemon and listen for HTTP connections on a socket")
var logVerbose = flag.Bool("v", false, "Verbose (debug) logging")
var printVersion = flag.Bool("V", false, "Print version and exit")

func main() {

	flag.Parse()

	if *printVersion {
		fmt.Printf("%s %s, compiled for %s %s using %s", appName, version, runtime.GOOS, runtime.GOARCH, runtime.Version())
		os.Exit(0)
	}

	if *logVerbose {
		logger.SetLevel(logrus.DebugLevel)
	}

	switch *daemonMode {
	case true:
		runDaemon()
	default:
		runStandaloneHealthCheck()
	}
}

// runDaemon initializes config and database objects, listens for OS signals and starts an HTTP server instance.
func runDaemon() {
	sigs := make(chan os.Signal, 1)
	shutdown := false
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	go func() {
		for {
			s := <-sigs
			logger.Debugf("Received %s signal", s)
			switch s {
			case syscall.SIGHUP:
				logger.Info("Triggering reload of config, database connections and HTTP server...")
				stopServer()
			case syscall.SIGINT, syscall.SIGTERM:
				shutdown = true
				stopServer()

			}
		}
	}()

	for !shutdown {
		CreateConfig()
		OpenDatabase()
		// startServer() will spawn
		startServer()
		// db.Close() will only run once HTTP server has shut down - either for reload or due to termination
		db.Close()
	}
}

// startServer spawns an HTTP server and listens indefinitely until the server is shut down
func startServer() {
	socket := net.JoinHostPort(config.GetString("http.addr"), config.GetString("http.port"))
	server = createHTTPServer(socket)
	logger.Info("Starting HTTP server.")
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		logger.Fatalf("Error opening HTTP socket: %v", err)
	}
}

// stopServer signals to the running HTTP server to complete existing requests and shut down
func stopServer() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	server.SetKeepAlivesEnabled(false)
	if err := server.Shutdown(ctx); err != nil {
		logger.Fatalf("Could not gracefully shutdown the HTTP server: %v\n", err)
	}
	logger.Info("HTTP server stopped.")
}

// createHTTPServer creates and configures a new instance of an HTTP server
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
	CreateConfig()
	OpenDatabase()
	logger.Debug("Running standalone health check.")
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
}
