package mc

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func fakeAPI(t *testing.T) *httptest.Server {
	t.Helper()
	sha100 := strings.Repeat("a", 64)
	sha101 := strings.Repeat("c", 64)
	sha90 := strings.Repeat("b", 64)
	object := func(sum string) string {
		return "https://fill-data.example/v1/objects/" + sum + "/paper.jar"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/projects/paper", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"project": map[string]string{"id": "paper"},
			"versions": map[string][]string{
				"1.21": {"1.21.3", "1.21.4"},
				"1.22": {"1.22-pre1"},
			},
		})
	})
	mux.HandleFunc("/v3/projects/paper/versions/1.21.4/builds", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": 100, "channel": "STABLE", "downloads": map[string]any{
				"server:default": map[string]any{
					"name": "paper-1.21.4-100.jar", "url": object(sha100),
					"checksums": map[string]string{"sha256": sha100},
				}}},
			{"id": 101, "channel": "BETA", "downloads": map[string]any{
				"server:default": map[string]any{
					"name": "paper-1.21.4-101.jar", "url": object(sha101),
					"checksums": map[string]string{"sha256": sha101},
				}}},
		})
	})
	mux.HandleFunc("/v3/projects/paper/versions/1.21.3/builds", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": 90, "channel": "STABLE", "downloads": map[string]any{
				"server:default": map[string]any{
					"name": "paper-1.21.3-90.jar", "url": object(sha90),
					"checksums": map[string]string{"sha256": sha90},
				}}},
		})
	})
	mux.HandleFunc("/v3/projects/folia", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"project":  map[string]string{"id": "folia"},
			"versions": map[string][]string{"1.21": {"1.21.4"}},
		})
	})
	mux.HandleFunc("/v3/projects/folia/versions/1.21.4/builds", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": 7, "channel": "STABLE", "downloads": map[string]any{
				"server:default": map[string]any{
					"name": "folia-1.21.4-7.jar", "url": object(sha100),
					"checksums": map[string]string{"sha256": sha100},
				}}},
		})
	})
	return httptest.NewServer(mux)
}

func newTestClient(t *testing.T) *Client {
	srv := fakeAPI(t)
	t.Cleanup(srv.Close)
	c := NewClient(srv.Client())
	c.BaseURL = srv.URL + "/v3"
	c.Now = func() time.Time { return time.Unix(1000, 0) }
	return c
}

func TestResolveLatestPicksNewestStableVersionAndBuild(t *testing.T) {
	c := newTestClient(t)
	art, err := c.Resolve(context.Background(), "paper", "latest")
	if err != nil {
		t.Fatal(err)
	}
	if art.Version != "1.21.4" || art.Build != 101 {
		t.Fatalf("artifact = %+v", art)
	}
}

func TestResolveRecommendedPrefersStableChannel(t *testing.T) {
	c := newTestClient(t)
	art, err := c.Resolve(context.Background(), "paper", "recommended")
	if err != nil {
		t.Fatal(err)
	}
	if art.Version != "1.21.4" || art.Build != 100 {
		t.Fatalf("artifact = %+v", art)
	}
	if !strings.HasSuffix(art.JarURL, "/objects/"+strings.Repeat("a", 64)+"/paper.jar") {
		t.Fatalf("url = %s", art.JarURL)
	}
}

func TestResolveExplicitVersion(t *testing.T) {
	c := newTestClient(t)
	art, err := c.Resolve(context.Background(), "paper", "1.21.3")
	if err != nil || art.Version != "1.21.3" || art.Build != 90 {
		t.Fatalf("artifact = %+v, err = %v", art, err)
	}
}

func TestResolveFoliaThroughPaperAPI(t *testing.T) {
	c := newTestClient(t)
	art, err := c.Resolve(context.Background(), "folia", "latest")
	if err != nil {
		t.Fatal(err)
	}
	if art.Project != "folia" || art.Version != "1.21.4" || art.Build != 7 {
		t.Fatalf("artifact = %+v", art)
	}
}

func TestResolveRejectsUnknownVersionAndProject(t *testing.T) {
	c := newTestClient(t)
	if _, err := c.Resolve(context.Background(), "paper", "1.0.0"); !errors.Is(err, ErrResolve) {
		t.Fatalf("err = %v", err)
	}
	if _, err := c.Resolve(context.Background(), "fabric", "latest"); !errors.Is(err, ErrResolve) {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveCaches(t *testing.T) {
	c := newTestClient(t)
	if _, err := c.Resolve(context.Background(), "paper", "latest"); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	_, ok := c.cache["paper|latest"]
	c.mu.Unlock()
	if !ok {
		t.Fatal("result was not cached")
	}
}

func TestResolvePurpurPinsBuildAndComputesSHA256(t *testing.T) {
	jar := []byte("purpur jar bytes")
	sum := fmt.Sprintf("%x", sha256.Sum256(jar))
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/purpur", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"versions": []string{"1.21.3", "1.21.4"}})
	})
	mux.HandleFunc("/v2/purpur/1.21.4", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"builds": map[string]any{
			"latest": "2416", "all": []string{"2415", "2416"},
		}})
	})
	mux.HandleFunc("/v2/purpur/1.21.4/2416", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"result": "SUCCESS"})
	})
	mux.HandleFunc("/v2/purpur/1.21.4/2416/download", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/java-archive")
		_, _ = w.Write(jar)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	c := NewClient(srv.Client())
	c.PurpurBaseURL = srv.URL + "/v2"
	c.Now = func() time.Time { return time.Unix(1000, 0) }
	art, err := c.Resolve(context.Background(), "purpur", "latest")
	if err != nil {
		t.Fatal(err)
	}
	if art.Project != "purpur" || art.Version != "1.21.4" || art.Build != 2416 || art.JarSHA256 != sum {
		t.Fatalf("artifact = %+v, want sha %s", art, sum)
	}
	if !strings.HasSuffix(art.JarURL, "/1.21.4/2416/download") {
		t.Fatalf("url = %s", art.JarURL)
	}
}
