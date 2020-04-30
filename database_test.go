package main

import (
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
)

func TestCreateDBHandler(t *testing.T) {
	config := CreateConfig()

	db, err := sql.Open("mysql", "/") // Using an actual sql.DB here as placeholder
	if err != nil {
		t.Errorf("Failed to open database: %v", err)
	}

	dbHandler := CreateDBHandler(config, db)

	if dbHandler.db != db {
		t.Error("DBHandler.db not pointing to provided *sql.DB instance.")
	}

	if dbHandler.availableWhenDonor != config.GetBool("options.available_when_donor") {
		t.Error("DBHandler.availableWhenDonor does not match provided config.")
	}

	if dbHandler.availableWhenReadOnly != config.GetBool("options.available_when_readonly") {
		t.Error("DBHandler.availableWhenReadOnly does not match provided config.")
	}
}

func TestBuildDSN(t *testing.T) {
	config := CreateConfig()
	dsn := BuildDSN(config)
	defaultDSN := mysql.NewConfig().FormatDSN()

	if len(dsn) == 0 {
		t.Error("Received empty DSN from BuildDSN().")
	}

	if dsn == defaultDSN {
		t.Error("Received blank DSN from BuildDSN().")
	}
}

func getMockRow(val1 interface{}, val2 interface{}) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"variable", "value"}).AddRow(val1, val2)
}

func TestGetWsrepLocalState(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Errorf("Failed to open sqlmock database: %v", err)
	}

	mock.ExpectPrepare(wsrepLocalStateQuery)
	mock.ExpectQuery(wsrepLocalStateQuery).WillReturnRows(getMockRow("wsrep_local_state", Synced))

	dbHandler := &DBHandler{
		db,
		false,
		false,
	}

	wsrepStatus := dbHandler.getWsrepLocalState()

	if wsrepStatus != Synced {
		t.Errorf("Expected WsrepStatus \"Synced\" but received \"%v\".", wsrepStatus)
	}
}

func TestOfflinegetWsrepLocalState(t *testing.T) {
	db, err := sql.Open("mysql", "/") // Using an actual sql.DB here to simulate database being offline
	if err != nil {
		t.Errorf("Failed to open database: %v", err)
	}

	dbHandler := &DBHandler{
		db,
		false,
		false,
	}

	wsrepStatus := dbHandler.getWsrepLocalState()

	if wsrepStatus != Joining {
		t.Errorf("Expected WsrepStatus \"Joining\" due to server being offline but received \"%v\".", wsrepStatus)
	}
}

func TestIsReadOnly(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Errorf("Failed to open sqlmock database: %v", err)
	}

	mock.ExpectPrepare(readOnlyQuery)
	mock.ExpectQuery(readOnlyQuery).WillReturnRows(getMockRow("read_only", "OFF"))

	dbHandler := &DBHandler{
		db,
		false,
		false,
	}

	if dbHandler.isReadOnly() {
		t.Error("Database is read-write but isReadOnly() returned true.")
	}
}

func TestSyncedRWStatus(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption((true)))
	if err != nil {
		t.Errorf("Failed to open sqlmock database: %v", err)
	}

	mock.ExpectPing()
	mock.ExpectPrepare(wsrepLocalStateQuery)
	mock.ExpectQuery(wsrepLocalStateQuery).WillReturnRows(getMockRow("wsrep_local_state", Synced))
	mock.ExpectPrepare(readOnlyQuery)
	mock.ExpectQuery(readOnlyQuery).WillReturnRows(getMockRow("read_only", "OFF"))

	dbHandler := &DBHandler{
		db,
		false,
		false,
	}

	ready, msg := RunStatusCheck(dbHandler)

	if !ready {
		t.Errorf("Expected database to be available but RunStatusCheck returned false with message \"%s\".", msg)
	}
}

func TestIsConnected(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption((true)))
	if err != nil {
		t.Errorf("Failed to open sqlmock database: %v", err)
	}

	mock.ExpectPing()

	dbHandler := &DBHandler{
		db,
		false,
		false,
	}

	if !dbHandler.isConnected() {
		t.Errorf("Expected database to be connected but isConnected() returned false.")
	}
}

func TestDBOffline(t *testing.T) {
	db, err := sql.Open("mysql", "/") // Using an actual sql.DB here to simulate database being offline
	if err != nil {
		t.Errorf("Failed to open database: %v", err)
	}

	dbHandler := &DBHandler{
		db,
		false,
		false,
	}

	ready, _ := RunStatusCheck(dbHandler)

	if ready {
		t.Error("Expected database to be unavailable but RunStatusCheck returned true.")
	}
}
