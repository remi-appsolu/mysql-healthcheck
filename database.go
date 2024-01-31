/*
Database.go provides the connection and health checking logic for the target database.
*/
package main

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// DBHandler encapsulates all required objects to manage a database connection and run status checks.
type DBHandler struct {
	db                    *sql.DB
	availableWhenDonor    bool
	availableWhenReadOnly bool
}

// WsrepStatus represents the state of the wsrep process on the database server.
type WsrepStatus int

// ServerStatus represents the state of the database server.
type ServerStatus int

var customQuery string
var customResult string

const (
	databaseMaxOpenConns    = 5
	databaseConnMaxLifetime = time.Minute * 5

	// wsrepLocalStateQuery returns status of local wsrep instance.
	wsrepLocalStateQuery = "SHOW STATUS LIKE 'wsrep_local_state';"
	// readOnlyQuery determines if node is in read-only mode.
	readOnlyQuery = "SHOW GLOBAL VARIABLES LIKE 'read_only';"

	// Joining means the node is in process of joining the cluster.
	Joining WsrepStatus = 1
	// Donor means the node is providing SST to a joining node.
	Donor WsrepStatus = 2
	// Joined means the node has received the SST but is not synced yet.
	Joined WsrepStatus = 3 //nolint // Not explicitly used yet, but here for reference
	// Synced means the node is in the cluster and fully operational.
	Synced WsrepStatus = 4

	// Available means the node is ready to serve requests.
	Available ServerStatus = 1
	// ReadOnly means the node is in read-only mode.
	ReadOnly ServerStatus = 2
	// NotReady means the node is not available or not ready to serve requests.
	NotReady ServerStatus = 3
	// Unavailable means we are unable to connect to the node.
	Unavailable ServerStatus = 4
)

// CreateDBHandler instantiates a new DBHandler struct to hold the database connection and associated options.
func CreateDBHandler(config *viper.Viper, db *sql.DB) *DBHandler {
	instance := new(DBHandler)
	instance.db = db
	instance.availableWhenDonor = config.GetBool("options.available_when_donor")
	instance.availableWhenReadOnly = config.GetBool("options.available_when_readonly")

	if config.IsSet("customQuery") && config.IsSet("customResult") {
		customQuery = config.GetString("customQuery")
		customResult = config.GetString("customResult")
		logrus.Info("Custom query and result configured")
	} else {
		logrus.Info("Custom query or result is empty")
	}

	instance.db.SetMaxOpenConns(databaseMaxOpenConns)
	instance.db.SetConnMaxLifetime(databaseConnMaxLifetime)

	return instance
}

// BuildDSN constructs a MySQL DSN from the provided connection config.
func BuildDSN(config *viper.Viper) string {
	dsnConfig := mysql.NewConfig()
	dsnConfig.Params = make(map[string]string)

	if config.IsSet("connection.unix_socket") {
		dsnConfig.Net = "unix"
		dsnConfig.Addr = config.GetString("connection.unix_socket")
	} else {
		dsnConfig.Net = "tcp"
		if config.IsSet("connection.port") {
			dsnConfig.Addr = net.JoinHostPort(config.GetString("connection.host"),
				config.GetString("connection.port"))
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

	if config.GetBool("connection.tls.required") {
		// Full TLS is enabled
		dsnConfig.TLSConfig = "true"
	}

	if config.IsSet("connection.tls.ca") {
		// Full TLS is enabled with custom CA
		tlsConfig := buildTLSConfig(config)
		err := mysql.RegisterTLSConfig("custom", tlsConfig)
		if err != nil {
			logrus.Fatalf("Failed to register custom TLS configuration: %v", err)
		}
		dsnConfig.TLSConfig = "custom"
	}

	if config.GetBool("connection.tls.skip-verify") {
		// Enable SSL but skip TLS verification
		dsnConfig.TLSConfig = "skip-verify"
	}

	dsnConfig.Timeout = time.Second

	if logrus.IsLevelEnabled(logrus.DebugLevel) {
		sanitizedDsn := dsnConfig.Clone()
		sanitizedDsn.Passwd = "<redacted>"
		logrus.Debug(fmt.Sprintf("Constructed DSN for MySQL: %s", sanitizedDsn.FormatDSN()))
	}

	return dsnConfig.FormatDSN()
}

// buildTLSConfig creates a tls.Config instance from the provided application TLS config.
func buildTLSConfig(config *viper.Viper) *tls.Config {
	var tlsConfig tls.Config

	rootCertPool := x509.NewCertPool()

	if config.IsSet("connection.tls.ca") {
		pem, err := os.ReadFile(config.GetString("connection.tls.ca"))
		if err != nil {
			logrus.Error(err)
		}

		if ok := rootCertPool.AppendCertsFromPEM(pem); !ok {
			logrus.Error("Failed to append PEM.")
		} else {
			tlsConfig.RootCAs = rootCertPool
		}
	}

	if config.IsSet("connection.tls.cert") && config.IsSet("connection.tls.key") {
		certs, err := tls.LoadX509KeyPair(config.GetString("connection.tls.cert"),
			config.GetString("connection.tls.key"))
		if err != nil {
			logrus.Error(err)
		}

		clientCert := make([]tls.Certificate, 0, 1)
		clientCert = append(clientCert, certs)

		tlsConfig.Certificates = clientCert
	}

	return &tlsConfig
}

func (h *DBHandler) isConnected() bool {
	if err := h.db.Ping(); err != nil {
		logrus.Error(err)
		return false
	}

	return true
}

// GetStatus performs a health check on the database server and returns an int type
// enumerating the specific state.
func (h *DBHandler) GetStatus() ServerStatus {
	if h.isConnected() {
		if customQuery != "" {
			result := h.getCustomRequest(customQuery)
			return result
		} else {
			logrus.Info("Executing normal queyr")
			wsrepState := h.getWsrepLocalState()
			if wsrepState == Synced || (wsrepState == Donor && h.availableWhenDonor) {
				if !h.availableWhenReadOnly && h.isReadOnly() {
					return ReadOnly
				}

				return Available
			}

			return NotReady
		}
	}

	return Unavailable
}

// getWsrepLocalState queries the wsrep_local_state status from the database
// server and returns an int type enumerating the specific state.
func (h *DBHandler) getWsrepLocalState() WsrepStatus {
	stmtOut, err := h.db.Prepare(wsrepLocalStateQuery)
	if err != nil {
		logrus.Errorf("Error preparing wsrep_local_state query: %v", err)
		return Joining
	}

	defer func() {
		if err := stmtOut.Close(); err != nil {
			logrus.Errorf("Error closing prepared statement: %v", err)
		}
	}()

	var variable string

	var value int

	err = stmtOut.QueryRow().Scan(&variable, &value)
	if err != nil {
		logrus.Errorf("Error executing wsrep_local_state query: %v", err)
		return Joining
	}

	return WsrepStatus(value)
}

func (h *DBHandler) getCustomRequest(query string) ServerStatus {

	logrus.Debugf("Executing custom query: %s", query)

	/*result, err := h.db.Exec(query)

	if err != nil {
		logrus.Errorf("Error executing CUSTOM query: %v", err)
		return Unavailable
	}

	logrus.Infof("Result OK : %v", result)*/

	/* else {
		var index, rows int64
		index, err = result.LastInsertId()
		logrus.Infof("Index : %d, err : %v", index, err)
		rows, err = result.RowsAffected()
		logrus.Infof("Rows : %d, err : %v", rows, err)
	}

	//var value int64

	var _, err2 = h.db.Query(customQuery)
	if err2 != nil {
		logrus.Errorf("Error2 executing CUSTOM query: %v", err2)
		return Unavailable
	} /*else {
		var index, rows int64
		index, err = result.LastInsertId()
		logrus.Infof("Index : %d, err : %v", rows2, err2)
		rows, err = result.RowsAffected()
		logrus.Infof("Rows : %d, err : %v", rows, err2)
	}*/

	/*value, err = result.
	if err != nil {
		logrus.Errorf("Error getting Affected rows: %v", err)
		return Unavailable
	}*/

	var queryResult string

	var result, err2 = h.db.Query(query)
	if err2 != nil {
		logrus.Errorf("Error2 executing CUSTOM query: %v", err2)
		return NotReady
	}

	if result.Next() {
		result.Scan(&queryResult)

		if queryResult == customResult {
			result.Close()
			return Available
		}
	} else {
		logrus.Errorf("No query result")
		result.Close()
		return NotReady
	}
	result.Close()

	logrus.Errorf("Result is incorrect : '%s' != '%s'", queryResult, customResult)

	return NotReady
}

// isReadOnly queries the global variable read_only from the database server
// and returns whether the server is in read-only mode.
func (h *DBHandler) isReadOnly() bool {
	stmtOut, err := h.db.Prepare(readOnlyQuery)
	if err != nil {
		logrus.Errorf("Error preparing read_only query: %v", err)
	}

	defer func() {
		if err := stmtOut.Close(); err != nil {
			logrus.Errorf("Error closing prepared statement: %v", err)
		}
	}()

	var variable string

	var value string

	err = stmtOut.QueryRow().Scan(&variable, &value)
	if err != nil {
		logrus.Errorf("Error executing read_only query: %v", err)
	}

	if value == "OFF" {
		return false
	}

	return true
}
