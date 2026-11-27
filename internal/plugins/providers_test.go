package plugins

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModrinthPicksLatestCompatibleAndPin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/project/luckperms/version") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		fmt.Fprint(w, `[
			{"id":"v2","version_number":"v5.6.0","game_versions":["1.21.11"],"loaders":["paper"],
			 "files":[{"filename":"LuckPerms-5.6.0.jar","url":"https://cdn/x.jar","size":10,"primary":true}]},
			{"id":"v1","version_number":"v5.5.71","game_versions":["1.21.8"],"loaders":["paper"],
			 "files":[{"filename":"LuckPerms-5.5.71.jar","url":"https://cdn/y.jar","size":9,"primary":true}]}
		]`)
	}))
	defer srv.Close()
	r := NewResolver(srv.Client())
	r.ModrinthBase = srv.URL

	got, err := r.Resolve(context.Background(), "modrinth", "luckperms", "1.21.11", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "v5.6.0" || got.Filename != "LuckPerms-5.6.0.jar" {
		t.Fatalf("latest = %+v", got)
	}

	got, err = r.Resolve(context.Background(), "modrinth", "luckperms", "1.21.11", "v5.5.71", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "v5.5.71" {
		t.Fatalf("pin = %+v", got)
	}

	got, err = r.Resolve(context.Background(), "modrinth", "luckperms", "1.21.11", "", "5.5")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "v5.5.71" {
		t.Fatalf("range = %+v", got)
	}

	if _, err := r.Resolve(context.Background(), "modrinth", "luckperms", "1.21.11", "v9", ""); err == nil {
		t.Fatal("missing pin should fail")
	}
}

func TestHangarResolvesPaperDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/ViaVersion/ViaVersion/versions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"result":[{"name":"5.12.4","createdAt":"2026-11-26T00:00:00Z",
		 "platformDependencies":{"PAPER":["1.21.11"]},
		 "downloads":{"PAPER":{"fileInfo":{"name":"ViaVersion-5.12.4.jar","sizeBytes":7,"sha256Hash":"ABCDEF"},
		 "downloadUrl":"https://hangar/x.jar"}}}]}`)
	}))
	defer srv.Close()
	r := NewResolver(srv.Client())
	r.HangarBase = srv.URL

	got, err := r.Resolve(context.Background(), "hangar", "ViaVersion/ViaVersion", "1.21.11", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "5.12.4" || got.URL != "https://hangar/x.jar" || got.SHA256 != "abcdef" {
		t.Fatalf("hangar = %+v", got)
	}
	if _, err := r.Resolve(context.Background(), "hangar", "ViaVersion", "1.21.11", "", ""); err == nil {
		t.Fatal("owner/slug must be required")
	}
}

func TestGitHubPrefersPaperAsset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"5.12.3","assets":[
		 {"name":"ViaVersion-5.12.3-sources.jar","browser_download_url":"https://gh/sources.jar","size":1},
		 {"name":"ViaVersion-5.12.3.jar","browser_download_url":"https://gh/plain.jar","size":2},
		 {"name":"ViaVersion-5.12.3-paper.jar","browser_download_url":"https://gh/paper.jar","size":3}
		]}`)
	}))
	defer srv.Close()
	r := NewResolver(srv.Client())
	r.GitHubBase = srv.URL

	got, err := r.Resolve(context.Background(), "github", "ViaVersion/ViaVersion", "1.21.11", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "https://gh/paper.jar" || got.Version != "5.12.3" {
		t.Fatalf("github = %+v", got)
	}
}

func TestSlugIsStableAndSafe(t *testing.T) {
	cases := map[string]string{
		"LuckPerms":             "luckperms.jar",
		"ViaVersion/ViaVersion": "viaversion-viaversion.jar",
		"../evil":               "evil.jar",
		"":                      "plugin.jar",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCatalogSearchesModrinthAndHangar(t *testing.T) {
	modrinth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Errorf("unexpected modrinth path %s", r.URL.Path)
		}
		if !strings.Contains(r.URL.Query().Get("facets"), "1.21.11") {
			t.Errorf("facets missing game version: %s", r.URL.Query().Get("facets"))
		}
		fmt.Fprint(w, `{"hits":[{"slug":"luckperms","title":"LuckPerms","description":"Permissions",
		 "author":"lucko","icon_url":"https://cdn/icon.png","downloads":10,"versions":["5.5"]}]}`)
	}))
	defer modrinth.Close()
	hangar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects" {
			t.Errorf("unexpected hangar path %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"result":[{"name":"ViaVersion","namespace":{"owner":"ViaVersion","slug":"ViaVersion"},
		 "description":"Compatibility","avatarUrl":"https://cdn/via.png","stats":{"downloads":20}}]}`)
	}))
	defer hangar.Close()
	r := NewResolver(nil)
	r.ModrinthBase = modrinth.URL
	r.HangarBase = hangar.URL

	got, err := r.Search(context.Background(), "modrinth", "luck", "1.21.11", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Project != "luckperms" || got[0].Downloads != 10 {
		t.Fatalf("modrinth = %+v", got)
	}
	got, err = r.Search(context.Background(), "hangar", "via", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Project != "ViaVersion/ViaVersion" || got[0].PageURL != "https://hangar.papermc.io/ViaVersion/ViaVersion" {
		t.Fatalf("hangar = %+v", got)
	}
	if _, err := r.Search(context.Background(), "github", "x", "", 10); err == nil {
		t.Fatal("github catalog should be rejected")
	}
}
