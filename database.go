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
	// WsrepLocalStateQuery returns status of local wsrep instance
	WsrepLocalStateQuery = "SHOW STATUS LIKE 'wsrep_local_state';"
	// ReadOnlyQuery determines if node is in read-only mode
	ReadOnlyQuery = "SHOW GLOBAL VARIABLES LIKE 'read_only';"

	// Joining means the node is in process of joining the cluster
	Joining WsrepStatus = 1
	// Donor means the node is providing SST to a joining node
	Donor WsrepStatus = 2
	// Joined means the node has received the SST but is not synced yet
	Joined WsrepStatus = 3
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

	db.SetMaxOpenConns(5)
	db.SetConnMaxLifetime(time.Minute * 5)
}

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
			mysql.RegisterTLSConfig("custom", tlsConfig)
			dsnConfig.TLSConfig = "custom"
		} else if config.GetBool("connection.tls.required") {
			// Full TLS is enabled
			dsnConfig.TLSConfig = "true"
		}
	}

	dsnConfig.Timeout = time.Second

	return dsnConfig

}

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
		clientCert := make([]tls.Certificate, 0, 1)
		certs, err := tls.LoadX509KeyPair(cnxTLSCfg.GetString("cert"), cnxTLSCfg.GetString("key"))
		if err != nil {
			logger.Error(err)
		}
		clientCert = append(clientCert, certs)
		tlsConfig.Certificates = clientCert
	}
	return &tlsConfig
}

// RunStatusCheck performs a health check on the database.
func RunStatusCheck() DatabaseStatus {
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

func getWsrepLocalState() WsrepStatus {
	stmtOut, err := db.Prepare(WsrepLocalStateQuery)
	if err != nil {
		logger.Error(err)
		return Joining
	}
	defer stmtOut.Close()

	var variable string
	var value int

	err = stmtOut.QueryRow().Scan(&variable, &value)

	return WsrepStatus(value)
}

func isReadOnly() bool {
	stmtOut, err := db.Prepare(ReadOnlyQuery)
	if err != nil {
		logger.Error(err)
	}
	defer stmtOut.Close()

	var variable string
	var value string

	err = stmtOut.QueryRow().Scan(&variable, &value)

	switch value {
	case "OFF":
		return false
	}
	return true
}
