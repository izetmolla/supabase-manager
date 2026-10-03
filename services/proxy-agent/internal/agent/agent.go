// Package agent connects to the manager over gRPC and executes its commands on the proxy.
package agent

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/supabase-manager/proxy-agent/internal/logs"
	"github.com/supabase-manager/proxy-agent/internal/proxy"
	pb "github.com/supabase-manager/shared/proxyagent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
)

// Config is how the agent reaches and identifies itself to the manager.
type Config struct {
	ManagerAddr string
	InstanceID  uint64
	Token       string
	// TLS dials with TLS; CertSHA256 pins the manager's certificate (hex SHA-256 of its DER),
	// otherwise the system roots verify it.
	TLS        bool
	CertSHA256 string
	Version    string
}

const (
	applyTimeout   = 5 * time.Minute
	maxBackoff     = 30 * time.Second
	tailChunkLines = 500
)

type Agent struct {
	cfg Config
	p   proxy.Proxy
}

func New(cfg Config, p proxy.Proxy) *Agent { return &Agent{cfg: cfg, p: p} }

// Run keeps a session with the manager open, reconnecting with backoff, until ctx ends.
func (a *Agent) Run(ctx context.Context) {
	backoff := time.Second
	for {
		start := time.Now()
		err := a.session(ctx)
		if ctx.Err() != nil {
			return
		}
		if time.Since(start) > time.Minute {
			backoff = time.Second
		}
		log.Printf("agent: connection to %s lost: %v (retrying in %s)", a.cfg.ManagerAddr, err, backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

func (a *Agent) credentials() (credentials.TransportCredentials, error) {
	if !a.cfg.TLS {
		return insecure.NewCredentials(), nil
	}
	if a.cfg.CertSHA256 == "" {
		return credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12}), nil
	}
	want := strings.ToLower(a.cfg.CertSHA256)
	return credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS12,
		// The pinned certificate is self-signed; it is checked below instead of against roots.
		InsecureSkipVerify: true, //nolint:gosec // verified by VerifyPeerCertificate
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errors.New("the manager sent no certificate")
			}
			sum := sha256.Sum256(raw[0])
			if hex.EncodeToString(sum[:]) != want {
				return errors.New("the manager's certificate does not match the pinned fingerprint")
			}
			return nil
		},
	}), nil
}

type session struct {
	a      *Agent
	ctx    context.Context
	out    chan *pb.AgentMessage
	tailMu sync.Mutex
	tails  map[string]context.CancelFunc
}

func (a *Agent) session(ctx context.Context) error {
	creds, err := a.credentials()
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(a.cfg.ManagerAddr,
		grpc.WithTransportCredentials(creds),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: 30 * time.Second, Timeout: 10 * time.Second}),
	)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	md := metadata.Pairs("authorization", "Bearer "+a.cfg.Token, "x-sm-instance", strconv.FormatUint(a.cfg.InstanceID, 10))
	sctx, cancel := context.WithCancel(metadata.NewOutgoingContext(ctx, md))
	defer cancel()
	stream, err := pb.NewProxyAgentClient(conn).Connect(sctx)
	if err != nil {
		return err
	}
	if err := stream.Send(&pb.AgentMessage{Msg: &pb.AgentMessage_Hello{Hello: &pb.Hello{
		InstanceId:     a.cfg.InstanceID,
		Kind:           a.p.Kind(),
		AgentVersion:   a.cfg.Version,
		ProxyVersion:   a.p.Version(),
		ConfigChecksum: a.p.Checksum(),
	}}}); err != nil {
		return err
	}
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	welcome := first.GetWelcome()
	if welcome == nil {
		return errors.New("the manager did not accept the connection")
	}
	log.Printf("agent: connected to %s", a.cfg.ManagerAddr)

	s := &session{a: a, ctx: sctx, out: make(chan *pb.AgentMessage, 256), tails: map[string]context.CancelFunc{}}
	defer s.stopTails()
	sendErr := make(chan error, 1)
	go func() {
		for {
			select {
			case <-sctx.Done():
				return
			case m := <-s.out:
				if err := stream.Send(m); err != nil {
					sendErr <- err
					cancel()
					return
				}
			}
		}
	}()
	go s.reportStatus(time.Duration(max(welcome.StatusIntervalSeconds, 5)) * time.Second)

	for {
		msg, err := stream.Recv()
		if err != nil {
			select {
			case e := <-sendErr:
				return e
			default:
			}
			return err
		}
		s.handle(msg)
	}
}

func (s *session) send(m *pb.AgentMessage) {
	select {
	case s.out <- m:
	case <-s.ctx.Done():
	}
}

func (s *session) status() *pb.AgentMessage {
	st := s.a.p.State()
	var started int64
	if st.Running {
		started = st.StartedAt.Unix()
	}
	return &pb.AgentMessage{Msg: &pb.AgentMessage_Status{Status: &pb.Status{
		ProxyRunning:   st.Running,
		ProxyStartedAt: started,
		Restarts:       st.Restarts,
		ConfigChecksum: s.a.p.Checksum(),
		LastError:      st.LastError,
	}}}
}

func (s *session) reportStatus(every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	s.send(s.status())
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
		case <-s.a.p.Changed():
		}
		s.send(s.status())
	}
}

func (s *session) result(id string, output string, err error) {
	r := &pb.Result{RequestId: id, Ok: err == nil, Output: output}
	if err != nil {
		r.Error = err.Error()
	}
	s.send(&pb.AgentMessage{Msg: &pb.AgentMessage_Result{Result: r}})
}

func (s *session) handle(msg *pb.ManagerMessage) {
	id := msg.GetRequestId()
	switch m := msg.Msg.(type) {
	case *pb.ManagerMessage_Apply:
		go func() {
			ctx, cancel := context.WithTimeout(s.ctx, applyTimeout)
			defer cancel()
			files := make(map[string]proxy.File, len(m.Apply.Files))
			for name, f := range m.Apply.Files {
				files[name] = proxy.File{Content: f.Content, Mode: os.FileMode(f.Mode)}
			}
			out, err := s.a.p.Apply(ctx, files, m.Apply.Checksum, int(m.Apply.AdminPort))
			s.result(id, out, err)
			s.send(s.status())
		}()
	case *pb.ManagerMessage_Restart:
		go func() {
			s.result(id, "", s.a.p.Restart())
			s.send(s.status())
		}()
	case *pb.ManagerMessage_Ping:
		s.result(id, "", nil)
	case *pb.ManagerMessage_StartTail:
		s.startTail(m.StartTail.StreamId, int(m.StartTail.Lines))
	case *pb.ManagerMessage_StopTail:
		s.stopTail(m.StopTail.StreamId)
	default:
		s.result(id, "", fmt.Errorf("unsupported command %T", msg.Msg))
	}
}

func (s *session) startTail(streamID string, lines int) {
	ctx, cancel := context.WithCancel(s.ctx)
	s.tailMu.Lock()
	if old := s.tails[streamID]; old != nil {
		old()
	}
	s.tails[streamID] = cancel
	s.tailMu.Unlock()
	go func() {
		err := logs.Follow(ctx, s.a.p.AccessLogs(), lines, func(batch []logs.Line) error {
			for len(batch) > 0 {
				n := min(len(batch), tailChunkLines)
				chunk := &pb.LogChunk{StreamId: streamID}
				for _, l := range batch[:n] {
					chunk.Lines = append(chunk.Lines, &pb.LogLine{Source: l.Source, Text: l.Text})
				}
				s.send(&pb.AgentMessage{Msg: &pb.AgentMessage_Log{Log: chunk}})
				batch = batch[n:]
			}
			return ctx.Err()
		})
		closed := &pb.LogChunk{StreamId: streamID, Closed: true}
		if err != nil && !errors.Is(err, context.Canceled) {
			closed.Error = err.Error()
		}
		s.send(&pb.AgentMessage{Msg: &pb.AgentMessage_Log{Log: closed}})
		s.stopTail(streamID)
	}()
}

func (s *session) stopTail(streamID string) {
	s.tailMu.Lock()
	defer s.tailMu.Unlock()
	if c := s.tails[streamID]; c != nil {
		c()
		delete(s.tails, streamID)
	}
}

func (s *session) stopTails() {
	s.tailMu.Lock()
	defer s.tailMu.Unlock()
	for id, c := range s.tails {
		c()
		delete(s.tails, id)
	}
}
