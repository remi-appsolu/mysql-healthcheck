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

const (
	// AppName defines the name this application
	AppName = "mysql-healthcheck"
	// httpTimeout defines timeout period when gracefully shutting down HTTP server
	httpTimeout = 30 * time.Second
)

var (
	// version of this application - changed during compile time using ldflags
	version = "DEV-snapshot"
	db      *sql.DB
	config  *viper.Viper
	server  *http.Server
	logger  = logrus.New()
)

func main() {
	daemonMode := flag.Bool("d", false, "Run as a daemon and listen for HTTP connections on a socket")
	logVerbose := flag.Bool("v", false, "Verbose (debug) logging")
	printVersion := flag.Bool("V", false, "Print version and exit")
	flag.Parse()

	if *printVersion {
		fmt.Printf("%s version %s, compiled for %s %s using %s\n", AppName, version, runtime.GOOS, runtime.GOARCH, runtime.Version())
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
	shutdown := false

	sigs := make(chan os.Signal, 1)
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

// startServer spawns an HTTP server and listens indefinitely until the server is shut down.
func startServer() {
	socket := net.JoinHostPort(config.GetString("http.addr"), config.GetString("http.port"))
	server = createHTTPServer(socket)

	logger.Info("Starting HTTP server.")

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		logger.Fatalf("Error opening HTTP socket: %v", err)
	}
}

// stopServer signals to the running HTTP server to complete existing requests and shut down gracefully.
func stopServer() {
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()

	server.SetKeepAlivesEnabled(false)

	if err := server.Shutdown(ctx); err != nil {
		logger.Fatalf("Could not gracefully shutdown the HTTP server: %v\n", err)
	}

	logger.Info("HTTP server stopped.")
}

// createHTTPServer creates and configures a new instance of an HTTP server to handle health check requests.
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

// runStatusCheck queries the current state of the database and returns a boolean and status message indicating if the database is available.
func runStatusCheck() (bool, string) {
	switch GetDatabaseStatus() {
	case Available:
		return true, "MySQL cluster node is ready."
	case Unavailable:
		return false, "Could not connect to the MySQL cluster node."
	case ReadOnly:
		return false, "MySQL cluster node is read-only."
	case NotReady:
		return false, "MySQL cluster node is not ready."
	}
	return false, "Unknown error encountered running health check."
}

// runStandaloneHealthCheck runs a single health check against the target database and returns the result via log messages and os.Exit().
func runStandaloneHealthCheck() {
	CreateConfig()
	OpenDatabase()
	logger.Debug("Running standalone health check.")

	ready, msg := runStatusCheck()

	if ready {
		logger.Info(msg)
		os.Exit(0)
	} else {
		logger.Warn(msg)
		os.Exit(1)
	}
}

// serveHTTPHealthCheck responds to requests for health checks received by the HTTP server.
func serveHTTPHealthCheck(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path != config.GetString("http.path") {
		http.NotFound(w, req)
		return
	}

	logger.Debugf("Processing health check request from %s", req.RemoteAddr)
	w.Header().Add("Connection", "close")

	ready, msg := runStatusCheck()

	if !ready {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	_, err := w.Write([]byte(msg))
	if err != nil {
		logger.Errorf("Error writing data to HTTP response: %v", err)
	}
}
