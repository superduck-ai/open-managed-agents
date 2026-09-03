package workerevents

import (
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	server "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func TestJetStreamBrokerRejectsInsufficientReplicas(t *testing.T) {
	srv := runNATSServer(t, server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir()})
	connection := connectNATS(t, srv.ClientURL())
	if _, err := NewJetStream(t.Context(), connection); err == nil {
		t.Fatal("NewJetStream() error = nil, want three-replica stream failure")
	}
}

func TestJetStreamBrokerDeliversFullEnvelopeAndCleansConsumer(t *testing.T) {
	servers := runNATSCluster(t)
	publisherConnection := connectNATS(t, servers[0].ClientURL())
	subscriberConnection := connectNATS(t, servers[1].ClientURL())
	publisher, err := NewJetStream(t.Context(), publisherConnection)
	if err != nil {
		t.Fatal(err)
	}
	subscriber, err := NewJetStream(t.Context(), subscriberConnection)
	if err != nil {
		t.Fatal(err)
	}

	sessionID := "csess_test"
	subscription, err := subscriber.Subscribe(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	envelope := EventEnvelope(sessionID, "csev_test", "payload-test", 7, "user.message", "", []byte(`{"text":"hello"}`))
	if err := publisher.Publish(t.Context(), envelope); err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(t.Context(), envelope); err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(t.Context(), EventEnvelope("csess_other", "csev_other", "", 1, "user.message", "", []byte(`{}`))); err != nil {
		t.Fatal(err)
	}

	select {
	case delivery := <-subscription.Messages():
		if delivery.Envelope.EventID != envelope.EventID || string(delivery.Envelope.Payload) != string(envelope.Payload) || delivery.Envelope.SequenceNum != 7 {
			t.Fatalf("delivery = %#v, want full envelope %#v", delivery.Envelope, envelope)
		}
		if err := delivery.Ack(); err != nil {
			t.Fatal(err)
		}
	case err := <-subscription.Errors():
		t.Fatalf("subscription error: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for worker event")
	}
	select {
	case duplicate := <-subscription.Messages():
		t.Fatalf("received duplicate or cross-session event: %#v", duplicate.Envelope)
	case <-time.After(250 * time.Millisecond):
	}
	if err := subscription.Close(); err != nil {
		t.Fatal(err)
	}

	js, err := jetstream.New(subscriberConnection)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := js.Stream(t.Context(), StreamName)
	if err != nil {
		t.Fatal(err)
	}
	info, err := stream.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if info.Config.Replicas != 3 || info.Config.MaxAge != time.Hour || info.Config.MaxBytes != 1<<30 || info.Config.Storage != jetstream.FileStorage {
		t.Fatalf("stream config = %#v", info.Config)
	}
	if info.State.Consumers != 0 {
		t.Fatalf("consumer count = %d, want 0", info.State.Consumers)
	}
}

func runNATSCluster(t *testing.T) []*server.Server {
	t.Helper()
	const count = 3
	ports := make([]int, count)
	for i := range ports {
		ports[i] = freePort(t)
	}
	servers := make([]*server.Server, 0, count)
	for i := 0; i < count; i++ {
		routes := make([]*url.URL, 0, count-1)
		for j, port := range ports {
			if i == j {
				continue
			}
			route, err := url.Parse("nats-route://127.0.0.1:" + fmtInt(port))
			if err != nil {
				t.Fatal(err)
			}
			routes = append(routes, route)
		}
		srv := runNATSServer(t, server.Options{
			ServerName: "worker-events-" + fmtInt(i+1), Host: "127.0.0.1", Port: -1, JetStream: true,
			StoreDir: filepath.Join(t.TempDir(), fmtInt(i)),
			Cluster:  server.ClusterOpts{Name: "worker-events-test", Host: "127.0.0.1", Port: ports[i]},
			Routes:   routes,
		})
		servers = append(servers, srv)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		ready := false
		for _, srv := range servers {
			ready = ready || (srv.JetStreamIsLeader() && len(srv.JetStreamClusterPeers()) == count)
		}
		if ready {
			return servers
		}
		time.Sleep(25 * time.Millisecond)
	}
	for _, srv := range servers {
		t.Logf("server=%s routes=%d clustered=%t leader=%t peers=%v", srv.Name(), srv.NumRoutes(), srv.JetStreamIsClustered(), srv.JetStreamIsLeader(), srv.JetStreamClusterPeers())
	}
	t.Fatal("NATS cluster routes did not converge")
	return nil
}

func runNATSServer(t *testing.T, options server.Options) *server.Server {
	t.Helper()
	srv, err := server.NewServer(&options)
	if err != nil {
		t.Fatal(err)
	}
	srv.Start()
	t.Cleanup(srv.Shutdown)
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS server not ready")
	}
	return srv
}

func connectNATS(t *testing.T, serverURL string) *nats.Conn {
	t.Helper()
	connection, err := nats.Connect(serverURL, nats.ReconnectBufSize(-1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(connection.Close)
	return connection
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func fmtInt(value int) string { return strconv.Itoa(value) }
