package main

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"io/ioutil"
	"net"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/go-sql-driver/mysql"
)

// WsrepStatus represents the state of the wsrep instance
type WsrepStatus int

// DatabaseStatus represents the overall state of the database
type DatabaseStatus int

const (
	databaseMaxOpenConns    = 5
	databaseConnMaxLifetime = time.Minute * 5

	// WsrepLocalStateQuery returns status of local wsrep instance
	WsrepLocalStateQuery = "SHOW STATUS LIKE 'wsrep_local_state';"
	// ReadOnlyQuery determines if node is in read-only mode
	ReadOnlyQuery = "SHOW GLOBAL VARIABLES LIKE 'read_only';"

	// Joining means the node is in process of joining the cluster
	Joining WsrepStatus = 1
	// Donor means the node is providing SST to a joining node
	Donor WsrepStatus = 2
	// Joined means the node has received the SST but is not synced yet
	Joined WsrepStatus = 3 //nolint // Not explicitly used yet, but here for reference
	// Synced means the node is in the cluster and fully operational
	Synced WsrepStatus = 4

	// Available means the node is ready to serve requests
	Available DatabaseStatus = 1
	// ReadOnly means the node is in read-only mode
	ReadOnly DatabaseStatus = 2
	// NotReady means the node is not available or not ready to serve requests
	NotReady DatabaseStatus = 3
	// Unavailable means we are unable to connect to the node
	Unavailable DatabaseStatus = 4
)

// OpenDatabase initializes the DB object for health check queries.
func OpenDatabase() {
	var err error
	dsnConfig := buildDSNConfig()

	if logger.IsLevelEnabled(logrus.DebugLevel) {
		sanitizedDsn := dsnConfig.Clone()
		sanitizedDsn.Passwd = "<redacted>"
		logger.Debug(fmt.Sprintf("Constructed DSN for MySQL: %s", sanitizedDsn.FormatDSN()))
	}

	db, err = sql.Open("mysql", dsnConfig.FormatDSN())
	if err != nil {
		logger.Fatal(err)
	}

	db.SetMaxOpenConns(databaseMaxOpenConns)
	db.SetConnMaxLifetime(databaseConnMaxLifetime)
}

// buildDSNConfig constructs a mysql.Config instance from the provided application connection config.
func buildDSNConfig() *mysql.Config {
	dsnConfig := mysql.NewConfig()
	dsnConfig.Params = make(map[string]string)

	if config.IsSet("connection.unix_socket") {
		dsnConfig.Net = "unix"
		dsnConfig.Addr = config.GetString("connection.unix_socket")
	} else {
		dsnConfig.Net = "tcp"
		if config.IsSet("connection.port") {
			dsnConfig.Addr = net.JoinHostPort(config.GetString("connection.host"), config.GetString("connection.port"))
		} else {
			dsnConfig.Addr = config.GetString("connection.host")
		}
	}

	if config.IsSet("connection.user") {
		dsnConfig.User = config.GetString("connection.user")
	}

	if config.IsSet("connection.password") {
		dsnConfig.Passwd = config.GetString("connection.password")
	}

	if config.GetBool("connection.tls.skip-verify") {
		// Enable SSL but skip TLS verification
		dsnConfig.TLSConfig = "skip-verify"
	} else {
		if config.IsSet("connection.tls.ca") {
			// Full TLS is enabled with custom CA
			tlsConfig := buildTLSConfig()
			err := mysql.RegisterTLSConfig("custom", tlsConfig)
			if err != nil {
				logger.Fatalf("Failed to register custom TLS configuration: %v", err)
			}
			dsnConfig.TLSConfig = "custom"
		} else if config.GetBool("connection.tls.required") {
			// Full TLS is enabled
			dsnConfig.TLSConfig = "true"
		}
	}

	dsnConfig.Timeout = time.Second

	return dsnConfig
}

// buildTLSConfig creates a tls.Config instance from the provided application TLS config.
func buildTLSConfig() *tls.Config {
	var tlsConfig tls.Config

	cnxTLSCfg := config.Sub("connection.tls")
	rootCertPool := x509.NewCertPool()

	if cnxTLSCfg.IsSet("ca") {
		pem, err := ioutil.ReadFile(cnxTLSCfg.GetString("ca"))
		if err != nil {
			logger.Error(err)
		}
		if ok := rootCertPool.AppendCertsFromPEM(pem); !ok {
			logger.Error("Failed to append PEM.")
		} else {
			tlsConfig.RootCAs = rootCertPool
		}
	}

	if cnxTLSCfg.IsSet("cert") && cnxTLSCfg.IsSet("key") {
		certs, err := tls.LoadX509KeyPair(cnxTLSCfg.GetString("cert"), cnxTLSCfg.GetString("key"))
		if err != nil {
			logger.Error(err)
		}

		clientCert := make([]tls.Certificate, 0, 1)
		clientCert = append(clientCert, certs)

		tlsConfig.Certificates = clientCert
	}

	return &tlsConfig
}

// GetDatabaseStatus performs a health check on the database server and returns an int type enumerating the specific state.
func GetDatabaseStatus() DatabaseStatus {
	err := db.Ping()
	if err != nil {
		logger.Error(err)
		return Unavailable
	}
	wsrepState := getWsrepLocalState()
	if wsrepState == Synced || (wsrepState == Donor && config.GetBool("options.available_when_donor")) {
		if !config.GetBool("options.available_when_readonly") && isReadOnly() {
			return ReadOnly
		}

		return Available
	}

	return NotReady
}

// getWsrepLocalState queries the wsrep_local_state status from the database server and returns an int type enumerating the specific state.
func getWsrepLocalState() WsrepStatus {
	stmtOut, err := db.Prepare(WsrepLocalStateQuery)
	if err != nil {
		logger.Errorf("Error preparing wsrep_local_state query: %v", err)
		return Joining
	}
	defer func() {
		if err := stmtOut.Close(); err != nil {
			logger.Errorf("Error closing prepared statement: %v", err)
		}
	}()

	var variable string

	var value int

	err = stmtOut.QueryRow().Scan(&variable, &value)
	if err != nil {
		logger.Errorf("Error executing wsrep_local_state query: %v", err)
		return Joining
	}

	return WsrepStatus(value)
}

// isReadOnly queries the global variable read_only from the database server and returns whether the server is in read-only mode.
func isReadOnly() bool {
	stmtOut, err := db.Prepare(ReadOnlyQuery)
	if err != nil {
		logger.Errorf("Error preparing read_only query: %v", err)
	}
	defer func() {
		if err := stmtOut.Close(); err != nil {
			logger.Errorf("Error closing prepared statement: %v", err)
		}
	}()

	var variable string

	var value string

	err = stmtOut.QueryRow().Scan(&variable, &value)
	if err != nil {
		logger.Errorf("Error executing read_only query: %v", err)
	}

	if value == "OFF" {
		return false
	}
	return true
}
