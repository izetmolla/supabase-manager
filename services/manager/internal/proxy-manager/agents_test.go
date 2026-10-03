package proxymanager

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/supabase-manager/manager/internal/auth"
	"github.com/supabase-manager/manager/internal/models"
	"github.com/supabase-manager/manager/internal/proxy-manager/spec"
	"github.com/supabase-manager/manager/internal/settings"
	pb "github.com/supabase-manager/shared/proxyagent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(append(models.All(), Models()...)...); err != nil {
		t.Fatal(err)
	}
	cipher, err := auth.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	s := New(db, nil, cipher, nil, nil, settings.New(db), Options{AgentAddr: "127.0.0.1:7070"})
	s.enabled = true
	return s
}

func newTestInstance(t *testing.T, s *Service, kind string) (*ProxyInstance, string) {
	t.Helper()
	token := randomToken()
	in := &ProxyInstance{Name: "edge-" + kind, Kind: kind, Image: s.DefaultImage(kind), HTTPPort: 8081, Enabled: true, TokenEncrypted: s.encrypt(token)}
	if err := s.db.Create(in).Error; err != nil {
		t.Fatal(err)
	}
	return in, token
}

// startHub serves the hub over an in-memory listener and returns a client for it.
func startHub(t *testing.T, s *Service) pb.ProxyAgentClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	pb.RegisterProxyAgentServer(srv, s.agents)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return pb.NewProxyAgentClient(conn)
}

func connect(ctx context.Context, t *testing.T, c pb.ProxyAgentClient, id uint, token string, hello *pb.Hello) (grpc.BidiStreamingClient[pb.AgentMessage, pb.ManagerMessage], error) {
	t.Helper()
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token, "x-sm-instance", strconv.FormatUint(uint64(id), 10))
	st, err := c.Connect(ctx)
	if err != nil {
		return nil, err
	}
	if err := st.Send(&pb.AgentMessage{Msg: &pb.AgentMessage_Hello{Hello: hello}}); err != nil {
		return nil, err
	}
	m, err := st.Recv()
	if err != nil {
		return nil, err
	}
	if m.GetWelcome() == nil {
		t.Fatalf("first manager message is %T, want Welcome", m.Msg)
	}
	return st, nil
}

func TestAgentHubRoundTrip(t *testing.T) {
	s := newTestService(t)
	in, token := newTestInstance(t, s, spec.KindNginx)
	client := startHub(t, s)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if s.agents.info(in.ID).Connected {
		t.Fatal("agent reported connected before connecting")
	}
	waited := make(chan error, 1)
	go func() { waited <- s.agents.waitConnected(ctx, in.ID, 10*time.Second) }()

	st, err := connect(ctx, t, client, in.ID, token, &pb.Hello{InstanceId: uint64(in.ID), Kind: spec.KindNginx, AgentVersion: "1.2.3", ProxyVersion: "nginx/1.27"})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-waited; err != nil {
		t.Fatalf("waitConnected: %v", err)
	}
	info := s.agents.info(in.ID)
	if !info.Connected || info.AgentVersion != "1.2.3" || info.ProxyVersion != "nginx/1.27" {
		t.Fatalf("info after hello = %+v", info)
	}

	// The fake agent answers every command and reports a status after an apply.
	go func() {
		for {
			m, err := st.Recv()
			if err != nil {
				return
			}
			switch x := m.Msg.(type) {
			case *pb.ManagerMessage_Apply:
				ok := string(x.Apply.Files["nginx.conf"].Content) != "bad"
				res := &pb.Result{RequestId: m.RequestId, Ok: ok, Output: "syntax is ok"}
				if !ok {
					res.Error = "nginx rejected the configuration"
				}
				_ = st.Send(&pb.AgentMessage{Msg: &pb.AgentMessage_Status{Status: &pb.Status{ProxyRunning: true, ConfigChecksum: x.Apply.Checksum, Restarts: 1}}})
				_ = st.Send(&pb.AgentMessage{Msg: &pb.AgentMessage_Result{Result: res}})
			case *pb.ManagerMessage_StartTail:
				_ = st.Send(&pb.AgentMessage{Msg: &pb.AgentMessage_Log{Log: &pb.LogChunk{StreamId: x.StartTail.StreamId, Lines: []*pb.LogLine{
					{Source: "7.log", Text: `{"method":"GET","uri":"/a","status":200,"host":"a.test"}`},
					{Source: "8.log", Text: `{"method":"POST","uri":"/b","status":500,"host":"b.test"}`},
				}}}})
				_ = st.Send(&pb.AgentMessage{Msg: &pb.AgentMessage_Log{Log: &pb.LogChunk{StreamId: x.StartTail.StreamId, Closed: true}}})
			}
		}
	}()

	apply := func(conf string) *pb.Result {
		t.Helper()
		res, err := s.agents.call(ctx, in.ID, &pb.ManagerMessage{Msg: &pb.ManagerMessage_Apply{Apply: &pb.Apply{
			Files: agentFiles(map[string]string{"nginx.conf": conf}), Checksum: "sum-" + conf,
		}}})
		if err != nil {
			t.Fatalf("call: %v", err)
		}
		return res
	}
	if res := apply("good"); !res.Ok || res.Output != "syntax is ok" {
		t.Fatalf("apply good = %+v", res)
	}
	if res := apply("bad"); res.Ok || res.Error == "" {
		t.Fatalf("apply bad = %+v", res)
	}
	info = s.agents.info(in.ID)
	if !info.ProxyRunning || info.ConfigChecksum != "sum-bad" || info.Restarts != 1 {
		t.Fatalf("status not recorded: %+v", info)
	}

	var got []AccessEntry
	if err := s.StreamAccessLog(ctx, in.ID, 8, 10, func(e AccessEntry) error { got = append(got, e); return nil }); err != nil {
		t.Fatalf("StreamAccessLog: %v", err)
	}
	if len(got) != 1 || got[0].HostID != 8 || got[0].Status != 500 || got[0].Host != "b.test" {
		t.Fatalf("access entries = %+v", got)
	}

	_ = st.CloseSend()
	deadline := time.Now().Add(5 * time.Second)
	for s.agents.info(in.ID).Connected {
		if time.Now().After(deadline) {
			t.Fatal("agent still connected after closing the stream")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := s.agents.call(ctx, in.ID, &pb.ManagerMessage{Msg: &pb.ManagerMessage_Ping{Ping: &pb.Ping{}}}); err != ErrAgentOffline {
		t.Fatalf("call after disconnect = %v, want ErrAgentOffline", err)
	}
}

func TestAgentHubRejects(t *testing.T) {
	s := newTestService(t)
	in, token := newTestInstance(t, s, spec.KindNginx)
	other, _ := newTestInstance(t, s, spec.KindTraefik)
	client := startHub(t, s)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cases := []struct {
		name  string
		id    uint
		token string
		hello *pb.Hello
		code  codes.Code
	}{
		{"wrong token", in.ID, "nope", &pb.Hello{InstanceId: uint64(in.ID), Kind: spec.KindNginx}, codes.Unauthenticated},
		{"token of another instance", other.ID, token, &pb.Hello{InstanceId: uint64(other.ID), Kind: spec.KindTraefik}, codes.Unauthenticated},
		{"hello for another instance", in.ID, token, &pb.Hello{InstanceId: uint64(other.ID), Kind: spec.KindNginx}, codes.InvalidArgument},
		{"wrong proxy kind", in.ID, token, &pb.Hello{InstanceId: uint64(in.ID), Kind: spec.KindTraefik}, codes.FailedPrecondition},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := connect(ctx, t, client, tc.id, tc.token, tc.hello)
			if status.Code(err) != tc.code {
				t.Fatalf("err = %v, want code %s", err, tc.code)
			}
		})
	}

	s.stateMu.Lock()
	s.enabled = false
	s.stateMu.Unlock()
	if _, err := connect(ctx, t, client, in.ID, token, &pb.Hello{InstanceId: uint64(in.ID), Kind: spec.KindNginx}); status.Code(err) != codes.Unavailable {
		t.Fatalf("connect while disabled = %v, want Unavailable", err)
	}
}

func TestAgentHubResyncOnConnect(t *testing.T) {
	s := newTestService(t)
	in, token := newTestInstance(t, s, spec.KindNginx)
	enc, err := s.storeFiles(map[string]string{"nginx.conf": "deployed"})
	if err != nil {
		t.Fatal(err)
	}
	rev := &ConfigRevision{InstanceID: in.ID, Number: 1, Checksum: "deployed-sum", FilesEncrypted: enc, Status: RevisionApplied}
	if err := s.db.Create(rev).Error; err != nil {
		t.Fatal(err)
	}
	s.db.Model(in).Updates(map[string]any{"deployed_revision_id": rev.ID, "deployed_checksum": rev.Checksum})
	client := startHub(t, s)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	st, err := connect(ctx, t, client, in.ID, token, &pb.Hello{InstanceId: uint64(in.ID), Kind: spec.KindNginx, ConfigChecksum: ""})
	if err != nil {
		t.Fatal(err)
	}
	m, err := st.Recv()
	if err != nil {
		t.Fatal(err)
	}
	a := m.GetApply()
	if a == nil || a.Checksum != "deployed-sum" || string(a.Files["nginx.conf"].Content) != "deployed" {
		t.Fatalf("expected the deployed revision to be pushed, got %v", m)
	}
}

func TestAgentCertificateIsStable(t *testing.T) {
	s := newTestService(t)
	_, fp1, err := s.agentCertificate()
	if err != nil {
		t.Fatal(err)
	}
	_, fp2, err := s.agentCertificate()
	if err != nil {
		t.Fatal(err)
	}
	if fp1 == "" || fp1 != fp2 {
		t.Fatalf("fingerprints differ: %q %q", fp1, fp2)
	}
}

func TestAgentAddressing(t *testing.T) {
	for _, tc := range []struct {
		listen, dial string
		tls          bool
	}{
		{"127.0.0.1:7070", "127.0.0.1:7070", false},
		{"0.0.0.0:7070", "127.0.0.1:7070", true},
		{":7070", "127.0.0.1:7070", true},
		{"10.0.0.5:7070", "10.0.0.5:7070", true},
	} {
		s := &Service{opts: Options{AgentAddr: tc.listen}}
		if got := s.agentDialAddr(); got != tc.dial {
			t.Errorf("%s: dial = %s, want %s", tc.listen, got, tc.dial)
		}
		if got := s.agentTLS(); got != tc.tls {
			t.Errorf("%s: tls = %v, want %v", tc.listen, got, tc.tls)
		}
	}
	if ImageTag("", "1.4.0") != "1.4.0" || ImageTag("", "(untracked)") != "latest" || ImageTag("edge", "1.4.0") != "edge" {
		t.Error("ImageTag picks the wrong tag")
	}
	if !legacyImage("nginx:stable-alpine") || !legacyImage("docker.io/library/traefik:v3") || legacyImage("izetmolla/supabase-manager-proxy-nginx:1.0.0") {
		t.Error("legacyImage misclassifies images")
	}
}
