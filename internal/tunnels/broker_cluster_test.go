package tunnels

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

func TestNATSBrokerRejectsInsufficientReplicas(t *testing.T) {
	srv := startTunnelNATS(t, server.Options{})
	if _, err := NewBroker(t.Context(), connectTunnelNATS(t, srv.ClientURL()), brokerTestConfig()); err == nil {
		t.Fatal("production broker accepted one node")
	}
}

func TestNATSBrokerClusterRecoversCommittedTerminalAfterLeaderLoss(t *testing.T) {
	servers := startTunnelNATSCluster(t)
	urls := make([]string, 0, len(servers))
	for _, srv := range servers {
		urls = append(urls, srv.ClientURL())
	}
	brokers := make([]*Broker, 2)
	for i := range brokers {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		broker, err := NewBroker(ctx, connectTunnelNATS(t, strings.Join(urls, ",")), brokerTestConfig())
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(broker.Close)
		brokers[i] = broker
	}
	channels := []ChannelDeclaration{{Name: "main"}}
	registerTestConnector(t, brokers[1], "a", channels)
	command := testQueuedCommand("cluster-recovery")
	waiter, err := brokers[0].subscribeResponse(t.Context(), "tunnel", command.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	defer waiter.Close()
	if err := brokers[0].Enqueue(t.Context(), "tunnel", command); err != nil {
		t.Fatal(err)
	}
	commands := pollTestCommands(t, brokers[1], "a", channels, 1)
	if len(commands) != 1 {
		t.Fatal("command was not dispatched")
	}
	if err := brokers[0].responseHub.subscription.Unsubscribe(); err != nil {
		t.Fatal(err)
	}
	if err := brokers[1].SubmitResponse(t.Context(), "tunnel", "a", 1, commands[0].ShardToken, testTerminalResponse(command.RequestID)); err != nil {
		t.Fatal(err)
	}
	info, err := brokers[1].requests.stream.Info(t.Context())
	if err != nil || info.Cluster == nil {
		t.Fatalf("cluster info = %#v %v", info, err)
	}
	if info.Config.Replicas != 3 {
		t.Fatalf("replicas = %d", info.Config.Replicas)
	}
	var stopped *server.Server
	for _, srv := range servers {
		if srv.Name() == info.Cluster.Leader {
			srv.Shutdown()
			srv.WaitForShutdown()
			stopped = srv
			break
		}
	}
	if stopped == nil {
		t.Fatal("request stream leader not found")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	response, err := waiter.Wait(ctx, nil)
	if err != nil || response.RequestID != command.RequestID {
		t.Fatalf("after leader loss = %+v, %v", response, err)
	}
	for _, srv := range servers {
		if srv != stopped {
			srv.Shutdown()
			srv.WaitForShutdown()
			break
		}
	}
	ctx, cancelWrite := context.WithTimeout(t.Context(), time.Second)
	defer cancelWrite()
	err = brokers[1].Enqueue(ctx, "tunnel", testQueuedCommand("without-quorum"))
	if err == nil || errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("quorum loss admission = %v", err)
	}
}

func startTunnelNATSCluster(t *testing.T) []*server.Server {
	t.Helper()
	ports := make([]int, 3)
	for i := range ports {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		ports[i] = listener.Addr().(*net.TCPAddr).Port
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
	}
	servers := make([]*server.Server, 0, 3)
	for i := range ports {
		routes := make([]*url.URL, 0, 2)
		for j, port := range ports {
			if i == j {
				continue
			}
			route, err := url.Parse("nats-route://127.0.0.1:" + strconv.Itoa(port))
			if err != nil {
				t.Fatal(err)
			}
			routes = append(routes, route)
		}
		servers = append(servers, startTunnelNATS(t, server.Options{ServerName: "tunnel-test-" + strconv.Itoa(i),
			Cluster: server.ClusterOpts{Name: "tunnel-test", Host: "127.0.0.1", Port: ports[i]}, Routes: routes}))
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, srv := range servers {
			if srv.JetStreamIsLeader() && len(srv.JetStreamClusterPeers()) == 3 {
				return servers
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("NATS cluster did not converge")
	return nil
}
