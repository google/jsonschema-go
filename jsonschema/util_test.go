// Copyright 2025 The JSON Schema Go Project Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package jsonschema

import (
	"encoding/json"
	"hash/maphash"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestEqual(t *testing.T) {
	for _, tt := range []struct {
		x1, x2 any
		want   bool
	}{
		{0, 1, false},
		{1, 1.0, true},
		{nil, 0, false},
		{"0", 0, false},
		{2.5, 2.5, true},
		{[]int{1, 2}, []float64{1.0, 2.0}, true},
		{[]int(nil), []int{}, false},
		{[]map[string]any(nil), []map[string]any{}, false},
		{
			map[string]any{"a": 1, "b": 2.0},
			map[string]any{"a": 1.0, "b": 2},
			true,
		},
		{json.Number("42"), 42, true},
		{json.Number("42"), 42.0, true},
		{json.Number("42"), "42", false},
		{"42", json.Number("42"), false},
		{json.Number("42"), json.Number("42"), true},
		{json.Number("42"), json.Number("42.0"), true},
		{json.Number("42"), json.Number("43"), false},
		{map[string]any{"k": json.Number("1")}, map[string]any{"k": 1.0}, true},
		{map[string]any{"k": json.Number("1")}, map[string]any{"k": "1"}, false},
		{[]any{json.Number("1")}, []any{1}, true},
		{[]any{json.Number("1")}, []any{"1"}, false},
	} {
		check := func(x1, x2 any, want bool) {
			t.Helper()
			if got := Equal(x1, x2); got != want {
				t.Errorf("jsonEqual(%#v, %#v) = %t, want %t", x1, x2, got, want)
			}
		}
		check(tt.x1, tt.x1, true)
		check(tt.x2, tt.x2, true)
		check(tt.x1, tt.x2, tt.want)
		check(tt.x2, tt.x1, tt.want)
	}
}

func TestJSONType(t *testing.T) {
	stdCases := []struct {
		val  string
		want string
	}{
		{`null`, "null"},
		{`0`, "integer"},
		{`0.0`, "integer"},
		{`1e2`, "integer"},
		{`0.1`, "number"},
		{`""`, "string"},
		{`true`, "boolean"},
		{`[]`, "array"},
		{`{}`, "object"},
	}

	t.Run("normal unmarshal", func(t *testing.T) {
		for _, tt := range stdCases {
			var val any
			if err := json.Unmarshal([]byte(tt.val), &val); err != nil {
				t.Fatal(err)
			}
			got, ok := jsonType(reflect.ValueOf(val))
			if !ok {
				t.Fatalf("jsonType failed on %q", tt.val)
			}
			if got != tt.want {
				t.Errorf("%s: got %q, want %q", tt.val, got, tt.want)
			}
		}
	})

	t.Run("with UseNumber", func(t *testing.T) {
		for _, tt := range stdCases {
			var val any
			decoder := json.NewDecoder(strings.NewReader(tt.val))
			decoder.UseNumber()
			if err := decoder.Decode(&val); err != nil {
				t.Fatal(err)
			}
			got, ok := jsonType(reflect.ValueOf(val))
			if !ok {
				t.Fatalf("jsonType failed on %q", tt.val)
			}
			if got != tt.want {
				t.Errorf("%s: got %q, want %q", tt.val, got, tt.want)
			}
		}
	})

	t.Run("exact precision", func(t *testing.T) {
		for _, tt := range []struct {
			val  json.Number
			want string
		}{
			{"-42", "integer"},
			{"-42.5", "number"},
			{"9007199254740993", "integer"},
			{"1.0000000000000000001", "number"},
			{"1e400", "integer"},
		} {
			got, ok := jsonType(reflect.ValueOf(tt.val))
			if !ok {
				t.Errorf("%s: got ok false, want true", tt.val)
			}
			if got != tt.want {
				t.Errorf("%s: got %q, want %q", tt.val, got, tt.want)
			}
		}
	})

	t.Run("fallback cases", func(t *testing.T) {
		for _, tt := range []struct {
			val  json.Number
			want string
		}{
			{"", "string"},
			{"1e9999999", "string"},
			{"0x10", "string"},
			{"1/2", "string"},
			{"+5", "string"},
			{".5", "string"},
			{"1_000", "string"},
			{"0123", "string"},
			{"0b101", "string"},
			{"bad", "string"},
		} {
			got, ok := jsonType(reflect.ValueOf(tt.val))
			if !ok {
				t.Errorf("%s: got ok false, want true", tt.val)
			}
			if got != tt.want {
				t.Errorf("%s: got %q, want %q", tt.val, got, tt.want)
			}
		}
	})
}

func TestHash(t *testing.T) {
	x := map[string]any{
		"s": []any{1, "foo", nil, true},
		"f": 2.5,
		"m": map[string]any{
			"n":      json.Number("123.456"),
			"schema": &Schema{Type: "integer", UniqueItems: true},
		},
		"c": 1.2 + 3.4i,
		"n": nil,
	}

	seed := maphash.MakeSeed()

	hash := func(x any) uint64 {
		var h maphash.Hash
		h.SetSeed(seed)
		hashValue(&h, reflect.ValueOf(x))
		return h.Sum64()
	}

	want := hash(x)
	// Run several times to verify consistency.
	for range 10 {
		if got := hash(x); got != want {
			t.Errorf("hash values differ: %d vs. %d", got, want)
		}
	}

	// Check mathematically equal values.
	nums := []any{
		5,
		uint(5),
		5.0,
		json.Number("5"),
		json.Number("5.00"),
	}
	for i, n := range nums {
		if i == 0 {
			want = hash(n)
		} else if got := hash(n); got != want {
			t.Errorf("hashes differ between %v (%[1]T) and %v (%[2]T)", nums[0], n)
		}
	}

	// Check that a bare JSON `null` is OK.
	var null any
	if err := json.Unmarshal([]byte(`null`), &null); err != nil {
		t.Fatal(err)
	}
	_ = hash(null)
}

func TestMarshalStructWithMap(t *testing.T) {
	type S struct {
		A int
		B string `json:"b,omitempty"`
		u bool
		M map[string]any `json:"-"`
	}
	t.Run("basic", func(t *testing.T) {
		s := S{A: 1, B: "two", M: map[string]any{"!@#": true}}
		got, err := marshalStructWithMap(&s, "M")
		if err != nil {
			t.Fatal(err)
		}
		want := `{"A":1,"b":"two","!@#":true}`
		if g := string(got); g != want {
			t.Errorf("\ngot  %s\nwant %s", g, want)
		}

		var un S
		if err := unmarshalStructWithMap(got, &un, "M"); err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(s, un, cmpopts.IgnoreUnexported(S{})); diff != "" {
			t.Errorf("mismatch (-want, +got):\n%s", diff)
		}
	})
	t.Run("duplicate", func(t *testing.T) {
		s := S{A: 1, B: "two", M: map[string]any{"b": "dup"}}
		_, err := marshalStructWithMap(&s, "M")
		if err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Errorf("got %v, want error with 'duplicate'", err)
		}
	})
	t.Run("embedded", func(t *testing.T) {
		type Embedded struct {
			A     int
			B     int
			Extra map[string]any `json:"-"`
		}
		type S struct {
			C int
			Embedded
		}
		s := S{C: 1, Embedded: Embedded{A: 2, B: 3, Extra: map[string]any{"d": 4, "e": 5}}}
		got, err := marshalStructWithMap(&s, "Extra")
		if err != nil {
			t.Fatal(err)
		}
		want := `{"C":1,"A":2,"B":3,"d":4,"e":5}`
		if g := string(got); g != want {
			t.Errorf("got %v, want %v", g, want)
		}
	})
}

func TestJSONInfo(t *testing.T) {
	type S struct {
		A int
		B int `json:","`
		C int `json:"-"`
		D int `json:"-,"`
		E int `json:"echo"`
		F int `json:"foxtrot,omitempty"`
		g int `json:"golf"`
	}
	want := []jsonInfo{
		{name: "A"},
		{name: "B"},
		{omit: true},
		{name: "-"},
		{name: "echo"},
		{name: "foxtrot", settings: map[string]bool{"omitempty": true}},
		{omit: true},
	}
	tt := reflect.TypeFor[S]()
	for i := range tt.NumField() {
		got := fieldJSONInfo(tt.Field(i))
		if !reflect.DeepEqual(got, want[i]) {
			t.Errorf("got %+v, want %+v", got, want[i])
		}
	}
}
