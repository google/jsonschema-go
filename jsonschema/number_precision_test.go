// Copyright 2026 The JSON Schema Go Project Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package jsonschema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
)

func TestSchemaConstraintNumberPrecision(t *testing.T) {
	for _, pair := range [][2]string{
		{"9007199254740993", "9007199254740992"},
		{"-9007199254740993", "-9007199254740992"},
		{"18446744073709551615", "18446744073709551614"},
		{"0.12345678901234567890123456789", "0.12345678901234567890123456788"},
		{"1e400", "2e400"},
	} {
		for _, shape := range []string{"%s", `{"id":[%s]}`} {
			for _, keyword := range []string{"const", "enum"} {
				t.Run(keyword+"/"+shape+"/"+pair[0], func(t *testing.T) {
					want := fmt.Sprintf(shape, pair[0])
					wrong := fmt.Sprintf(shape, pair[1])
					constraint := want
					if keyword == "enum" {
						constraint = "[" + constraint + "]"
					}
					data := []byte(fmt.Sprintf(`{"%s":%s}`, keyword, constraint))
					var s Schema
					if err := json.Unmarshal(data, &s); err != nil {
						t.Fatal(err)
					}
					encoded, err := json.Marshal(&s)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(encoded, data) {
						t.Errorf("schema = %s, want %s", encoded, data)
					}
					r, err := s.Resolve(nil)
					if err != nil {
						t.Fatal(err)
					}
					for _, tc := range []struct {
						data  string
						valid bool
					}{{want, true}, {wrong, false}} {
						var instance any
						d := json.NewDecoder(bytes.NewBufferString(tc.data))
						d.UseNumber()
						if err := d.Decode(&instance); err != nil {
							t.Fatal(err)
						}
						if err := r.Validate(instance); (err == nil) != tc.valid {
							t.Errorf("Validate(%s) = %v; want valid=%v", tc.data, err, tc.valid)
						}
					}
				})
			}
		}
	}
}

func TestSchemaJSONNumberValues(t *testing.T) {
	const data = `{"const":null,"enum":[null,"text",true,9007199254740993],"examples":[{"id":9007199254740993}],"x-values":[9007199254740993],"minimum":1.5}`
	var s Schema
	if err := json.Unmarshal([]byte(data), &s); err != nil {
		t.Fatal(err)
	}
	if s.Const == nil || *s.Const != nil {
		t.Fatalf("const = %v, want explicit null", s.Const)
	}
	if s.Enum[0] != nil || s.Enum[1] != "text" || s.Enum[2] != true {
		t.Errorf("non-numeric enum values changed: %v", s.Enum)
	}
	for _, got := range []any{s.Enum[3], s.Examples[0].(map[string]any)["id"], s.Extra["x-values"].([]any)[0]} {
		if got != json.Number("9007199254740993") {
			t.Errorf("got %v (%T), want exact json.Number", got, got)
		}
	}
	if s.Minimum == nil || *s.Minimum != 1.5 {
		t.Errorf("minimum = %v, want float64(1.5)", s.Minimum)
	}
	for _, invalid := range []string{`{} {}`, `{} trailing`, `{"enum":[01]}`} {
		if err := s.UnmarshalJSON([]byte(invalid)); err == nil {
			t.Errorf("UnmarshalJSON(%q) succeeded", invalid)
		}
	}
}
