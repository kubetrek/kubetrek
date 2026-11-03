// Command server pairs a browser tab with a terminal by a short code and relays the agent's messages to it.
package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/kubetrek/kubetrek/internal/protocol"
)

const (
	pingEvery    = 10 * time.Second
	writeTimeout = 5 * time.Second
	// A snapshot of a busy namespace is well over the default 32 KB.
	readLimit = 4 << 20
)

type room struct {
	web, agent *websocket.Conn
}

// relay keeps a room per code. A room lives while its web tab or its agent is connected,
// so a reload of the page finds the agent still there.
type relay struct {
	mu      sync.Mutex
	rooms   map[string]*room
	origins []string
}

// No 0/O or 1/I, so the code is easy to type from the screen.
const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func (r *relay) newCode() string {
	for {
		b := make([]byte, 8)
		rand.Read(b)
		for i := range b {
			b[i] = alphabet[int(b[i])%len(alphabet)]
		}
		code := string(b[:4]) + "-" + string(b[4:])
		if r.rooms[code] == nil {
			return code
		}
	}
}

func normalize(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

// drop deletes the room once nobody is in it. Call it with r.mu held.
func (r *relay) drop(code string, rm *room) {
	if rm.web == nil && rm.agent == nil && r.rooms[code] == rm {
		delete(r.rooms, code)
	}
}

func send(ctx context.Context, c *websocket.Conn, m protocol.Message) {
	b, _ := json.Marshal(m)
	write(ctx, c, b)
}

// write errors are ignored: a failed write closes the connection and its own handler cleans up.
func write(ctx context.Context, c *websocket.Conn, b []byte) {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	c.Write(ctx, websocket.MessageText, b)
}

// heartbeat returns when the peer stops answering pings or the connection closes.
func heartbeat(ctx context.Context, c *websocket.Conn) {
	t := time.NewTicker(pingEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		pctx, cancel := context.WithTimeout(ctx, writeTimeout)
		err := c.Ping(pctx)
		cancel()
		if err != nil {
			return
		}
	}
}

// web serves the browser. Without ?code= it opens a new room; with the code of a live room
// (the page was reloaded) it rejoins it and asks the agent for a fresh snapshot.
func (r *relay) web(w http.ResponseWriter, req *http.Request) {
	c, err := websocket.Accept(w, req, &websocket.AcceptOptions{OriginPatterns: r.origins})
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx := c.CloseRead(req.Context())

	r.mu.Lock()
	code := normalize(req.URL.Query().Get("code"))
	rm := r.rooms[code]
	if rm == nil {
		code = r.newCode()
		rm = &room{}
		r.rooms[code] = rm
	}
	if rm.web != nil {
		rm.web.Close(websocket.StatusPolicyViolation, "opened in another tab")
	}
	rm.web = c
	agent := rm.agent
	r.mu.Unlock()

	send(ctx, c, protocol.Message{Type: protocol.Code, Code: code})
	if agent != nil {
		send(ctx, c, protocol.Message{Type: protocol.Connected})
		send(ctx, agent, protocol.Message{Type: protocol.Resync})
	}
	heartbeat(ctx, c)

	r.mu.Lock()
	if rm.web == c {
		rm.web = nil
	}
	r.drop(code, rm)
	r.mu.Unlock()
}

// agent serves `ktrek connect <code>` and forwards every message it sends to the web as is.
func (r *relay) agent(w http.ResponseWriter, req *http.Request) {
	code := normalize(req.URL.Query().Get("code"))
	r.mu.Lock()
	rm := r.rooms[code]
	busy := rm != nil && rm.agent != nil
	r.mu.Unlock()
	switch {
	case rm == nil:
		http.Error(w, "unknown code", http.StatusNotFound)
		return
	case busy:
		http.Error(w, "another terminal is already connected", http.StatusConflict)
		return
	}

	c, err := websocket.Accept(w, req, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(readLimit)

	r.mu.Lock()
	if r.rooms[code] != rm || rm.agent != nil {
		r.mu.Unlock()
		c.Close(websocket.StatusTryAgainLater, "the code changed hands, try again")
		return
	}
	rm.agent = c
	web := rm.web
	r.mu.Unlock()
	log.Printf("%s: terminal connected", code)

	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	if web != nil {
		send(ctx, web, protocol.Message{Type: protocol.Connected})
	}
	go heartbeat(ctx, c)

	for {
		_, b, err := c.Read(ctx)
		if err != nil {
			break
		}
		r.mu.Lock()
		web := rm.web
		r.mu.Unlock()
		if web != nil {
			write(ctx, web, b)
		}
	}

	r.mu.Lock()
	rm.agent = nil
	web = rm.web
	r.drop(code, rm)
	r.mu.Unlock()
	log.Printf("%s: terminal disconnected", code)
	if web != nil {
		send(context.Background(), web, protocol.Message{Type: protocol.Disconnected})
	}
}

func (r *relay) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /web", r.web)
	mux.HandleFunc("GET /agent", r.agent)
	return mux
}

func main() {
	addr := flag.String("addr", ":8787", "address to listen on")
	origins := flag.String("origins", "localhost:*", "comma-separated hosts whose pages may open /web")
	flag.Parse()

	r := &relay{rooms: map[string]*room{}, origins: strings.Split(*origins, ",")}
	log.Printf("listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, r.routes()))
}
