package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"delphi.dev/delphi/internal/upgrade"
	"delphi.dev/delphi/internal/wire"
)

// HandleAgentUpdate downloads and verifies a signed binary, keeps a rollback
// slot, and re-execs the agent in place.
func HandleAgentUpdate(ctx context.Context, client *http.Client, panelURL, publicKey string, frame wire.AgentUpdateFrame, log *slog.Logger) {
	keyBytes, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil || len(keyBytes) != ed25519.PublicKeySize {
		log.Warn("agent update rejected: no valid update_public_key")
		return
	}
	sig, err := base64.StdEncoding.DecodeString(frame.Signature)
	if err != nil {
		log.Warn("agent update rejected: signature encoding", "err", err)
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	tmp, err := os.CreateTemp(filepath.Dir(exe), ".agent-update-*")
	if err != nil {
		log.Warn("agent update temp", "err", err)
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	url := panelURL + "/agent/updates/" + frame.SHA256
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		tmp.Close()
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		tmp.Close()
		log.Warn("agent update download", "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		tmp.Close()
		log.Warn("agent update download", "status", resp.Status)
		return
	}
	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hasher), resp.Body); err != nil {
		tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	sum := hasher.Sum(nil)
	if hex.EncodeToString(sum) != frame.SHA256 {
		log.Warn("agent update rejected: sha mismatch")
		return
	}
	if !ed25519.Verify(ed25519.PublicKey(keyBytes), sum, sig) {
		log.Warn("agent update rejected: bad signature")
		return
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return
	}
	// Pre-flight: the candidate must answer version before it replaces us.
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	out, err := exec.CommandContext(checkCtx, tmpName, "version").CombinedOutput()
	cancel()
	if err != nil {
		log.Warn("agent update rejected: candidate failed version check", "err", err, "out", string(out))
		return
	}
	if _, err := upgrade.Swap(exe, tmpName); err != nil {
		log.Warn("agent update install", "err", err)
		return
	}
	log.Info("agent update installed; re-exec", "version", frame.Version, "sha256", frame.SHA256)
	if err := syscall.Exec(exe, os.Args, os.Environ()); err != nil {
		log.Error("agent re-exec failed; rollback binary is next to the exe", "err", err)
	}
}
