package redact

import "testing"

func TestMask(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			"string and number literals",
			`UPDATE accounts SET email = 'bob@x.io', balance = 42.5 WHERE id = 7`,
			`UPDATE accounts SET email = ?, balance = ? WHERE id = ?`,
		},
		{
			"escaped quote inside string",
			`SELECT * FROM t WHERE name = 'O''Brien'`,
			`SELECT * FROM t WHERE name = ?`,
		},
		{
			"identifiers with digits untouched",
			`SELECT col2 FROM t1 WHERE a2b = 'x'`,
			`SELECT col2 FROM t1 WHERE a2b = ?`,
		},
		{
			"dollar-quoted string",
			`DO $$ secret body $$`,
			`DO ?`,
		},
		{
			"tagged dollar quote",
			`SELECT $tag$ hidden $tag$ FROM x`,
			`SELECT ? FROM x`,
		},
		{
			"positional params kept",
			`SELECT * FROM t WHERE id = $1 AND v = $2`,
			`SELECT * FROM t WHERE id = $1 AND v = $2`,
		},
		{
			"line comment masked",
			"SELECT 1 -- token abc123\nFROM t",
			"SELECT ? --?\nFROM t",
		},
		{
			"block comment masked",
			`SELECT /* secret */ x FROM t`,
			`SELECT /*?*/ x FROM t`,
		},
		{
			"quoted identifier kept",
			`SELECT "weird col" FROM t WHERE x = 'v'`,
			`SELECT "weird col" FROM t WHERE x = ?`,
		},
		{
			"scientific notation",
			`SELECT 1.5e+10, 2E-3`,
			`SELECT ?, ?`,
		},
		{
			"unterminated string masked to end",
			`SELECT 'oops`,
			`SELECT ?`,
		},
		{
			"unterminated dollar quote masked to end",
			`DO $$ never closed`,
			`DO ?`,
		},
		{"empty", ``, ``},
	}
	for _, c := range cases {
		if got := Mask(c.in); got != c.want {
			t.Errorf("%s:\n  in   %q\n  got  %q\n  want %q", c.name, c.in, got, c.want)
		}
	}
}
