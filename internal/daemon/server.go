package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"

	"github.com/itsmehatef/dclaw/internal/protocol"
	"github.com/itsmehatef/dclaw/internal/sandbox"
	"github.com/itsmehatef/dclaw/internal/store"
)

// Server is the dclawd Unix-socket listener. One listener, many connections.
// Each connection is handled in its own goroutine; each message on a
// connection is processed sequentially (per the wire protocol's v1
// one-request-at-a-time rule).
type Server struct {
	cfg    *Config
	log    *slog.Logger
	repo   *store.Repo
	docker *sandbox.DockerClient

	router *Router

	mu       sync.Mutex
	listener net.Listener
	closed   bool
}

// NewServer wires up a Server. Call Run to start it.
func NewServer(cfg *Config, log *slog.Logger, repo *store.Repo, docker *sandbox.DockerClient) *Server {
	s := &Server{cfg: cfg, log: log, repo: repo, docker: docker}
	s.router = NewRouter(log, repo, docker)
	return s
}

// Run starts listening on cfg.SocketPath and serves connections until ctx is
// cancelled. Returns the first non-normal error.
func (s *Server) Run(ctx context.Context) error {
	// Remove stale socket from a crashed previous run.
	_ = os.Remove(s.cfg.SocketPath)

	ln, err := net.Listen("unix", s.cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("listen %q: %w", s.cfg.SocketPath, err)
	}
	if err := os.Chmod(s.cfg.SocketPath, 0o660); err != nil {
		ln.Close()
		return fmt.Errorf("chmod socket: %w", err)
	}

	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		ln.Close()
		_ = os.Remove(s.cfg.SocketPath)
	}()

	// Cancel blocks when ctx is done.
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	s.log.Info("dclawd listening", "socket", s.cfg.SocketPath)

	var wg sync.WaitGroup
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				break
			}
			s.log.Warn("accept error", "err", err)
			continue
		}
		wg.Add(1)
		go func(c net.Conn) {
			defer wg.Done()
			s.serveConn(ctx, c)
		}(conn)
	}

	wg.Wait()
	return nil
}

// serveConn handles one connection for its lifetime: handshake, then
// per-message dispatch until EOF or ctx cancellation.
func (s *Server) serveConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	// 1. Handshake required.
	var hs protocol.Envelope
	if err := dec.Decode(&hs); err != nil {
		s.log.Warn("handshake decode failed", "err", err)
		return
	}
	if hs.Method != "dclaw.handshake" {
		_ = enc.Encode(protocol.ErrorResponse(hs.ID, protocol.ErrInvalidRequest, "first message must be dclaw.handshake", nil))
		return
	}
	var hreq protocol.Handshake
	if err := json.Unmarshal(hs.Params, &hreq); err != nil {
		_ = enc.Encode(protocol.ErrorResponse(hs.ID, protocol.ErrInvalidParams, "handshake params invalid", err.Error()))
		return
	}
	if hreq.ProtocolVersion != protocol.Version {
		_ = enc.Encode(protocol.ErrorResponse(hs.ID, protocol.ErrInvalidRequest, "unsupported protocol version",
			map[string]any{"requested": hreq.ProtocolVersion, "supported": []int{protocol.Version}}))
		return
	}
	if err := enc.Encode(protocol.SuccessResponse(hs.ID, protocol.HandshakeResult{Accepted: true, NegotiatedVersion: protocol.Version})); err != nil {
		return
	}
	s.log.Debug("handshake ok", "component", hreq.ComponentType, "id", hreq.ComponentID)

	// 2. Main message loop.
	send := func(env *protocol.Envelope) error {
		return enc.Encode(env)
	}
	for {
		if ctx.Err() != nil {
			return
		}
		var env protocol.Envelope
		if err := dec.Decode(&env); err != nil {
			if !errors.Is(err, io.EOF) {
				s.log.Debug("conn decode done", "err", err)
			}
			return
		}
		resp := s.router.Dispatch(ctx, &env, send)
		if resp != nil {
			if err := enc.Encode(resp); err != nil {
				return
			}
		}
	}
}
