// Package plugins resolves plugin versions from Modrinth, Hangar, and GitHub
// releases, and names the resulting jars stably per project.
package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Resolved is one provider artifact.
type Resolved struct {
	Provider string
	Project  string
	Version  string
	Filename string
	URL      string
	SHA256   string
	Size     int64
}

// Resolver talks to the three supported providers.
type Resolver struct {
	Client       *http.Client
	ModrinthBase string
	HangarBase   string
	GitHubBase   string
}

// NewResolver builds a resolver with sane timeouts and endpoints.
func NewResolver(client *http.Client) *Resolver {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Resolver{
		Client:       client,
		ModrinthBase: "https://api.modrinth.com/v2",
		HangarBase:   "https://hangar.papermc.io/api/v1",
		GitHubBase:   "https://api.github.com",
	}
}

// Resolve selects the newest compatible version, honoring an exact pin or a
// version-prefix range.
func (r *Resolver) Resolve(ctx context.Context, provider, project, mcVersion, pin, rng string) (Resolved, error) {
	switch provider {
	case "modrinth":
		return r.modrinth(ctx, project, mcVersion, pin, rng)
	case "hangar":
		return r.hangar(ctx, project, mcVersion, pin, rng)
	case "github":
		return r.github(ctx, project, pin)
	default:
		return Resolved{}, fmt.Errorf("plugins: unsupported provider %q", provider)
	}
}

// CatalogEntry is one plugin project returned by a provider search.
type CatalogEntry struct {
	Provider    string   `json:"provider"`
	Project     string   `json:"project"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Author      string   `json:"author,omitempty"`
	IconURL     string   `json:"icon_url,omitempty"`
	PageURL     string   `json:"page_url,omitempty"`
	Downloads   int64    `json:"downloads,omitempty"`
	Versions    []string `json:"versions,omitempty"`
}

// Search lists plugin projects from Modrinth or Hangar. GitHub has no public
// project search endpoint, so it is intentionally not a catalog provider.
func (r *Resolver) Search(ctx context.Context, provider, query, mcVersion string, limit int) ([]CatalogEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	query = strings.TrimSpace(query)
	switch provider {
	case "modrinth":
		return r.searchModrinth(ctx, query, mcVersion, limit)
	case "hangar":
		return r.searchHangar(ctx, query, limit)
	default:
		return nil, fmt.Errorf("plugins: catalog supports modrinth and hangar, not %q", provider)
	}
}

type modrinthSearch struct {
	Hits []struct {
		Slug        string   `json:"slug"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Author      string   `json:"author"`
		IconURL     string   `json:"icon_url"`
		Downloads   int64    `json:"downloads"`
		Versions    []string `json:"versions"`
	} `json:"hits"`
}

func (r *Resolver) searchModrinth(ctx context.Context, query, mcVersion string, limit int) ([]CatalogEntry, error) {
	base := strings.TrimRight(r.ModrinthBase, "/")
	facets := `[["project_type:plugin"],["categories:paper"]]`
	if mcVersion != "" {
		facets = fmt.Sprintf(`[["project_type:plugin"],["categories:paper"],["versions:%s"]]`, mcVersion)
	}
	q := url.Values{}
	q.Set("query", query)
	q.Set("limit", strconv.Itoa(limit))
	q.Set("facets", facets)
	var body modrinthSearch
	if err := r.get(ctx, base+"/search?"+q.Encode(), &body); err != nil {
		return nil, err
	}
	out := make([]CatalogEntry, 0, len(body.Hits))
	for _, hit := range body.Hits {
		if hit.Slug == "" {
			continue
		}
		out = append(out, CatalogEntry{
			Provider: "modrinth", Project: hit.Slug, Title: hit.Title,
			Description: hit.Description, Author: hit.Author,
			IconURL: hit.IconURL, PageURL: "https://modrinth.com/plugin/" + hit.Slug,
			Downloads: hit.Downloads, Versions: hit.Versions,
		})
	}
	return out, nil
}

type hangarSearch struct {
	Result []struct {
		Name      string `json:"name"`
		Namespace struct {
			Owner string `json:"owner"`
			Slug  string `json:"slug"`
		} `json:"namespace"`
		Description string `json:"description"`
		AvatarURL   string `json:"avatarUrl"`
		Stats       struct {
			Downloads int64 `json:"downloads"`
		} `json:"stats"`
	} `json:"result"`
}

func (r *Resolver) searchHangar(ctx context.Context, query string, limit int) ([]CatalogEntry, error) {
	base := strings.TrimRight(r.HangarBase, "/")
	q := url.Values{}
	if query != "" {
		q.Set("q", query)
	}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("offset", "0")
	q.Set("platform", "PAPER")
	var body hangarSearch
	if err := r.get(ctx, base+"/projects?"+q.Encode(), &body); err != nil {
		return nil, err
	}
	out := make([]CatalogEntry, 0, len(body.Result))
	for _, hit := range body.Result {
		owner, slug := hit.Namespace.Owner, hit.Namespace.Slug
		if owner == "" || slug == "" {
			continue
		}
		project := owner + "/" + slug
		out = append(out, CatalogEntry{
			Provider: "hangar", Project: project, Title: hit.Name,
			Description: hit.Description, Author: owner, IconURL: hit.AvatarURL,
			PageURL: "https://hangar.papermc.io/" + project, Downloads: hit.Stats.Downloads,
		})
	}
	return out, nil
}

// Slug returns a safe, stable jar base name for a project.
func Slug(project string) string {
	s := strings.ToLower(strings.TrimSpace(project))
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, "\\", "-")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "plugin.jar"
	}
	if !strings.HasSuffix(out, ".jar") {
		out += ".jar"
	}
	if len(out) > 120 {
		out = out[:116] + ".jar"
	}
	return out
}

// JarName is the installed filename for a project.
func JarName(project string) string { return Slug(project) }

func (r *Resolver) get(ctx context.Context, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("plugins: request: %w", err)
	}
	req.Header.Set("User-Agent", "delphi/1.0 (minecraft control plane)")
	req.Header.Set("Accept", "application/json")
	resp, err := r.Client.Do(req)
	if err != nil {
		return fmt.Errorf("plugins: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("plugins: %s: %s: %s", rawURL, resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out); err != nil {
		return fmt.Errorf("plugins: decode %s: %w", rawURL, err)
	}
	return nil
}

type modrinthVersion struct {
	ID            string   `json:"id"`
	VersionNumber string   `json:"version_number"`
	GameVersions  []string `json:"game_versions"`
	Loaders       []string `json:"loaders"`
	Files         []struct {
		Filename string `json:"filename"`
		URL      string `json:"url"`
		Size     int64  `json:"size"`
		Primary  bool   `json:"primary"`
		Hashes   struct {
			SHA256 string `json:"sha256"`
			SHA512 string `json:"sha512"`
		} `json:"hashes"`
	} `json:"files"`
}

func (r *Resolver) modrinth(ctx context.Context, project, mcVersion, pin, rng string) (Resolved, error) {
	if strings.TrimSpace(project) == "" {
		return Resolved{}, errors.New("plugins: modrinth project is required")
	}
	base := strings.TrimRight(r.ModrinthBase, "/")
	endpoint := fmt.Sprintf("%s/project/%s/version?loaders=%s",
		base, url.PathEscape(project), url.QueryEscape(`["paper"]`))
	var versions []modrinthVersion
	if err := r.get(ctx, endpoint, &versions); err != nil {
		return Resolved{}, err
	}
	if len(versions) == 0 {
		return Resolved{}, fmt.Errorf("plugins: modrinth project %s has no paper builds", project)
	}

	compatible := func(v modrinthVersion) bool {
		return contains(v.GameVersions, mcVersion) || (len(v.GameVersions) > 0 && sameMajor(v.GameVersions, mcVersion))
	}
	pick := -1
	switch {
	case pin != "":
		for i, v := range versions {
			if v.VersionNumber == pin || v.ID == pin {
				pick = i
				break
			}
		}
		if pick < 0 {
			return Resolved{}, fmt.Errorf("plugins: modrinth %s has no version %q", project, pin)
		}
	case rng != "":
		for i, v := range versions {
			if versionHasPrefix(v.VersionNumber, rng) && compatible(v) {
				pick = i
				break
			}
		}
		if pick < 0 {
			return Resolved{}, fmt.Errorf("plugins: modrinth %s has no version in %q for %s", project, rng, mcVersion)
		}
	default:
		for i, v := range versions {
			if contains(v.GameVersions, mcVersion) {
				pick = i
				break
			}
		}
		if pick < 0 {
			for i, v := range versions {
				if compatible(v) {
					pick = i
					break
				}
			}
		}
		if pick < 0 {
			return Resolved{}, fmt.Errorf("plugins: modrinth %s has no build for %s", project, mcVersion)
		}
	}

	v := versions[pick]
	if len(v.Files) == 0 {
		return Resolved{}, fmt.Errorf("plugins: modrinth %s %s has no files", project, v.VersionNumber)
	}
	file := v.Files[0]
	for _, f := range v.Files {
		if f.Primary {
			file = f
			break
		}
	}
	return Resolved{
		Provider: "modrinth", Project: project, Version: v.VersionNumber,
		Filename: file.Filename, URL: file.URL, SHA256: file.Hashes.SHA256, Size: file.Size,
	}, nil
}

type hangarResponse struct {
	Result []struct {
		Name      string `json:"name"`
		CreatedAt string `json:"createdAt"`
		Downloads struct {
			Paper *struct {
				FileInfo struct {
					Name       string `json:"name"`
					SizeBytes  int64  `json:"sizeBytes"`
					SHA256Hash string `json:"sha256Hash"`
				} `json:"fileInfo"`
				DownloadURL string `json:"downloadUrl"`
				ExternalURL string `json:"externalUrl"`
			} `json:"PAPER"`
		} `json:"downloads"`
		PlatformDependencies map[string][]string `json:"platformDependencies"`
	} `json:"result"`
}

func (r *Resolver) hangar(ctx context.Context, project, mcVersion, pin, rng string) (Resolved, error) {
	parts := strings.Split(strings.Trim(project, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Resolved{}, fmt.Errorf("plugins: hangar project %q must be owner/slug", project)
	}
	base := strings.TrimRight(r.HangarBase, "/")
	query := url.Values{}
	query.Set("limit", "50")
	query.Set("offset", "0")
	query.Set("platform", "PAPER")
	query.Set("platformVersion", mcVersion)
	var body hangarResponse
	endpoint := fmt.Sprintf("%s/projects/%s/%s/versions?%s",
		base, url.PathEscape(parts[0]), url.PathEscape(parts[1]), query.Encode())
	if err := r.get(ctx, endpoint, &body); err != nil {
		return Resolved{}, err
	}
	if len(body.Result) == 0 {
		query.Del("platformVersion")
		endpoint = fmt.Sprintf("%s/projects/%s/%s/versions?%s",
			base, url.PathEscape(parts[0]), url.PathEscape(parts[1]), query.Encode())
		if err := r.get(ctx, endpoint, &body); err != nil {
			return Resolved{}, err
		}
	}

	pick := -1
	for i, v := range body.Result {
		if v.Downloads.Paper == nil || v.Downloads.Paper.DownloadURL == "" {
			continue
		}
		if pin != "" {
			if v.Name == pin {
				pick = i
				break
			}
			continue
		}
		if rng != "" && !versionHasPrefix(v.Name, rng) {
			continue
		}
		if len(v.PlatformDependencies["PAPER"]) > 0 && !contains(v.PlatformDependencies["PAPER"], mcVersion) {
			continue
		}
		pick = i
		break
	}
	if pick < 0 {
		if pin != "" {
			return Resolved{}, fmt.Errorf("plugins: hangar %s has no version %q", project, pin)
		}
		return Resolved{}, fmt.Errorf("plugins: hangar %s has no paper build for %s", project, mcVersion)
	}
	v := body.Result[pick]
	paper := v.Downloads.Paper
	return Resolved{
		Provider: "hangar", Project: project, Version: v.Name,
		Filename: paper.FileInfo.Name, URL: paper.DownloadURL,
		SHA256: strings.ToLower(paper.FileInfo.SHA256Hash), Size: paper.FileInfo.SizeBytes,
	}, nil
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

func (r *Resolver) github(ctx context.Context, project, pin string) (Resolved, error) {
	parts := strings.Split(strings.Trim(project, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Resolved{}, fmt.Errorf("plugins: github project %q must be owner/repo", project)
	}
	base := strings.TrimRight(r.GitHubBase, "/")
	endpoint := fmt.Sprintf("%s/repos/%s/%s/releases/latest", base, url.PathEscape(parts[0]), url.PathEscape(parts[1]))
	if pin != "" {
		endpoint = fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s", base, url.PathEscape(parts[0]), url.PathEscape(parts[1]), url.PathEscape(pin))
	}
	var rel githubRelease
	if err := r.get(ctx, endpoint, &rel); err != nil {
		return Resolved{}, err
	}
	var asset struct {
		Name string
		URL  string
		Size int64
	}
	found := false
	for _, a := range rel.Assets {
		lower := strings.ToLower(a.Name)
		if !strings.HasSuffix(lower, ".jar") || strings.Contains(lower, "sources") || strings.Contains(lower, "javadoc") || strings.Contains(lower, "dev") {
			continue
		}
		if !found || strings.Contains(lower, "paper") {
			asset.Name, asset.URL, asset.Size = a.Name, a.URL, a.Size
			found = true
			if strings.Contains(lower, "paper") {
				break
			}
		}
	}
	if !found {
		return Resolved{}, fmt.Errorf("plugins: github %s release %s has no jar asset", project, rel.TagName)
	}
	return Resolved{
		Provider: "github", Project: project, Version: rel.TagName,
		Filename: asset.Name, URL: asset.URL, Size: asset.Size,
	}, nil
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// sameMajor reports whether any listed version shares the leading "1.21"
// family with want, allowing a plugin that lags one patch behind.
func sameMajor(list []string, want string) bool {
	prefix := want
	if i := strings.LastIndex(want, "."); i > 0 {
		prefix = want[:i]
	}
	for _, v := range list {
		if v == prefix || strings.HasPrefix(v, prefix+".") {
			return true
		}
	}
	return false
}

var versionTrim = regexp.MustCompile(`^[vV]`)

func versionHasPrefix(version, prefix string) bool {
	v := versionTrim.ReplaceAllString(version, "")
	p := versionTrim.ReplaceAllString(prefix, "")
	return v == p || strings.HasPrefix(v, p+".") || strings.HasPrefix(v, p+"-") || strings.HasPrefix(v, p+"+")
}
