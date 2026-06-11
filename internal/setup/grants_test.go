package setup

import (
	"strings"
	"testing"
)

func TestScriptIncludesGrants(t *testing.T) {
	s := Script(GrantOptions{Role: "pglockr_ro", Password: "pw", Signal: true})
	for _, want := range []string{
		`CREATE ROLE "pglockr_ro" LOGIN PASSWORD 'pw';`,
		`GRANT pg_monitor TO "pglockr_ro";`,
		`GRANT pg_signal_backend TO "pglockr_ro";`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script missing %q\n---\n%s", want, s)
		}
	}
}

func TestScriptNoSignal(t *testing.T) {
	s := Script(GrantOptions{Role: "viewer", Password: "pw", Signal: false})
	if strings.Contains(s, "GRANT pg_signal_backend") {
		t.Errorf("read-only script must not grant pg_signal_backend:\n%s", s)
	}
	if !strings.Contains(s, "GRANT pg_monitor TO \"viewer\";") {
		t.Errorf("script should still grant pg_monitor:\n%s", s)
	}
}

func TestScriptEscapesInjection(t *testing.T) {
	// A role/password with quotes must be escaped, not break out of the literal.
	s := Script(GrantOptions{Role: `ro"; DROP TABLE x;--`, Password: `p'w`, Signal: true})
	if !strings.Contains(s, `"ro""; DROP TABLE x;--"`) {
		t.Errorf("identifier not escaped:\n%s", s)
	}
	if !strings.Contains(s, `'p''w'`) {
		t.Errorf("literal not escaped:\n%s", s)
	}
}

func TestRemediation(t *testing.T) {
	if got := Remediation("r", true, false); got != `GRANT pg_monitor TO "r";` {
		t.Errorf("stats-only remediation = %q", got)
	}
	if got := Remediation("r", false, true); got != `GRANT pg_signal_backend TO "r";` {
		t.Errorf("signal-only remediation = %q", got)
	}
	both := Remediation("r", true, true)
	if !strings.Contains(both, "pg_monitor") || !strings.Contains(both, "pg_signal_backend") {
		t.Errorf("both remediation incomplete: %q", both)
	}
	if got := Remediation("r", false, false); got != "" {
		t.Errorf("no-missing remediation should be empty, got %q", got)
	}
}

func TestRandomPassword(t *testing.T) {
	a, err := RandomPassword()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := RandomPassword()
	if a == "" || a == b {
		t.Fatalf("passwords should be non-empty and unique: %q %q", a, b)
	}
	if strings.ContainsAny(a, "'\"\\") {
		t.Errorf("password contains chars needing SQL escaping: %q", a)
	}
	if len(a) < 16 {
		t.Errorf("password too short: %q", a)
	}
}
