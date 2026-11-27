package serverprops

import (
	"strings"
	"testing"
)

func TestParseEscapesAndContinuation(t *testing.T) {
	entries := Parse("# comment\nmotd=hello\\: world\nlevel-type=minecraft\\:normal\npath=a\\\n  b\n")
	if len(entries) != 3 {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].Value != "hello: world" || entries[1].Value != "minecraft:normal" {
		t.Fatalf("escapes = %+v", entries)
	}
	if entries[2].Value != "ab" {
		t.Fatalf("continuation = %+v", entries[2])
	}
}

func TestValidateAcceptsCurrentKeysAndWarnsOnRemoved(t *testing.T) {
	body := strings.Join([]string{
		"difficulty=normal",
		"gamemode=survival",
		"view-distance=10",
		"management-server-allowed-origins=",
		"pvp=true",
		"enable-command-block=false",
		"level-type=minecraft\\:normal",
	}, "\n")
	res := Validate(body, "1.21.11")
	if !res.Valid {
		t.Fatalf("valid config rejected: %v", res.Errors)
	}
	if len(res.Warnings) != 2 {
		t.Fatalf("warnings = %v, want 2 removed-key warnings", res.Warnings)
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	cases := map[string]string{
		"boolean": "online-mode=maybe",
		"port":    "server-port=70000",
		"range":   "view-distance=64",
		"enum":    "difficulty=nightmare",
		"uuid":    "resource-pack-id=not-a-uuid",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			res := Validate(body, "1.21.11")
			if res.Valid || len(res.Errors) == 0 {
				t.Fatalf("invalid value accepted: %+v", res)
			}
		})
	}
}

func TestValidateVersionGatesKeys(t *testing.T) {
	res := Validate("management-server-enabled=false", "1.21.8")
	if !res.Valid || len(res.Warnings) == 0 {
		t.Fatalf("result = %+v, want a since warning", res)
	}
	for _, k := range Reference("1.21.8") {
		if k.Name == "management-server-enabled" {
			t.Fatal("new key appeared in an older reference")
		}
	}
	for _, k := range Reference("1.21.11") {
		if k.Name == "management-server-enabled" {
			return
		}
	}
	t.Fatal("new key missing from its reference version")
}

func TestValidateCrossFieldManagementTLS(t *testing.T) {
	res := Validate("management-server-enabled=true\nmanagement-server-tls-enabled=true\n", "1.21.11")
	if res.Valid || len(res.Errors) == 0 {
		t.Fatalf("missing management TLS keystore accepted: %+v", res)
	}
}

func TestValidateCrossFieldRcon(t *testing.T) {
	res := Validate("enable-rcon=true\nrcon.password=\n", "1.21.11")
	if res.Valid || len(res.Errors) == 0 {
		t.Fatalf("missing rcon password accepted: %+v", res)
	}
}
