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

func TestContainerName(t *testing.T) {
	if got := containerName("s_abc"); got != "delphi-s_abc" {
		t.Fatalf("containerName = %q", got)
	}
}
