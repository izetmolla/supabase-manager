package proxymanager

import (
	"strings"
	"testing"
)

// Expected values come from `openssl passwd -apr1 -salt <salt> <password>`.
func TestApr1WithSalt(t *testing.T) {
	cases := []struct{ password, salt, want string }{
		{"password", "abcdefgh", "$apr1$abcdefgh$FBwExRW4dCc8aL.OvjpIE1"},
		{"a much longer secret password!", "Zx9./AbC", "$apr1$Zx9./AbC$YHYlRvD.JoyFm0jdKHE/K/"},
		{"x", "s", "$apr1$s$bHQMM1UunWFj8XWEuTGlW."},
	}
	for _, c := range cases {
		if got := apr1WithSalt(c.password, c.salt); got != c.want {
			t.Errorf("apr1WithSalt(%q, %q) = %q, want %q", c.password, c.salt, got, c.want)
		}
	}
}

func TestApr1HashRandomSalt(t *testing.T) {
	a, b := apr1Hash("secret"), apr1Hash("secret")
	if a == b {
		t.Fatal("two hashes of the same password share a salt")
	}
	parts := strings.Split(a, "$")
	if len(parts) != 4 || parts[1] != "apr1" || len(parts[2]) != 8 {
		t.Fatalf("unexpected hash format %q", a)
	}
	if apr1WithSalt("secret", parts[2]) != a {
		t.Fatal("hash does not verify with its own salt")
	}
}
