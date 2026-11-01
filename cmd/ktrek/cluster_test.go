package main

import (
	"errors"
	"net"
	"strings"
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

func TestHintTellsPermissionFromDaemonDown(t *testing.T) {
	cases := map[string]string{
		"permission denied while trying to connect to the docker API at unix:///var/run/docker.sock": "usermod",
		"failed to connect to the docker API at unix:///nope.sock; check if the daemon is running":   "isn't running",
		"Cannot connect to the Docker daemon at unix:///var/run/docker.sock":                         "isn't running",
		`exec: "docker": executable file not found in $PATH`:                                         "can't find",
		"something else": "",
	}
	for msg, want := range cases {
		got := hint(errors.New(msg))
		if want == "" && got != "" || !strings.Contains(got, want) {
			t.Errorf("hint(%q) = %q, want it to mention %q", msg, got, want)
		}
	}
}
