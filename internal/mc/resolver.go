// Package mc resolves Minecraft server artifacts (Paper, Velocity) from the
// PaperMC API: channel to version to build to a pinned download and sha256.
package mc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrResolve marks failures that are upstream (network, API, no build), as
// opposed to a bad request.
var ErrResolve = errors.New("artifact resolution failed")

// Artifact is one downloadable jar with its verified hash.
type Artifact struct {
	Project   string
	Version   string
	Build     int
	JarURL    string
	JarSHA256 string
}

// Resolver turns a project and version/channel into a concrete artifact.
type Resolver interface {
	Resolve(ctx context.Context, project, version string) (Artifact, error)
}

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Client talks to the PaperMC v3 API (https://fill.papermc.io/v3) and caches
// lookups briefly. v2 was sunset.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	Now     func() time.Time
	TTL     time.Duration

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	artifact Artifact
	expires  time.Time
}

// NewClient returns a resolver for the public PaperMC API.
func NewClient(hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	return &Client{
		BaseURL: "https://fill.papermc.io/v3",
		HTTP:    hc,
		Now:     time.Now,
		TTL:     10 * time.Minute,
		cache:   map[string]cached{},
	}
}

type projectResponse struct {
	Project struct {
		ID string `json:"id"`
	} `json:"project"`
	// Versions are grouped by minor ("1.21": ["1.21.11", ...]).
	Versions map[string][]string `json:"versions"`
}

type download struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Checksums struct {
		SHA256 string `json:"sha256"`
	} `json:"checksums"`
}

type build struct {
	ID        int                 `json:"id"`
	Channel   string              `json:"channel"` // STABLE | BETA | ALPHA
	Downloads map[string]download `json:"downloads"`
}

// Resolve pins version/channel to a build and its sha256.
func (c *Client) Resolve(ctx context.Context, project, version string) (Artifact, error) {
	switch project {
	case "paper", "velocity":
	default:
		return Artifact{}, fmt.Errorf("%w: project %q is not supported", ErrResolve, project)
	}

	key := project + "|" + version
	c.mu.Lock()
	if hit, ok := c.cache[key]; ok && c.Now().Before(hit.expires) {
		c.mu.Unlock()
		return hit.artifact, nil
	}
	c.mu.Unlock()

	var meta projectResponse
	if err := c.get(ctx, "/projects/"+project, &meta); err != nil {
		return Artifact{}, err
	}
	resolved, err := pickVersion(flattenVersions(meta.Versions), version)
	if err != nil {
		return Artifact{}, err
	}

	var builds []build
	if err := c.get(ctx, "/projects/"+project+"/versions/"+resolved+"/builds", &builds); err != nil {
		return Artifact{}, err
	}
	chosen, err := pickBuild(builds, version)
	if err != nil {
		return Artifact{}, fmt.Errorf("%w: %v", ErrResolve, err)
	}
	file, ok := chosen.Downloads["server:default"]
	if !ok {
		for _, candidate := range chosen.Downloads {
			file = candidate
			ok = true
			break
		}
	}
	if !ok {
		return Artifact{}, fmt.Errorf("%w: build %d has no download", ErrResolve, chosen.ID)
	}
	sum := strings.ToLower(file.Checksums.SHA256)
	if !sha256Pattern.MatchString(sum) {
		return Artifact{}, fmt.Errorf("%w: build %d has no sha256", ErrResolve, chosen.ID)
	}
	if !strings.HasPrefix(file.URL, "https://") {
		return Artifact{}, fmt.Errorf("%w: build %d has no https download", ErrResolve, chosen.ID)
	}
	artifact := Artifact{
		Project:   project,
		Version:   resolved,
		Build:     chosen.ID,
		JarURL:    file.URL,
		JarSHA256: sum,
	}
	c.mu.Lock()
	c.cache[key] = cached{artifact: artifact, expires: c.Now().Add(c.TTL)}
	c.mu.Unlock()
	return artifact, nil
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrResolve, err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrResolve, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%w: %s: %s", ErrResolve, resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("%w: decode %s: %v", ErrResolve, path, err)
	}
	return nil
}

// pickVersion resolves "latest"/"recommended" to the newest stable version,
// or checks an explicit version exists.
func pickVersion(versions []string, want string) (string, error) {
	if want != "latest" && want != "recommended" {
		for _, v := range versions {
			if v == want {
				return v, nil
			}
		}
		return "", fmt.Errorf("%w: version %q is not available", ErrResolve, want)
	}
	var stable []string
	for _, v := range versions {
		if !strings.Contains(v, "-") {
			stable = append(stable, v)
		}
	}
	if len(stable) == 0 {
		return "", fmt.Errorf("%w: no stable versions", ErrResolve)
	}
	sort.Slice(stable, func(i, j int) bool { return versionLess(stable[i], stable[j]) })
	return stable[len(stable)-1], nil
}

// pickBuild takes the newest build; "recommended" prefers the default channel.
func pickBuild(builds []build, version string) (build, error) {
	if len(builds) == 0 {
		return build{}, errors.New("no builds")
	}
	usable := builds
	if version == "recommended" {
		var stable []build
		for _, b := range builds {
			if b.Channel == "STABLE" {
				stable = append(stable, b)
			}
		}
		if len(stable) > 0 {
			usable = stable
		}
	}
	sort.Slice(usable, func(i, j int) bool { return usable[i].ID < usable[j].ID })
	return usable[len(usable)-1], nil
}

func flattenVersions(grouped map[string][]string) []string {
	var out []string
	for _, versions := range grouped {
		out = append(out, versions...)
	}
	return out
}

func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var ai, bi int
		if i < len(pa) {
			ai, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			bi, _ = strconv.Atoi(pb[i])
		}
		if ai != bi {
			return ai < bi
		}
	}
	return a < b
}
