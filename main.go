package main

import (
	"database/sql"
	"flag"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	// AppName defines the name this application
	AppName = "mysql-healthcheck"
	// httpTimeout defines timeout period when gracefully shutting down HTTP server
	httpTimeout = 30 * time.Second
	// version of this application - changed during compile time using ldflags
)

var version = "DEV-snapshot"

func main() {
	daemonMode := flag.Bool("d", false, "Run as a daemon and listen for HTTP connections on a socket")
	logVerbose := flag.Bool("v", false, "Verbose (debug) logging")
	printVersion := flag.Bool("V", false, "Print version and exit")
	flag.Parse()

	if *printVersion {
		logrus.SetLevel(logrus.InfoLevel)
		logrus.Infof("%s version %s, compiled for %s %s using %s\n", AppName,
			version, runtime.GOOS, runtime.GOARCH, runtime.Version())
		os.Exit(0)
	}

	if *logVerbose {
		logrus.SetLevel(logrus.DebugLevel)
	}

	switch *daemonMode {
	case true:
		runDaemon()
	default:
		runStandaloneHealthCheck()
	}
}

// runDaemon starts an HTTP server instance and listens for OS signals.
func runDaemon() {
	var httpHandler *HTTPServerHandler

	shutdown := false

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	go func() {
		for {
			s := <-sigs

			logrus.Debugf("Received %s signal", s)

			switch s {
			case syscall.SIGHUP:
				logrus.Info("Triggering reload of config, database connections and HTTP server...")

				httpHandler.StopServer()
			case syscall.SIGINT, syscall.SIGTERM:
				shutdown = true

				httpHandler.StopServer()
			}
		}
	}()

	for !shutdown {
		config := CreateConfig()
		dsn := BuildDSN(config)

		db, err := sql.Open("mysql", dsn)
		if err != nil {
			logrus.Fatal(err)
		}

		defer func() {
			err := db.Close()
			if err != nil {
				logrus.Fatalf("Error closing the database connection: %v", err)
			}
		}()

		dbHandler := CreateDBHandler(config, db)
		httpHandler = NewHTTPServerHandler(config, dbHandler)

		httpHandler.StartServer()

		// HTTPHandler blocks here on HTTP server execution.  Next line will run
		// only after the HTTP server is shutdown.

		err = db.Close()
		if err != nil {
			logrus.Fatalf("Error closing the database connection: %v", err)
		}
	}
}

// RunStatusCheck queries the current state of the database and returns a boolean
// and status message indicating if the database is available.
func RunStatusCheck(dbHandler *DBHandler) (bool, string) {
	switch dbHandler.GetStatus() {
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

// runStandaloneHealthCheck runs a single health check against the target database
// and returns the result via log messages and os.Exit().
func runStandaloneHealthCheck() bool {
	config := CreateConfig()
	dsn := BuildDSN(config)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		logrus.Fatal(err)
	}

	defer func() {
		err := db.Close()
		if err != nil {
			logrus.Fatalf("Error closing the database connection: %v", err)
		}
	}()

	dbHandler := CreateDBHandler(config, db)

	logrus.Debug("Running standalone health check.")

	ready, msg := RunStatusCheck(dbHandler)

	if ready {
		logrus.Info(msg)
	} else {
		logrus.Warn(msg)
	}

	return ready
}
