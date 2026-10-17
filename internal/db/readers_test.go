package db

import "testing"

func TestRenewReadersClosesTheIdleReadersAndKeepsThePool(t *testing.T) {
	d := Fresh(t)
	first, err := d.R.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	second, err := d.R.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil { // Close gives the connection back to the pool
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if got := d.R.Stats().Idle; got != 2 {
		t.Fatalf("idle readers before RenewReaders = %d; want 2", got)
	}
	d.RenewReaders()
	if got := d.R.Stats().Idle; got != 0 {
		t.Errorf("idle readers after RenewReaders = %d; want 0", got)
	}
	var one int
	if err := d.R.QueryRowContext(t.Context(), "SELECT 1").Scan(&one); err != nil || one != 1 {
		t.Fatalf("the pool must still read: %d %v", one, err)
	}
	if got := d.R.Stats().Idle; got != 1 {
		t.Errorf("idle readers after a read = %d; want 1: the pool keeps connections again", got)
	}
}
