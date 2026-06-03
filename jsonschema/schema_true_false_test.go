package jsonschema

import (
    "encoding/json"
    "testing"
)

func TestTrueFalseConstructors_Marshal(t *testing.T) {
    bt, err := json.Marshal(True())
    if err != nil {
        t.Fatalf("Marshal(True()) error: %v", err)
    }
    if string(bt) != "true" {
        t.Fatalf("Marshal(True()) = %s, want true", string(bt))
    }

    bf, err := json.Marshal(False())
    if err != nil {
        t.Fatalf("Marshal(False()) error: %v", err)
    }
    if string(bf) != "false" {
        t.Fatalf("Marshal(False()) = %s, want false", string(bf))
    }
}

func TestTrueFalseConstructors_Validate(t *testing.T) {
    rsTrue, err := True().Resolve(nil)
    if err != nil {
        t.Fatalf("True().Resolve() error: %v", err)
    }
    // Should validate any instance
    cases := []any{nil, true, false, 0, 3.14, "x", map[string]any{"a": 1}, []any{1, 2}}
    for i, c := range cases {
        if err := rsTrue.Validate(c); err != nil {
            t.Fatalf("True().Validate case %d failed: %v", i, err)
        }
    }

    rsFalse, err := False().Resolve(nil)
    if err != nil {
        t.Fatalf("False().Resolve() error: %v", err)
    }
    // Should reject any instance
    for i, c := range cases {
        if err := rsFalse.Validate(c); err == nil {
            t.Fatalf("False().Validate case %d succeeded unexpectedly", i)
        }
    }
}

