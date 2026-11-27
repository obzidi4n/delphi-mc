package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	goruntime "runtime"
	"strconv"
	"strings"

	"delphi.dev/delphi/internal/wire"
)

// Runtime is the container lifecycle the supervisor drives. The exec
// implementation shells out to podman or docker; tests use a fake.
type Runtime interface {
	Name() string
	Ensure(ctx context.Context, spec wire.ServerSpec, dir string) error
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	Kill(ctx context.Context, id string) error
	Restart(ctx context.Context, id string) error
	State(ctx context.Context, id string) (string, error)
	Remove(ctx context.Context, id string) error
	AppliedRevision(ctx context.Context, id string) (int64, error)
	Logs(ctx context.Context, id string, tail int) (string, error)
}

// DetectRuntime picks podman (default), docker, or the configured one.
func DetectRuntime(pref string) (Runtime, error) {
	var candidates []string
	switch pref {
	case "", "auto":
		candidates = []string{"podman", "docker"}
	case "podman", "docker":
		candidates = []string{pref}
	default:
		return nil, fmt.Errorf("container_runtime must be auto, podman or docker (got %q)", pref)
	}
	for _, bin := range candidates {
		if _, err := exec.LookPath(bin); err == nil {
			return &execRuntime{bin: bin, podman: bin == "podman"}, nil
		}
	}
	return nil, errors.New("no container runtime found: install rootless podman or docker")
}

type execRuntime struct {
	bin    string
	podman bool
}

func (r *execRuntime) Name() string { return r.bin }

func containerName(id string) string { return "delphi-" + id }

func (r *execRuntime) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, r.bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", r.bin, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (r *execRuntime) Ensure(ctx context.Context, spec wire.ServerSpec, dir string) error {
	_, err := r.run(ctx, runArgs(r, spec, dir)...)
	return err
}

func (r *execRuntime) Start(ctx context.Context, id string) error {
	_, err := r.run(ctx, "start", containerName(id))
	return err
}

func (r *execRuntime) Stop(ctx context.Context, id string) error {
	_, err := r.run(ctx, "stop", "-t", "30", containerName(id))
	return ignoreStateError(err)
}

func (r *execRuntime) Kill(ctx context.Context, id string) error {
	_, err := r.run(ctx, "kill", containerName(id))
	return ignoreStateError(err)
}

func (r *execRuntime) Restart(ctx context.Context, id string) error {
	_, err := r.run(ctx, "restart", "-t", "30", containerName(id))
	return ignoreStateError(err)
}

func (r *execRuntime) State(ctx context.Context, id string) (string, error) {
	out, err := r.run(ctx, "inspect", "-f", "{{.State.Status}}", containerName(id))
	if err != nil {
		if isMissing(err) {
			return "missing", nil
		}
		return "", err
	}
	switch out {
	case "running":
		return "running", nil
	case "exited":
		return "exited", nil
	case "created", "configured":
		return "created", nil
	case "paused":
		return "paused", nil
	default:
		return out, nil
	}
}

func (r *execRuntime) AppliedRevision(ctx context.Context, id string) (int64, error) {
	out, err := r.run(ctx, "inspect", "-f", `{{index .Config.Labels "delphi.revision"}}`, containerName(id))
	if err != nil {
		if isMissing(err) {
			return 0, nil
		}
		return 0, err
	}
	revision, _ := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	return revision, nil
}

func (r *execRuntime) Remove(ctx context.Context, id string) error {
	_, err := r.run(ctx, "rm", "-f", containerName(id))
	return ignoreStateError(err)
}

func (r *execRuntime) Logs(ctx context.Context, id string, tail int) (string, error) {
	return r.run(ctx, "logs", "--tail", strconv.Itoa(tail), containerName(id))
}

// runArgs builds the run command. It is a pure function so the flags are
// testable without a runtime.
func runArgs(r *execRuntime, spec wire.ServerSpec, dir string) []string {
	// Paper's JVM needs more than the heap: metaspace, code cache, Netty, and
	// native compression. 1 GiB of container with a 768 MiB heap OOMs at the
	// cgroup boundary during world gen, so leave 512 MiB of headroom.
	heap := spec.MemoryMB - 512
	if heap < 256 {
		heap = 256
	}

	cores := spec.CPUCores
	if cores <= 0 {
		cores = 1
	}

	args := []string{
		"run", "-d",
		"--name", containerName(spec.ID),
		"--label", "delphi.server=" + spec.ID,
		"--label", fmt.Sprintf("delphi.revision=%d", spec.Revision),
		"-v", dir + ":/data",
		"-w", "/data",
		"-p", fmt.Sprintf("%d:%d", spec.Port, spec.Port),
		"--memory", fmt.Sprintf("%dm", spec.MemoryMB),
		"--cpus", strconv.FormatFloat(cores, 'f', -1, 64),
		"--shm-size", "512m",
		"--read-only", "--tmpfs", "/tmp",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"-e", "HOME=/data",
	}

	// On Linux, keep host files owned by the invoking user. On macOS the
	// machine's virtiofs share already maps ownership, and keep-id breaks.
	if r.podman && goruntime.GOOS == "linux" {
		args = append(args,
			"--userns=keep-id",
			"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		)
	}

	args = append(args,
		spec.Image,
		"java",
		fmt.Sprintf("-Xms%dm", heap/2),
		fmt.Sprintf("-Xmx%dm", heap),
		"-jar", "paper.jar",
	)
	if spec.Base != "velocity" {
		// Velocity has no --nogui; it runs headless from velocity.toml.
		args = append(args, "--nogui")
	}
	return args
}

func isMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such") || strings.Contains(msg, "not found") || strings.Contains(msg, "does not exist")
}

func ignoreStateError(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "not running") || strings.Contains(msg, "is not running") || isMissing(err) {
		return nil
	}
	return err
}
