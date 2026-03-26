package config

import "testing"

func TestNewMap_DefaultAuthRoleHierarchy(t *testing.T) {
	cfg := NewMap(nil)

	cases := map[string]string{
		KeyAuthDefaultRole:                "node",
		KeyAuthDefaultPerms:               "",
		KeyAuthRolePerms:                  DefaultAuthRolePerms,
		KeyAuthBootstrapFirstRegisterRole: DefaultAuthBootstrapFirstRegisterRole,
	}

	for key, want := range cases {
		got, ok := cfg.Get(key)
		if !ok {
			t.Fatalf("missing default key %q", key)
		}
		if got != want {
			t.Fatalf("unexpected default for %q: got %q want %q", key, got, want)
		}
	}
}
