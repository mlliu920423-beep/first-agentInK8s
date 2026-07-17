package mcp

import (
	"testing"
)

func TestParseCondition(t *testing.T) {
	cases := []struct {
		name    string
		expr    string
		wantErr bool
		check   func(t *testing.T, c condition)
	}{
		{
			name: "empty is always",
			expr: "",
			check: func(t *testing.T, c condition) {
				if !c.always {
					t.Fatalf("want always=true, got %+v", c)
				}
				if c.raw != "always" {
					t.Fatalf("empty expr should normalise raw to \"always\", got %q", c.raw)
				}
			},
		},
		{
			name: "always keyword",
			expr: "always",
			check: func(t *testing.T, c condition) {
				if !c.always {
					t.Fatalf("want always=true, got %+v", c)
				}
			},
		},
		{
			name: "env:X non-empty check",
			expr: "env:X",
			check: func(t *testing.T, c condition) {
				if c.always {
					t.Fatalf("should not be always")
				}
				if c.envVar != "X" || c.envEq {
					t.Fatalf("want envVar=X envEq=false, got %+v", c)
				}
			},
		},
		{
			name: "env:X=y equality check",
			expr: "env:X=y",
			check: func(t *testing.T, c condition) {
				if c.envVar != "X" || !c.envEq || c.envWant != "y" {
					t.Fatalf("want envVar=X envEq=true envWant=y, got %+v", c)
				}
			},
		},
		{
			name: "env:X= empty string equality",
			expr: "env:X=",
			check: func(t *testing.T, c condition) {
				if c.envVar != "X" || !c.envEq || c.envWant != "" {
					t.Fatalf("want envVar=X envEq=true envWant=\"\", got %+v", c)
				}
			},
		},
		{name: "empty variable after env:", expr: "env:", wantErr: true},
		{name: "empty variable before =", expr: "env:=x", wantErr: true},
		{name: "multiple = rejected", expr: "env:X=y=z", wantErr: true},
		{name: "unknown keyword", expr: "unknown", wantErr: true},
		{name: "typo missing colon", expr: "envX", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseCondition(tc.expr)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got nil (parsed=%+v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}

func TestConditionEval(t *testing.T) {
	t.Run("always is always true", func(t *testing.T) {
		c, err := parseCondition("always")
		if err != nil {
			t.Fatal(err)
		}
		if !c.eval() {
			t.Fatal("want true")
		}
	})

	t.Run("env:X unset is false", func(t *testing.T) {
		// Deliberately pick a name unlikely to be exported by any CI runner.
		// t.Setenv cannot unset, so we rely on the name being unset by nature.
		c, err := parseCondition("env:COND_TEST_UNSET_XYZ")
		if err != nil {
			t.Fatal(err)
		}
		if c.eval() {
			t.Fatal("unset var must eval false")
		}
	})

	t.Run("env:X set is true", func(t *testing.T) {
		t.Setenv("COND_TEST_X", "anything")
		c, err := parseCondition("env:COND_TEST_X")
		if err != nil {
			t.Fatal(err)
		}
		if !c.eval() {
			t.Fatal("set var must eval true")
		}
	})

	t.Run("env:X=1 unset is false", func(t *testing.T) {
		c, err := parseCondition("env:COND_TEST_UNSET_ABC=1")
		if err != nil {
			t.Fatal(err)
		}
		if c.eval() {
			t.Fatal("unset must not equal 1")
		}
	})

	t.Run("env:X=1 matching is true", func(t *testing.T) {
		t.Setenv("COND_TEST_EQ", "1")
		c, err := parseCondition("env:COND_TEST_EQ=1")
		if err != nil {
			t.Fatal(err)
		}
		if !c.eval() {
			t.Fatal("matching value must eval true")
		}
	})

	t.Run("env:X=1 mismatching is false", func(t *testing.T) {
		t.Setenv("COND_TEST_EQ", "2")
		c, err := parseCondition("env:COND_TEST_EQ=1")
		if err != nil {
			t.Fatal(err)
		}
		if c.eval() {
			t.Fatal("mismatch must eval false")
		}
	})
}

func TestExpandVars(t *testing.T) {
	// Ensure the test-vars are actually unset when a case expects that.
	// t.Setenv can only set (not unset) — for "unset" cases we pick names
	// unlikely to appear in any real environment.
	t.Run("no substitution", func(t *testing.T) {
		if got := expandVars("no vars"); got != "no vars" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("unset without default is empty", func(t *testing.T) {
		if got := expandVars("${env:EXP_UNSET_FOO}"); got != "" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("unset with default", func(t *testing.T) {
		if got := expandVars("${env:EXP_UNSET_FOO:-fallback}"); got != "fallback" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("set without default", func(t *testing.T) {
		t.Setenv("EXP_SET_FOO", "bar")
		if got := expandVars("${env:EXP_SET_FOO}"); got != "bar" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("set with default prefers value", func(t *testing.T) {
		t.Setenv("EXP_SET_FOO2", "bar")
		if got := expandVars("${env:EXP_SET_FOO2:-x}"); got != "bar" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("compound interpolation", func(t *testing.T) {
		got := expandVars("a${env:EXP_X:-1}b${env:EXP_Y:-2}c")
		if got != "a1b2c" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("empty env name soft-fails", func(t *testing.T) {
		if got := expandVars("${env:}"); got != "${env:}" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("unknown prefix soft-fails", func(t *testing.T) {
		if got := expandVars("${bad-prefix:X}"); got != "${bad-prefix:X}" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("unterminated brace soft-fails", func(t *testing.T) {
		if got := expandVars("${env:X"); got != "${env:X" {
			t.Fatalf("got %q", got)
		}
	})
}
