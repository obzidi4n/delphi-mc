package template

import (
	"strings"
	"testing"
)

func TestBuiltinsValidate(t *testing.T) {
	all, err := Builtins()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Fatalf("builtins = %d, want 5", len(all))
	}
	bases := map[string]bool{}
	for _, tpl := range all {
		if tpl.ID == "" || tpl.Name == "" {
			t.Fatalf("builtin without id/name: %+v", tpl)
		}
		bases[tpl.Base] = true
	}
	if !bases["paper"] || !bases["velocity"] {
		t.Fatalf("bases = %v", bases)
	}
}

func TestRenderSurvivalSubstitutesServerFacts(t *testing.T) {
	all, err := Builtins()
	if err != nil {
		t.Fatal(err)
	}
	var survival Template
	for _, tpl := range all {
		if tpl.ID == "builtin-survival" {
			survival = tpl
		}
	}
	inst, err := survival.Render(Input{
		Name: "SMP One", Port: 25570, MemoryMB: 4096, CPUCores: 2,
		Java: 21, MCVersion: "1.21.4",
	})
	if err != nil {
		t.Fatal(err)
	}
	props := inst.Files["server.properties"]
	for _, want := range []string{"server-port=25570", "motd=SMP One", "gamemode=survival"} {
		if !strings.Contains(props, want) {
			t.Fatalf("server.properties missing %q:\n%s", want, props)
		}
	}
	if inst.Variables["template.mc_version"] != "1.21.4" {
		t.Fatalf("variables = %v", inst.Variables)
	}
}

func TestRenderRejectsUnknownReferenceAndOverride(t *testing.T) {
	tpl := Template{
		SchemaVersion: 1, Name: "x", Base: "paper", MCVersion: "latest", Java: 21,
		Files: map[string]string{"a.txt": "{{nope}}"},
	}
	if err := tpl.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := tpl.Render(Input{Name: "n", Port: 25565}); err == nil {
		t.Fatal("unknown reference was accepted")
	}
	tpl.Files = map[string]string{"a.txt": "{{server.name}}"}
	if _, err := tpl.Render(Input{Name: "n", Port: 25565, Overrides: map[string]string{"nope": "1"}}); err == nil {
		t.Fatal("unknown override was accepted")
	}
}

func TestRenderRequiredVariable(t *testing.T) {
	tpl := Template{
		SchemaVersion: 1, Name: "x", Base: "paper", MCVersion: "latest", Java: 21,
		Files:     map[string]string{"a.txt": "{{secret}}"},
		Variables: map[string]Variable{"secret": {Required: true}},
	}
	if _, err := tpl.Render(Input{Name: "n", Port: 25565}); err == nil {
		t.Fatal("missing required variable was accepted")
	}
	inst, err := tpl.Render(Input{Name: "n", Port: 25565, Overrides: map[string]string{"secret": "s3"}})
	if err != nil || inst.Files["a.txt"] != "s3" {
		t.Fatalf("files = %v, err = %v", inst.Files, err)
	}
}

func TestValidateRejectsUnsafeTemplates(t *testing.T) {
	base := Template{SchemaVersion: 1, Name: "x", Base: "paper", MCVersion: "latest", Java: 21}
	cases := map[string]func(*Template){
		"purpur":    func(t *Template) { t.Base = "purpur" },
		"traversal": func(t *Template) { t.Files = map[string]string{"../x": "y"} },
		"absolute":  func(t *Template) { t.Files = map[string]string{"/etc/passwd": "y"} },
		"eula":      func(t *Template) { t.Files = map[string]string{"eula.txt": "eula=true"} },
		"java":      func(t *Template) { t.Java = 11 },
		"version":   func(t *Template) { t.MCVersion = "newest" },
		"reserved":  func(t *Template) { t.Variables = map[string]Variable{"server.port": {}} },
		"plugin":    func(t *Template) { t.Plugins = []Plugin{{Project: "x", Provider: "ftb"}} },
	}
	for name, mutate := range cases {
		tpl := base
		mutate(&tpl)
		if err := tpl.Validate(); err == nil {
			t.Fatalf("%s: invalid template accepted", name)
		}
	}
}
