package agent

import (
	"strings"
	"testing"

	"delphi.dev/delphi/internal/wire"
)

func TestRunArgs(t *testing.T) {
	rt := &execRuntime{bin: "podman", podman: true}
	spec := wire.ServerSpec{
		ID:           "s_1",
		Name:         "survival-1",
		Revision:     3,
		Image:        "delphi/java:21-temurin",
		MemoryMB:     1024,
		CPUCores:     2,
		Port:         25565,
		JarURL:       "https://example.invalid/paper.jar",
		JarSHA256:    strings.Repeat("a", 64),
		DesiredState: "running",
	}

	joined := strings.Join(runArgs(rt, spec, "/var/lib/delphi/servers/s_1"), " ")

	for _, want := range []string{
		"run -d",
		"--name delphi-s_1",
		"--label delphi.server=s_1",
		"--label delphi.revision=3",
		"-v /var/lib/delphi/servers/s_1:/data",
		"-p 25565:25565",
		"--memory 1024m",
		"--cpus 2",
		"--read-only",
		"--cap-drop ALL",
		"delphi/java:21-temurin",
		"java -Xms256m -Xmx512m -jar paper.jar --nogui",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("run args missing %q in %s", want, joined)
		}
	}
}

func TestRunArgsVelocityOmitsNoGUI(t *testing.T) {
	rt := &execRuntime{bin: "podman", podman: true}
	spec := wire.ServerSpec{
		ID: "s_v", Name: "proxy", Revision: 1, Base: "velocity",
		Image: "delphi/java:17-temurin", MemoryMB: 1024, CPUCores: 1, Port: 25566,
		JarURL: "https://example.invalid/velocity.jar", JarSHA256: strings.Repeat("a", 64),
	}
	joined := strings.Join(runArgs(rt, spec, "/var/lib/delphi/servers/s_v"), " ")
	if strings.Contains(joined, "--nogui") {
		t.Fatalf("velocity got --nogui: %s", joined)
	}
	if !strings.Contains(joined, "-p 25566:25566") {
		t.Fatalf("port mapping wrong: %s", joined)
	}
}

func TestContainerName(t *testing.T) {
	if got := containerName("s_abc"); got != "delphi-s_abc" {
		t.Fatalf("containerName = %q", got)
	}
}

func TestNetworkRunArgsPublishOnlyProxy(t *testing.T) {
	r := &execRuntime{bin: "podman", podman: true}
	backend := runArgs(r, wire.ServerSpec{
		ID: "s_1", Port: 25566, MemoryMB: 1024, CPUCores: 1,
		Image: "img", JarURL: "x", JarSHA256: "y", NetworkName: "delphi-net-n_1", NetworkRole: "backend",
	}, "/tmp/s_1")
	joined := strings.Join(backend, " ")
	if !strings.Contains(joined, "--network delphi-net-n_1") {
		t.Fatalf("backend lacks network: %v", backend)
	}
	if strings.Contains(joined, "-p ") {
		t.Fatalf("backend publishes a host port: %v", backend)
	}

	proxy := runArgs(r, wire.ServerSpec{
		ID: "s_2", Port: 25565, MemoryMB: 1024, CPUCores: 1,
		Image: "img", JarURL: "x", JarSHA256: "y", Base: "velocity",
		NetworkName: "delphi-net-n_1", NetworkRole: "proxy",
	}, "/tmp/s_2")
	joined = strings.Join(proxy, " ")
	if !strings.Contains(joined, "--network delphi-net-n_1") || !strings.Contains(joined, "-p 25565:25565") {
		t.Fatalf("proxy args = %v", proxy)
	}
	if strings.Contains(joined, "--nogui") {
		t.Fatalf("velocity should not get --nogui: %v", proxy)
	}
}
