package selfupdate

import "testing"

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"1.2.3", "1.2.10", -1},
		{"2.0.0", "1.9.9", 1},
		{"v1.0.0", "1.0.0", 0},
		{"0.10.0", "0.9.1", 1},
	}
	for _, c := range cases {
		if got := compare(c.a, c.b); got != c.want {
			t.Errorf("compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestReleaseTags(t *testing.T) {
	for tag, want := range map[string]bool{
		"1.2.3": true, "v1.2.3": true, "latest": false, "1.2": false, "1.2.3-rc1": false, "1.2.3-dirty": false,
	} {
		if got := releaseRe.MatchString(tag); got != want {
			t.Errorf("release %q = %v, want %v", tag, got, want)
		}
	}
}

func TestContainerIDFromMountinfo(t *testing.T) {
	id := "3f2a9c0d1e4b5a6f7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b"
	line := "612 590 259:2 /var/lib/docker/containers/" + id + "/hostname /etc/hostname rw,relatime - ext4 /dev/root rw"
	m := containerIDRe.FindStringSubmatch(line)
	if m == nil || m[1] != id {
		t.Fatalf("got %v, want %s", m, id)
	}
}
