package agent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/supabase-manager/proxy-agent/internal/proxy"
	"github.com/supabase-manager/proxy-agent/internal/supervisor"
	pb "github.com/supabase-manager/shared/proxyagent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

type fakeProxy struct {
	mu      sync.Mutex
	sum     string
	files   map[string]proxy.File
	logDir  string
	changed chan struct{}
}

func (f *fakeProxy) Kind() string    { return "nginx" }
func (f *fakeProxy) Version() string { return "nginx/test" }
func (f *fakeProxy) Checksum() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sum
}
func (f *fakeProxy) Boot() error { return nil }
func (f *fakeProxy) Apply(_ context.Context, files map[string]proxy.File, sum string, _ int) (string, error) {
	if string(files["nginx.conf"].Content) == "bad" {
		return "emerg", errors.New("rejected")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files, f.sum = files, sum
	return "ok", nil
}
func (f *fakeProxy) Restart() error { return nil }
func (f *fakeProxy) State() supervisor.State {
	return supervisor.State{Running: true, StartedAt: time.Unix(1700000000, 0)}
}
func (f *fakeProxy) Changed() <-chan struct{} { return f.changed }
func (f *fakeProxy) AccessLogs() []string     { return []string{filepath.Join(f.logDir, "*.log")} }
func (f *fakeProxy) Shutdown()                {}

// fakeManager accepts one session at a time and hands it to the test.
type fakeManager struct {
	pb.UnimplementedProxyAgentServer
	sessions chan *managerSession
}

type managerSession struct {
	md     metadata.MD
	hello  *pb.Hello
	stream grpc.BidiStreamingServer[pb.AgentMessage, pb.ManagerMessage]
	recv   chan *pb.AgentMessage
	done   chan struct{}
}

func (m *fakeManager) Connect(stream grpc.BidiStreamingServer[pb.AgentMessage, pb.ManagerMessage]) error {
	md, _ := metadata.FromIncomingContext(stream.Context())
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	if err := stream.Send(&pb.ManagerMessage{Msg: &pb.ManagerMessage_Welcome{Welcome: &pb.Welcome{StatusIntervalSeconds: 5}}}); err != nil {
		return err
	}
	s := &managerSession{md: md, hello: first.GetHello(), stream: stream, recv: make(chan *pb.AgentMessage, 100), done: make(chan struct{})}
	m.sessions <- s
	defer close(s.done)
	for {
		msg, err := stream.Recv()
		if err != nil {
			return nil
		}
		s.recv <- msg
	}
}

func startManager(t *testing.T, opts ...grpc.ServerOption) (*fakeManager, string) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	m := &fakeManager{sessions: make(chan *managerSession, 4)}
	srv := grpc.NewServer(opts...)
	pb.RegisterProxyAgentServer(srv, m)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return m, lis.Addr().String()
}

func (s *managerSession) await(t *testing.T, match func(*pb.AgentMessage) bool) *pb.AgentMessage {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case m := <-s.recv:
			if match(m) {
				return m
			}
		case <-timeout:
			t.Fatal("timed out waiting for an agent message")
		}
	}
}

func resultFor(id string) func(*pb.AgentMessage) bool {
	return func(m *pb.AgentMessage) bool { return m.GetResult().GetRequestId() == id }
}

func TestSession(t *testing.T) {
	m, addr := startManager(t)
	logDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(logDir, "3.log"), []byte("old\nlast\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &fakeProxy{sum: "boot-sum", logDir: logDir, changed: make(chan struct{})}
	ctx := t.Context()
	go New(Config{ManagerAddr: addr, InstanceID: 42, Token: "secret", Version: "9.9.9"}, p).Run(ctx)

	var s *managerSession
	select {
	case s = <-m.sessions:
	case <-time.After(10 * time.Second):
		t.Fatal("the agent did not connect")
	}
	if got := s.md.Get("authorization"); len(got) != 1 || got[0] != "Bearer secret" {
		t.Fatalf("authorization = %v", got)
	}
	if got := s.md.Get("x-sm-instance"); len(got) != 1 || got[0] != "42" {
		t.Fatalf("x-sm-instance = %v", got)
	}
	if h := s.hello; h.InstanceId != 42 || h.Kind != "nginx" || h.AgentVersion != "9.9.9" || h.ProxyVersion != "nginx/test" || h.ConfigChecksum != "boot-sum" {
		t.Fatalf("hello = %v", h)
	}
	if st := s.await(t, func(m *pb.AgentMessage) bool { return m.GetStatus() != nil }).GetStatus(); !st.ProxyRunning || st.ProxyStartedAt != 1700000000 {
		t.Fatalf("status = %v", st)
	}

	send := func(m *pb.ManagerMessage) {
		t.Helper()
		if err := s.stream.Send(m); err != nil {
			t.Fatal(err)
		}
	}
	send(&pb.ManagerMessage{RequestId: "a1", Msg: &pb.ManagerMessage_Apply{Apply: &pb.Apply{
		Files: map[string]*pb.File{"nginx.conf": {Content: []byte("good"), Mode: 0o600}}, Checksum: "new-sum",
	}}})
	if r := s.await(t, resultFor("a1")).GetResult(); !r.Ok || r.Output != "ok" {
		t.Fatalf("apply result = %v", r)
	}
	if p.Checksum() != "new-sum" || p.files["nginx.conf"].Mode != 0o600 {
		t.Fatalf("proxy got checksum %q files %v", p.Checksum(), p.files)
	}
	send(&pb.ManagerMessage{RequestId: "a2", Msg: &pb.ManagerMessage_Apply{Apply: &pb.Apply{
		Files: map[string]*pb.File{"nginx.conf": {Content: []byte("bad")}}, Checksum: "bad-sum",
	}}})
	if r := s.await(t, resultFor("a2")).GetResult(); r.Ok || r.Error != "rejected" || r.Output != "emerg" {
		t.Fatalf("bad apply result = %v", r)
	}
	send(&pb.ManagerMessage{RequestId: "p1", Msg: &pb.ManagerMessage_Ping{Ping: &pb.Ping{}}})
	if r := s.await(t, resultFor("p1")).GetResult(); !r.Ok {
		t.Fatalf("ping result = %v", r)
	}

	send(&pb.ManagerMessage{Msg: &pb.ManagerMessage_StartTail{StartTail: &pb.StartTail{StreamId: "t1", Lines: 1}}})
	isLog := func(m *pb.AgentMessage) bool { return m.GetLog().GetStreamId() == "t1" }
	first := s.await(t, isLog).GetLog()
	if len(first.Lines) != 1 || first.Lines[0].Text != "last" || first.Lines[0].Source != "3.log" {
		t.Fatalf("first tail chunk = %v", first)
	}
	f, _ := os.OpenFile(filepath.Join(logDir, "3.log"), os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString("fresh\n")
	_ = f.Close()
	if c := s.await(t, isLog).GetLog(); len(c.Lines) != 1 || c.Lines[0].Text != "fresh" {
		t.Fatalf("followed tail chunk = %v", c)
	}
	send(&pb.ManagerMessage{Msg: &pb.ManagerMessage_StopTail{StopTail: &pb.StopTail{StreamId: "t1"}}})
	if c := s.await(t, isLog).GetLog(); !c.Closed || c.Error != "" {
		t.Fatalf("tail did not close cleanly: %v", c)
	}
}

func selfSigned(t *testing.T) (tls.Certificate, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "manager"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, hex.EncodeToString(sum[:])
}

func TestTLSPinning(t *testing.T) {
	cert, fp := selfSigned(t)
	m, addr := startManager(t, grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{cert}})))

	run := func(pin string) bool {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		p := &fakeProxy{logDir: t.TempDir(), changed: make(chan struct{})}
		go New(Config{ManagerAddr: addr, InstanceID: 1, Token: "t", TLS: true, CertSHA256: pin}, p).Run(ctx)
		select {
		case <-m.sessions:
			return true
		case <-ctx.Done():
			return false
		}
	}
	if !run(fp) {
		t.Fatal("the agent did not connect with the right fingerprint")
	}
	wrong := make([]byte, 32)
	if run(hex.EncodeToString(wrong)) {
		t.Fatal("the agent connected although the fingerprint does not match")
	}
}
