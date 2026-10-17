package db

// readerIdleConns is the number of idle read connections that the pool keeps. It is the default of database/sql:
// nothing in this package sets another.
const readerIdleConns = 2

// RenewReaders closes the idle read connections, so the next read opens a new one. A connection keeps state of a
// virtual table between its statements. After the FTS5 'rebuild' of another connection, a reader that read the old
// index still reports corruption of the new one in PRAGMA integrity_check. A reader in use stays in the pool when it
// is released: a caller that must not meet one runs RenewReaders when no read is open.
func (d *DB) RenewReaders() {
	d.R.SetMaxIdleConns(0)
	d.R.SetMaxIdleConns(readerIdleConns)
}
