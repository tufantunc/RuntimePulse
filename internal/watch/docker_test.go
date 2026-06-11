package watch

import "testing"

func TestMapDockerEvent(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
	}{
		{"start", `{"Type":"container","Action":"start","Actor":{"Attributes":{"name":"postgres"}}}`, "docker.started"},
		{"die", `{"Type":"container","Action":"die","Actor":{"Attributes":{"name":"postgres"}}}`, "docker.stopped"},
		{"stop", `{"Type":"container","Action":"stop","Actor":{"Attributes":{"name":"postgres"}}}`, "docker.stopped"},
		{"healthy", `{"Type":"container","Action":"health_status: healthy","Actor":{"Attributes":{"name":"postgres"}}}`, "docker.healthy"},
		{"unhealthy", `{"Type":"container","Action":"health_status: unhealthy","Actor":{"Attributes":{"name":"postgres"}}}`, "docker.unhealthy"},
		{"ignored exec", `{"Type":"container","Action":"exec_create: ls","Actor":{"Attributes":{"name":"postgres"}}}`, ""},
		{"ignored network", `{"Type":"network","Action":"connect","Actor":{"Attributes":{"name":"bridge"}}}`, ""},
		{"garbage", `not json`, ""},
	}
	for _, c := range cases {
		evType, payload := MapDockerEvent([]byte(c.line))
		if evType != c.want {
			t.Errorf("%s: got %q want %q", c.name, evType, c.want)
		}
		if c.want != "" && payload["container"] != "postgres" {
			t.Errorf("%s: payload missing container: %v", c.name, payload)
		}
	}
}

func TestMapDockerInspect(t *testing.T) {
	cases := []struct{ out, want string }{
		{"running healthy", "docker.healthy"},
		{"running unhealthy", "docker.unhealthy"},
		{"running ", "docker.started"},
		{"running", "docker.started"},
		{"exited ", "docker.stopped"},
		{"created ", "docker.stopped"},
		{"", ""},
	}
	for _, c := range cases {
		if got := MapDockerInspect(c.out); got != c.want {
			t.Errorf("%q: got %q want %q", c.out, got, c.want)
		}
	}
}
