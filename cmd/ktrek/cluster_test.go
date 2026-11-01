package main

import (
	"net"
	"testing"
)

func TestHostPortSkipsBusyPorts(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	busy := l.Addr().(*net.TCPAddr).Port

	got, err := hostPort(busy, busy)
	if err != nil || got != busy+1 {
		t.Fatalf("hostPort(%d) = %d, %v; want %d", busy, got, err, busy+1)
	}
}
