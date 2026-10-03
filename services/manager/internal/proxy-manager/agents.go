package proxymanager

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	pb "github.com/supabase-manager/shared/proxyagent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

const (
	agentStatusInterval = 15
	agentTLSKey         = "proxy_agent_tls"
)

// ErrAgentOffline means the instance's agent is not connected.
var ErrAgentOffline = errors.New("the proxy agent is not connected")

// AgentInfo is what the manager knows about a connected agent.
type AgentInfo struct {
	Connected      bool       `json:"connected"`
	ConnectedAt    *time.Time `json:"connected_at,omitempty"`
	RemoteAddr     string     `json:"remote_addr,omitempty"`
	AgentVersion   string     `json:"agent_version,omitempty"`
	ProxyVersion   string     `json:"proxy_version,omitempty"`
	ProxyRunning   bool       `json:"proxy_running"`
	ProxyStartedAt *time.Time `json:"proxy_started_at,omitempty"`
	Restarts       uint32     `json:"restarts"`
	ConfigChecksum string     `json:"config_checksum,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
}

type agentConn struct {
	instanceID uint
	stream     grpc.BidiStreamingServer[pb.AgentMessage, pb.ManagerMessage]
	done       chan struct{}
	sendMu     sync.Mutex

	mu      sync.Mutex
	info    AgentInfo
	pending map[string]chan *pb.Result
	tails   map[string]chan *pb.LogChunk
}

func (c *agentConn) send(m *pb.ManagerMessage) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.stream.Send(m)
}

// agentHub accepts agent connections and routes commands and replies.
type agentHub struct {
	pb.UnimplementedProxyAgentServer
	s *Service

	mu      sync.Mutex
	conns   map[uint]*agentConn
	waiters map[uint][]chan struct{}
}

func newAgentHub(s *Service) *agentHub {
	return &agentHub{s: s, conns: map[uint]*agentConn{}, waiters: map[uint][]chan struct{}{}}
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// agentDialAddr is where agents on this host reach the gRPC server (they use the host network).
func (s *Service) agentDialAddr() string {
	host, port, err := net.SplitHostPort(s.opts.AgentAddr)
	if err != nil {
		return s.opts.AgentAddr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

// agentTLS reports whether the gRPC server uses TLS: whenever it is reachable beyond loopback.
func (s *Service) agentTLS() bool {
	host, _, err := net.SplitHostPort(s.opts.AgentAddr)
	return err == nil && !isLoopbackHost(host)
}

type storedAgentCert struct {
	CertPEM      string `json:"cert_pem"`
	KeyEncrypted string `json:"key_encrypted"`
}

// agentCertificate returns the server's self-signed certificate, creating it once. Agents pin
// its SHA-256 fingerprint.
func (s *Service) agentCertificate() (tls.Certificate, string, error) {
	var st storedAgentCert
	if ok, err := s.settings.Get(agentTLSKey, &st); err != nil {
		return tls.Certificate{}, "", err
	} else if !ok || st.CertPEM == "" {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return tls.Certificate{}, "", err
		}
		serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
		tpl := &x509.Certificate{
			SerialNumber: serial,
			Subject:      pkix.Name{CommonName: "supabase-manager proxy agents"},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().AddDate(20, 0, 0),
			KeyUsage:     x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
		if err != nil {
			return tls.Certificate{}, "", err
		}
		kb, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			return tls.Certificate{}, "", err
		}
		st = storedAgentCert{
			CertPEM:      string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
			KeyEncrypted: s.encrypt(string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}))),
		}
		if err := s.settings.Put(agentTLSKey, st); err != nil {
			return tls.Certificate{}, "", err
		}
	}
	cert, err := tls.X509KeyPair([]byte(st.CertPEM), []byte(s.decrypt(st.KeyEncrypted)))
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("agent TLS certificate: %w", err)
	}
	sum := sha256.Sum256(cert.Certificate[0])
	return cert, hex.EncodeToString(sum[:]), nil
}

// serve runs the gRPC server until ctx ends.
func (h *agentHub) serve(ctx context.Context) {
	addr := h.s.opts.AgentAddr
	opts := []grpc.ServerOption{
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{MinTime: 20 * time.Second, PermitWithoutStream: true}),
		grpc.KeepaliveParams(keepalive.ServerParameters{Time: time.Minute, Timeout: 20 * time.Second}),
		grpc.MaxRecvMsgSize(16 << 20),
	}
	if h.s.agentTLS() {
		cert, _, err := h.s.agentCertificate()
		if err != nil {
			log.Printf("proxy manager: agent server: %v", err)
			return
		}
		opts = append(opts, grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})))
	}
	var lis net.Listener
	for {
		var err error
		lis, err = net.Listen("tcp", addr)
		if err == nil {
			break
		}
		log.Printf("proxy manager: agent server cannot listen on %s: %v (retrying)", addr, err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
	srv := grpc.NewServer(opts...)
	pb.RegisterProxyAgentServer(srv, h)
	go func() {
		<-ctx.Done()
		stopped := make(chan struct{})
		go func() { srv.GracefulStop(); close(stopped) }()
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			srv.Stop()
		}
	}()
	log.Printf("proxy manager: agent server listening on %s", addr)
	if err := srv.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		log.Printf("proxy manager: agent server: %v", err)
	}
}

func (h *agentHub) authenticate(ctx context.Context) (*ProxyInstance, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	get := func(k string) string {
		if v := md.Get(k); len(v) > 0 {
			return v[0]
		}
		return ""
	}
	id, err := strconv.ParseUint(get("x-sm-instance"), 10, 64)
	token := strings.TrimPrefix(get("authorization"), "Bearer ")
	if err != nil || id == 0 || token == "" {
		return nil, status.Error(codes.Unauthenticated, "missing instance id or token")
	}
	in, err := h.s.GetInstance(uint(id))
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "unknown instance")
	}
	want := h.s.decrypt(in.TokenEncrypted)
	if want == "" || subtle.ConstantTimeCompare([]byte(want), []byte(token)) != 1 {
		return nil, status.Error(codes.Unauthenticated, "invalid token")
	}
	return in, nil
}

// Connect is the agent's session: Hello, Welcome, then commands and replies until either side
// closes it. A new session for the same instance replaces the old one.
func (h *agentHub) Connect(stream grpc.BidiStreamingServer[pb.AgentMessage, pb.ManagerMessage]) error {
	if !h.s.Enabled() {
		return status.Error(codes.Unavailable, "the Proxy Manager is disabled")
	}
	in, err := h.authenticate(stream.Context())
	if err != nil {
		return err
	}
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	hello := first.GetHello()
	if hello == nil || uint(hello.InstanceId) != in.ID {
		return status.Error(codes.InvalidArgument, "the first message must be Hello for this instance")
	}
	if hello.Kind != in.Kind {
		return status.Errorf(codes.FailedPrecondition, "the container runs %s but the instance is %s; use a %s proxy image", hello.Kind, in.Kind, in.Kind)
	}
	if err := stream.Send(&pb.ManagerMessage{Msg: &pb.ManagerMessage_Welcome{Welcome: &pb.Welcome{StatusIntervalSeconds: agentStatusInterval}}}); err != nil {
		return err
	}

	now := time.Now()
	c := &agentConn{
		instanceID: in.ID, stream: stream, done: make(chan struct{}),
		pending: map[string]chan *pb.Result{}, tails: map[string]chan *pb.LogChunk{},
		info: AgentInfo{
			Connected: true, ConnectedAt: &now, AgentVersion: hello.AgentVersion,
			ProxyVersion: hello.ProxyVersion, ConfigChecksum: hello.ConfigChecksum,
		},
	}
	if p, ok := peerAddr(stream.Context()); ok {
		c.info.RemoteAddr = p
	}
	h.register(c)
	defer h.unregister(c)
	if in.DeployedRevisionID != 0 && hello.ConfigChecksum != in.DeployedChecksum {
		go h.s.resync(stream.Context(), in.ID)
	}

	for {
		msg, err := stream.Recv()
		if err != nil {
			return nil
		}
		switch m := msg.Msg.(type) {
		case *pb.AgentMessage_Result:
			c.mu.Lock()
			ch := c.pending[m.Result.RequestId]
			delete(c.pending, m.Result.RequestId)
			c.mu.Unlock()
			if ch != nil {
				ch <- m.Result
			}
		case *pb.AgentMessage_Status:
			c.mu.Lock()
			st := m.Status
			c.info.ProxyRunning, c.info.Restarts = st.ProxyRunning, st.Restarts
			c.info.ConfigChecksum, c.info.LastError = st.ConfigChecksum, st.LastError
			c.info.ProxyStartedAt = nil
			if st.ProxyStartedAt > 0 {
				t := time.Unix(st.ProxyStartedAt, 0)
				c.info.ProxyStartedAt = &t
			}
			c.mu.Unlock()
		case *pb.AgentMessage_Log:
			c.mu.Lock()
			ch := c.tails[m.Log.StreamId]
			c.mu.Unlock()
			if ch != nil {
				select {
				case ch <- m.Log:
				case <-time.After(5 * time.Second):
					// The reader is gone or too slow; drop the chunk instead of blocking the session.
				}
			}
		}
	}
}

func (h *agentHub) register(c *agentConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if old := h.conns[c.instanceID]; old != nil {
		old.closeLocked()
	}
	h.conns[c.instanceID] = c
	for _, w := range h.waiters[c.instanceID] {
		close(w)
	}
	delete(h.waiters, c.instanceID)
	log.Printf("proxy manager: agent for instance %d connected (%s %s)", c.instanceID, c.info.AgentVersion, c.info.RemoteAddr)
}

func (h *agentHub) unregister(c *agentConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conns[c.instanceID] == c {
		delete(h.conns, c.instanceID)
		log.Printf("proxy manager: agent for instance %d disconnected", c.instanceID)
	}
	c.closeLocked()
}

// closeLocked fails everything waiting on the connection.
func (c *agentConn) closeLocked() {
	select {
	case <-c.done:
		return
	default:
	}
	close(c.done)
}

func (h *agentHub) conn(id uint) *agentConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conns[id]
}

// info returns the agent state of an instance.
func (h *agentHub) info(id uint) AgentInfo {
	c := h.conn(id)
	if c == nil {
		return AgentInfo{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.info
}

// waitConnected waits until the instance's agent is connected.
func (h *agentHub) waitConnected(ctx context.Context, id uint, timeout time.Duration) error {
	h.mu.Lock()
	if h.conns[id] != nil {
		h.mu.Unlock()
		return nil
	}
	w := make(chan struct{})
	h.waiters[id] = append(h.waiters[id], w)
	h.mu.Unlock()
	select {
	case <-w:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(timeout):
		return ErrAgentOffline
	}
}

func newRequestID() string { return randomToken()[:16] }

func peerAddr(ctx context.Context) (string, bool) {
	p, ok := peer.FromContext(ctx)
	if !ok || p.Addr == nil {
		return "", false
	}
	return p.Addr.String(), true
}

// call sends a command and waits for its result.
func (h *agentHub) call(ctx context.Context, id uint, msg *pb.ManagerMessage) (*pb.Result, error) {
	c := h.conn(id)
	if c == nil {
		return nil, ErrAgentOffline
	}
	msg.RequestId = newRequestID()
	ch := make(chan *pb.Result, 1)
	c.mu.Lock()
	c.pending[msg.RequestId] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, msg.RequestId)
		c.mu.Unlock()
	}()
	if err := c.send(msg); err != nil {
		return nil, fmt.Errorf("send to agent: %w", err)
	}
	select {
	case r := <-ch:
		return r, nil
	case <-c.done:
		return nil, errors.New("the proxy agent disconnected before answering")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// tail streams access log lines from the agent until ctx ends or the agent stops the tail.
func (h *agentHub) tail(ctx context.Context, id uint, lines int, fn func(source, text string) error) error {
	c := h.conn(id)
	if c == nil {
		return ErrAgentOffline
	}
	streamID := newRequestID()
	ch := make(chan *pb.LogChunk, 64)
	c.mu.Lock()
	c.tails[streamID] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.tails, streamID)
		c.mu.Unlock()
		_ = c.send(&pb.ManagerMessage{Msg: &pb.ManagerMessage_StopTail{StopTail: &pb.StopTail{StreamId: streamID}}})
	}()
	if err := c.send(&pb.ManagerMessage{Msg: &pb.ManagerMessage_StartTail{StartTail: &pb.StartTail{StreamId: streamID, Lines: int32(lines)}}}); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-c.done:
			return ErrAgentOffline
		case chunk := <-ch:
			for _, l := range chunk.Lines {
				if err := fn(l.Source, l.Text); err != nil {
					return err
				}
			}
			if chunk.Closed {
				if chunk.Error != "" {
					return errors.New(chunk.Error)
				}
				return nil
			}
		}
	}
}
