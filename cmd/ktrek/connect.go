package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/kubetrek/kubetrek/internal/protocol"
)

const defaultServer = "ws://localhost:8787"

// wsWriter turns each json.Encoder.Encode call (a single Write) into one WebSocket message.
type wsWriter struct {
	ctx context.Context
	c   *websocket.Conn
}

func (w wsWriter) Write(p []byte) (int, error) {
	ctx, cancel := context.WithTimeout(w.ctx, 10*time.Second)
	defer cancel()
	return len(p), w.c.Write(ctx, websocket.MessageText, p)
}

func stateFile(name string) string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "ktrek", name)
}

// agentPid returns the background agent's pid, if one is running.
func agentPid() (int, bool) {
	b, err := os.ReadFile(stateFile("agent.pid"))
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid, err == nil && alive(pid)
}

func stopAgent() error {
	pid, ok := agentPid()
	if !ok {
		return nil
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := stop(p); err != nil {
		return err
	}
	for range 30 {
		if !alive(pid) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	os.Remove(stateFile("agent.pid"))
	return nil
}

func disconnect() error {
	if _, ok := agentPid(); !ok {
		fmt.Printf("\n %s Nothing to disconnect: no terminal is connected to the web.\n\n", gray("●"))
		return nil
	}
	if err := stopAgent(); err != nil {
		return err
	}
	fmt.Printf("\n %s Disconnected. The web stopped seeing your cluster.\n\n", green("✓"))
	return nil
}

func dial(ctx context.Context, server, code string) (*websocket.Conn, error) {
	c, resp, err := websocket.Dial(ctx, server+"/agent?code="+url.QueryEscape(code), nil)
	if err == nil {
		return c, nil
	}
	switch {
	case resp != nil && resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("the code %s doesn't match any open page. Copy it again from the web", cyan(code))
	case resp != nil && resp.StatusCode == http.StatusConflict:
		return nil, errors.New("another terminal is already connected to that page. Open the page again for a new code")
	}
	return nil, fmt.Errorf("I can't reach the KubeTrek server at %s: %w", server, err)
}

// connect starts the agent in the background and returns once it is connected, leaving the terminal free.
// A previous agent is stopped first: a new code means a new browser tab.
func connect(server, code, namespace string) error {
	on, err := nodeRunning()
	if err != nil && hint(err) != "" {
		return err
	}
	if !on {
		return fmt.Errorf("your cluster isn't running. Start it with %s first", cyan("ktrek up"))
	}
	if err := stopAgent(); err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateFile(""), 0o755); err != nil {
		return err
	}
	logf, err := os.Create(stateFile("agent.log"))
	if err != nil {
		return err
	}
	defer logf.Close()
	r, w, err := os.Pipe()
	if err != nil {
		return err
	}
	defer r.Close()

	code = strings.ToUpper(strings.TrimSpace(code))
	cmd := exec.Command(exe, "agent", "--detached", "-n", namespace, "--connect", code, "--server", server)
	cmd.Stdout, cmd.Stderr = w, logf
	detach(cmd)
	err = cmd.Start()
	w.Close()
	if err != nil {
		return err
	}
	cmd.Process.Release()

	// The agent writes one line on stdout: "ok", or why it could not connect.
	line, _ := bufio.NewReader(r).ReadString('\n')
	if line = strings.TrimSpace(line); line != "ok" {
		if line == "" {
			line = "the agent stopped before connecting, see " + stateFile("agent.log")
		}
		return errors.New(line)
	}
	kubi(happy,
		bold("Connected!"),
		"Your browser now sees namespace "+cyan(namespace)+" live.",
		"I keep watching in the background, the terminal is yours.",
		gray("ktrek disconnect stops me."),
	)
	return nil
}

// runConnected is the agent process behind connect. With detached it reports on stdout and then
// only writes to stderr, which connect sends to agent.log.
func runConnected(ctx context.Context, server, code, namespace string, detached bool) error {
	report := func(string) {}
	if detached {
		report = func(s string) {
			fmt.Println(s)
			os.Stdout.Close()
		}
	}
	c, err := dial(ctx, server, code)
	if err != nil {
		report(err.Error())
		return err
	}
	defer c.CloseNow()
	pidFile := stateFile("agent.pid")
	if detached {
		if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
			report(err.Error())
			return err
		}
		defer os.Remove(pidFile)
	}
	report("ok")

	conn, lost := context.WithCancelCause(ctx)
	defer lost(nil)
	resync := make(chan struct{}, 1)
	go func() {
		for {
			_, b, err := c.Read(conn)
			if err != nil {
				lost(err)
				return
			}
			var m protocol.Message
			if json.Unmarshal(b, &m) == nil && m.Type == protocol.Resync {
				select {
				case resync <- struct{}{}:
				default:
				}
			}
		}
	}()

	err = runAgent(conn, namespace, wsWriter{conn, c}, resync)
	if ctx.Err() != nil {
		c.Close(websocket.StatusNormalClosure, "")
		return nil
	}
	if cause := context.Cause(conn); cause != nil {
		return fmt.Errorf("lost the connection to the KubeTrek server: %w", cause)
	}
	return err
}
