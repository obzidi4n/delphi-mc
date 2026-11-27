package auth

import "testing"

func TestPasswordRoundTrip(t *testing.T) {
	encoded, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(encoded, "correct horse battery staple") {
		t.Fatal("correct password rejected")
	}
	if VerifyPassword(encoded, "wrong password") {
		t.Fatal("wrong password accepted")
	}
	if VerifyPassword("not-a-hash", "whatever") {
		t.Fatal("garbage hash accepted")
	}
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("short password accepted")
	}
}

func TestAllows(t *testing.T) {
	for _, tc := range []struct {
		scopes []string
		want   string
		ok     bool
	}{
		{[]string{"*"}, "server:s_1:power", true},
		{[]string{"server:*:power"}, "server:s_1:power", true},
		{[]string{"server:*"}, "server:s_1:power", true},
		{[]string{"server:s_1:power"}, "server:s_2:power", false},
		{[]string{"org:*:read"}, "server:s_1:power", false},
		{nil, "server:s_1:power", false},
	} {
		if got := Allows(tc.scopes, tc.want); got != tc.ok {
			t.Errorf("Allows(%v, %q) = %v, want %v", tc.scopes, tc.want, got, tc.ok)
		}
	}
}

func TestScopesForRole(t *testing.T) {
	if !Allows(ScopesForRole("owner", "default"), "org:default:admin") {
		t.Fatal("owner lacks org admin")
	}
	if Allows(ScopesForRole("viewer", "default"), "server:s_1:power") {
		t.Fatal("viewer can power servers")
	}
	if !Allows(ScopesForRole("viewer", "default"), "server:s_1:read") {
		t.Fatal("viewer cannot read servers")
	}
	if !Allows(ScopesForRole("developer", "default"), "server:s_9:power") {
		t.Fatal("developer cannot power servers")
	}
	if Allows(ScopesForRole("developer", "default"), "org:default:admin") {
		t.Fatal("developer can administer the org")
	}
	if !Allows(ScopesForRole("admin", "default"), "org:default:user:admin") {
		t.Fatal("admin cannot manage users")
	}
	if !Allows(ScopesForServer("viewer", "s_9"), "server:s_9:read") {
		t.Fatal("server viewer cannot read its server")
	}
	if Allows(ScopesForServer("viewer", "s_9"), "server:s_8:read") {
		t.Fatal("server binding leaks to another server")
	}
}
