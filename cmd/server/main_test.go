package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/kubetrek/kubetrek/internal/protocol"
)

func dial(t *testing.T, ctx context.Context, url string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	c, resp, err := websocket.Dial(ctx, url, nil)
	if err == nil {
		t.Cleanup(func() { c.CloseNow() })
	}
	return c, resp, err
}

func read(t *testing.T, ctx context.Context, c *websocket.Conn) protocol.Message {
	t.Helper()
	_, b, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var m protocol.Message
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s: %v", b, err)
	}
	return m
}

func expect(t *testing.T, ctx context.Context, c *websocket.Conn, want protocol.Type) protocol.Message {
	t.Helper()
	m := read(t, ctx, c)
	if m.Type != want {
		t.Fatalf("got %q, want %q", m.Type, want)
	}
	return m
}

func TestPairRelayAndReload(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv := httptest.NewServer((&relay{rooms: map[string]*room{}}).routes())
	defer srv.Close()
	base := "ws" + strings.TrimPrefix(srv.URL, "http")

	if _, resp, err := dial(t, ctx, base+"/agent?code=NOPE-NOPE"); err == nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown code: err=%v, want 404", err)
	}

	web, _, err := dial(t, ctx, base+"/web")
	if err != nil {
		t.Fatal(err)
	}
	code := expect(t, ctx, web, protocol.Code).Code

	agent, _, err := dial(t, ctx, base+"/agent?code="+strings.ToLower(code))
	if err != nil {
		t.Fatal(err)
	}
	expect(t, ctx, web, protocol.Connected)
	if _, resp, err := dial(t, ctx, base+"/agent?code="+code); err == nil || resp.StatusCode != http.StatusConflict {
		t.Fatalf("second terminal: err=%v, want 409", err)
	}

	agent.Write(ctx, websocket.MessageText, []byte(`{"type":"snapshot","objects":[]}`))
	expect(t, ctx, web, protocol.Snapshot)

	// The page reloads: the old socket goes away and a new one comes back with the same code.
	web.Close(websocket.StatusGoingAway, "")
	web, _, err = dial(t, ctx, base+"/web?code="+code)
	if err != nil {
		t.Fatal(err)
	}
	if got := expect(t, ctx, web, protocol.Code).Code; got != code {
		t.Fatalf("rejoined room %s, want %s", got, code)
	}
	expect(t, ctx, web, protocol.Connected)
	expect(t, ctx, agent, protocol.Resync)

	agent.Close(websocket.StatusNormalClosure, "")
	expect(t, ctx, web, protocol.Disconnected)
}
