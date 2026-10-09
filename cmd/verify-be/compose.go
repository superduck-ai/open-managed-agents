package main

import (
	"fmt"
	"strings"
)

type healthcheck struct {
	Test     []string `json:"test"`
	Interval string   `json:"interval"`
	Timeout  string   `json:"timeout"`
	Retries  int      `json:"retries"`
}

type composeService struct {
	Image       string            `json:"image"`
	Ports       []string          `json:"ports"`
	Healthcheck healthcheck       `json:"healthcheck"`
	Environment map[string]string `json:"environment,omitempty"`
	Command     []string          `json:"command,omitempty"`
	Volumes     []string          `json:"volumes,omitempty"`
}

type composeSpec struct {
	Services map[string]*composeService `json:"services"`
}

func service(image string, port int, check string) *composeService {
	return &composeService{Image: image, Ports: []string{fmt.Sprintf("127.0.0.1::%d", port)},
		Healthcheck: healthcheck{Test: []string{"CMD-SHELL", check}, Interval: "2s", Timeout: "2s", Retries: 45}}
}

func dependencySpec(images map[string]string) composeSpec {
	services := map[string]*composeService{
		"postgres": service(images["postgres"], 5432, "pg_isready -U verify -d verify"),
		"redis":    service(images["redis"], 6379, "redis-cli ping"),
		"minio":    service(images["minio"], 9000, "mc ready local"),
	}
	services["postgres"].Environment = map[string]string{"POSTGRES_USER": "verify", "POSTGRES_PASSWORD": "verify", "POSTGRES_DB": "verify"}
	services["minio"].Command = []string{"server", "/data"}
	services["minio"].Environment = map[string]string{"MINIO_ROOT_USER": "verifychat", "MINIO_ROOT_PASSWORD": "verifychat-local-only"}
	for i := 1; i <= 3; i++ {
		name := fmt.Sprintf("nats%d", i)
		s := service(images["nats"], 4222, "wget -qO- 'http://localhost:8222/healthz?js-enabled-only=true'")
		routes := []string{}
		for peer := 1; peer <= 3; peer++ {
			if peer != i {
				routes = append(routes, fmt.Sprintf("nats://nats%d:6222", peer))
			}
		}
		s.Command = []string{"--name", name, "--jetstream", "--store_dir", "/data", "--http_port", "8222", "--config", "/etc/nats/verify.conf", "--cluster_name", "verify", "--cluster", "nats://0.0.0.0:6222", "--routes", strings.Join(routes, ",")}
		s.Volumes = []string{"./nats.conf:/etc/nats/verify.conf:ro"}
		services[name] = s
	}
	return composeSpec{Services: services}
}
