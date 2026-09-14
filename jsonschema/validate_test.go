// Copyright 2025 The JSON Schema Go Project Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package jsonschema

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// The test for validation uses the official test suite, expressed as a set of JSON files.
// Each file is an array of group objects.

// A testGroup consists of a schema and some tests on it.
type testGroup struct {
	Description string
	Schema      *Schema
	Tests       []test
}

// A test consists of a JSON instance to be validated and the expected result.
type test struct {
	Description string
	Data        any
	Valid       bool
	ErrContains string
}

func TestValidateDraft2020_12(t *testing.T) {
	files, err := filepath.Glob(filepath.FromSlash("testdata/draft2020-12/*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no files")
	}
	testValidate(t, files, "")
}

func TestValidateDraft7(t *testing.T) {
	files, err := filepath.Glob(filepath.FromSlash("testdata/draft7/*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no files")
	}
	testValidate(t, files, "https://json-schema.org/draft-07/schema#")
}

func testValidate(t *testing.T, files []string, draft string) {
	for _, file := range files {
		base := filepath.Base(file)
		t.Run(base, func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var groups []testGroup
			if err := json.Unmarshal(data, &groups); err != nil {
				t.Fatal(err)
			}
			for _, g := range groups {
				t.Run(g.Description, func(t *testing.T) {
					if g.Schema.Schema == "" {
						g.Schema.Schema = draft
					}
					rs, err := g.Schema.Resolve(&ResolveOptions{Loader: loadRemote})
					if err != nil {
						t.Fatal(err)
					}
					for _, test := range g.Tests {
						t.Run(test.Description, func(t *testing.T) {
							err = rs.Validate(test.Data)
							if err != nil && test.Valid {
								t.Errorf("wanted success, but failed with: %v", err)
							}
							if err == nil && !test.Valid {
								t.Error("succeeded but wanted failure")
							}
							if err != nil && test.ErrContains != "" {
								if !strings.Contains(err.Error(), test.ErrContains) {
									t.Errorf("got error %q, want containing %q", err, test.ErrContains)
								}
							}
							if t.Failed() {
								t.Errorf("schema: %s", g.Schema.json())
								t.Fatalf("instance: %v (%[1]T)", test.Data)
							}
						})
					}
				})
			}
		})
	}
}

func TestValidateErrors(t *testing.T) {
	schema := &Schema{
		PrefixItems: []*Schema{{Contains: &Schema{Type: "integer"}}},
	}
	rs, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	err = rs.Validate([]any{[]any{"1"}})
	want := "prefixItems/0"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("error:\n%s\ndoes not contain %q", err, want)
	}
}

func TestValidateDefaults(t *testing.T) {
	s := &Schema{
		Properties: map[string]*Schema{
			"a": {Type: "integer", Default: mustMarshal(1)},
			"b": {Type: "string", Default: mustMarshal("s")},
		},
		Default: mustMarshal(map[string]any{"a": 1, "b": "two"}),
	}
	if _, err := s.Resolve(&ResolveOptions{ValidateDefaults: true}); err != nil {
		t.Fatal(err)
	}

	s = &Schema{
		Properties: map[string]*Schema{
			"a": {Type: "integer", Default: mustMarshal(3)},
			"b": {Type: "string", Default: mustMarshal("s")},
		},
		Default: mustMarshal(map[string]any{"a": 1, "b": 2}),
	}
	_, err := s.Resolve(&ResolveOptions{ValidateDefaults: true})
	want := `has type "integer", want "string"`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Resolve returned error %q, want %q", err, want)
	}
}

func TestApplyDefaults(t *testing.T) {
	schema := &Schema{
		Properties: map[string]*Schema{
			"A": {Default: mustMarshal(1)},
			"B": {Default: mustMarshal(2)},
			"C": {Default: mustMarshal(3)},
		},
		Required: []string{"C"},
	}
	rs, err := schema.Resolve(&ResolveOptions{ValidateDefaults: true})
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		instancep any // pointer to instance value
		want      any // desired value (not a pointer)
	}{
		{
			&map[string]any{"B": 0},
			map[string]any{
				"A": float64(1), // filled from default
				"B": 0,          // untouched: it was already there
				// "C" not added: it is required (Validate will catch that)
			},
		},
	} {
		if err := rs.ApplyDefaults(tt.instancep); err != nil {
			t.Fatal(err)
		}
		got := reflect.ValueOf(tt.instancep).Elem().Interface() // dereference the pointer
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("\ngot  %#v\nwant %#v", got, tt.want)
		}
	}
}

func TestApplyNestedDefaults(t *testing.T) {
	base := &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"A": {
				Type: "object",
				Properties: map[string]*Schema{
					"B": {Type: "string", Default: mustMarshal("foo")},
				},
			},
		},
	}
	// Variant where parent A has its own default object; recursion should still fill B.
	withParentDefault := &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"A": {
				Type:    "object",
				Default: mustMarshal(map[string]any{"X": 1}),
				Properties: map[string]*Schema{
					"B": {Type: "string", Default: mustMarshal("foo")},
				},
			},
		},
	}

	for _, tc := range []struct {
		name      string
		schema    *Schema
		instancep any
		want      any
	}{
		{
			name:      "MapMissingParent",
			schema:    base,
			instancep: &map[string]any{},
			want:      map[string]any{"A": map[string]any{"B": "foo"}},
		},
		{
			name:      "MapEmptyParent",
			schema:    base,
			instancep: &map[string]any{"A": map[string]any{}},
			want:      map[string]any{"A": map[string]any{"B": "foo"}},
		},
		{
			name:      "MapParentHasDefaultObjectMissing",
			schema:    withParentDefault,
			instancep: &map[string]any{},
			want:      map[string]any{"A": map[string]any{"X": float64(1), "B": "foo"}},
		},
		{
			name:      "MapParentHasDefaultObjectPresent",
			schema:    withParentDefault,
			instancep: &map[string]any{"A": map[string]any{}},
			// Parent default is applied only when the property is missing, so
			// with the key present (even if empty), we apply only the nested defaults.
			want: map[string]any{"A": map[string]any{"B": "foo"}},
		},
		{
			name:      "MapValueMapMissingParentTyped",
			schema:    base,
			instancep: &map[string]map[string]any{},
			want:      map[string]map[string]any{"A": {"B": "foo"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rs, err := tc.schema.Resolve(&ResolveOptions{ValidateDefaults: true})
			if err != nil {
				t.Fatal(err)
			}
			if err := rs.ApplyDefaults(tc.instancep); err != nil {
				t.Fatal(err)
			}
			got := reflect.ValueOf(tc.instancep).Elem().Interface()
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("nested defaults:\n got  %#v\n want %#v", got, tc.want)
			}
		})
	}
}

func TestApplyDefaultsWithRef(t *testing.T) {
	for _, tt := range []struct {
		name      string
		schema    Schema
		instancep *map[string]any
		want      map[string]any
	}{
		{
			name: "RefHasDefault",
			schema: Schema{
				Type: "object",
				Properties: map[string]*Schema{
					"A": {Ref: "#/definitions/A"},
				},
				Definitions: map[string]*Schema{
					"A": {
						Type:    "array",
						Default: mustMarshal([]any{}),
					},
				},
			},
			instancep: &map[string]any{},
			want:      map[string]any{"A": []any{}},
		},
		{
			name: "RefDefaultOverriden",
			schema: Schema{
				Type: "object",
				Properties: map[string]*Schema{
					"A": {
						Ref:     "#/definitions/A",
						Default: mustMarshal([]any{1, 2, 3}),
					},
				},
				Definitions: map[string]*Schema{
					"A": {
						Type:    "array",
						Default: mustMarshal([]any{}),
					},
				},
			},
			instancep: &map[string]any{},
			want:      map[string]any{"A": []any{float64(1), float64(2), float64(3)}},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res, err := tt.schema.Resolve(&ResolveOptions{ValidateDefaults: true})
			if err != nil {
				t.Fatal(err)
			}
			if err := res.ApplyDefaults(tt.instancep); err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(&tt.want, tt.instancep, cmpopts.IgnoreUnexported(Schema{})); diff != "" {
				t.Fatalf("Schema mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStructInstance(t *testing.T) {
	instance := struct {
		I int
		B bool `json:"b"`
		P *int // either missing or nil
		u int  // unexported: not a property
	}{1, true, nil, 0}

	for _, tt := range []struct {
		s    Schema
		want bool
	}{
		{
			Schema{MinProperties: Ptr(4)},
			false,
		},
		{
			Schema{MinProperties: Ptr(3)},
			true, // P interpreted as present
		},
		{
			Schema{MaxProperties: Ptr(1)},
			false,
		},
		{
			Schema{MaxProperties: Ptr(2)},
			true, // P interpreted as absent
		},
		{
			Schema{Required: []string{"i"}}, // the name is "I"
			false,
		},
		{
			Schema{Required: []string{"B"}}, // the name is "b"
			false,
		},
		{
			Schema{PropertyNames: &Schema{MinLength: Ptr(2)}},
			false,
		},
		{
			Schema{Properties: map[string]*Schema{"b": {Type: "boolean"}}},
			true,
		},
		{
			Schema{Properties: map[string]*Schema{"b": {Type: "number"}}},
			false,
		},
		{
			Schema{Required: []string{"I"}},
			true,
		},
		{
			Schema{Required: []string{"I", "P"}},
			true, // P interpreted as present
		},
		{
			Schema{Required: []string{"I", "P"}, Properties: map[string]*Schema{"P": {Type: "number"}}},
			false, // P interpreted as present, but not a number
		},
		{
			Schema{Required: []string{"I"}, Properties: map[string]*Schema{"P": {Type: "number"}}},
			true, // P not required, so interpreted as absent
		},
		{
			Schema{Required: []string{"I"}, AdditionalProperties: falseSchema()},
			false,
		},
		{
			Schema{DependentRequired: map[string][]string{"b": {"u"}}},
			false,
		},
		{
			Schema{DependentSchemas: map[string]*Schema{"b": falseSchema()}},
			false,
		},
		{
			Schema{UnevaluatedProperties: falseSchema()},
			false,
		},
	} {
		res, err := tt.s.Resolve(nil)
		if err != nil {
			t.Fatal(err)
		}
		err = res.Validate(instance)
		// Validating a struct always fails.
		if err == nil {
			t.Error("struct validation succeeded")
		}
	}
}

func TestStructEmbedding(t *testing.T) {
	// For exported pointer embedding
	type Apple struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	type Banana struct {
		*Apple        // Pointer embedded - should flatten.
		Extra  string `json:"extra"`
	}

	// For unexported pointer embedding
	type cranberry struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	type Durian struct {
		*cranberry        // Pointer embedded - should flatten.
		Extra      string `json:"extra"`
	}

	// For exported value embedding
	type Elderberry struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	type Fig struct {
		Elderberry        // Value embedded - should flatten.
		Extra      string `json:"extra"`
	}

	// For unexported value embedding
	type grape struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	type Honeyberry struct {
		grape        // Value embedded - should flatten.
		Extra string `json:"extra"`
	}

	// For outer field shadowing a pointer embed
	type Inner struct {
		Conflict  string `json:"conflict_field"` // This string field should be ignored.
		InnerOnly string `json:"inner_only"`
	}
	type Outer struct {
		*Inner
		Conflict int `json:"conflict_field"` // This int field should take precedence.
	}

	testCases := []struct {
		name          string
		targetType    reflect.Type
		wantSchema    *Schema
		validInstance any
	}{
		{
			name:       "Slice",
			targetType: reflect.TypeOf([]Banana{}),
			wantSchema: &Schema{
				Types: []string{"null", "array"},
				Items: &Schema{
					Type: "object",
					Properties: map[string]*Schema{
						"id":    {Type: "string"},
						"name":  {Type: "string"},
						"extra": {Type: "string"},
					},
					Required:             []string{"id", "name", "extra"},
					AdditionalProperties: falseSchema(),
					PropertyOrder:        []string{"id", "name", "extra"},
				},
			},
			validInstance: []Banana{
				{Apple: &Apple{ID: "foo1", Name: "Test Foo 2"}, Extra: "additional data 1"},
				{Apple: &Apple{ID: "foo2", Name: "Test Foo 2"}, Extra: "additional data 2"},
			},
		},
		{
			name:       "Array",
			targetType: reflect.TypeOf([2]Banana{}),
			wantSchema: &Schema{
				Type:     "array",
				MinItems: Ptr(2),
				MaxItems: Ptr(2),
				Items: &Schema{
					Type: "object",
					Properties: map[string]*Schema{
						"id":    {Type: "string"},
						"name":  {Type: "string"},
						"extra": {Type: "string"},
					},
					Required:             []string{"id", "name", "extra"},
					AdditionalProperties: falseSchema(),
					PropertyOrder:        []string{"id", "name", "extra"},
				},
			},
			validInstance: [2]Banana{
				{Apple: &Apple{ID: "foo1", Name: "Test Foo 2"}, Extra: "additional data 1"},
				{Apple: &Apple{ID: "foo2", Name: "Test Foo 2"}, Extra: "additional data 2"},
			},
		},
		{
			name:       "UnExportedPointer",
			targetType: reflect.TypeOf([]Durian{}),
			wantSchema: &Schema{
				Types: []string{"null", "array"},
				Items: &Schema{
					Type: "object",
					Properties: map[string]*Schema{
						"id":    {Type: "string"},
						"name":  {Type: "string"},
						"extra": {Type: "string"},
					},
					Required:             []string{"id", "name", "extra"},
					AdditionalProperties: falseSchema(),
					PropertyOrder:        []string{"id", "name", "extra"},
				},
			},
			validInstance: []Durian{
				{cranberry: &cranberry{ID: "foo1", Name: "Test Foo 2"}, Extra: "additional data 1"},
				{cranberry: &cranberry{ID: "foo2", Name: "Test Foo 2"}, Extra: "additional data 2"},
			},
		},
		{
			name:       "ExportedValue",
			targetType: reflect.TypeOf([]Fig{}),
			wantSchema: &Schema{
				Types: []string{"null", "array"},
				Items: &Schema{
					Type: "object",
					Properties: map[string]*Schema{
						"id":    {Type: "string"},
						"name":  {Type: "string"},
						"extra": {Type: "string"},
					},
					Required:             []string{"id", "name", "extra"},
					AdditionalProperties: falseSchema(),
					PropertyOrder:        []string{"id", "name", "extra"},
				},
			},
			validInstance: []Fig{
				{Elderberry: Elderberry{ID: "foo1", Name: "Test Foo 2"}, Extra: "additional data 1"},
				{Elderberry: Elderberry{ID: "foo2", Name: "Test Foo 2"}, Extra: "additional data 2"},
			},
		},
		{
			name:       "UnExportedValue",
			targetType: reflect.TypeOf([]Honeyberry{}),
			wantSchema: &Schema{
				Types: []string{"null", "array"},
				Items: &Schema{
					Type: "object",
					Properties: map[string]*Schema{
						"id":    {Type: "string"},
						"name":  {Type: "string"},
						"extra": {Type: "string"},
					},
					Required:             []string{"id", "name", "extra"},
					AdditionalProperties: falseSchema(),
					PropertyOrder:        []string{"id", "name", "extra"},
				},
			},
			validInstance: []Honeyberry{
				{grape: grape{ID: "foo1", Name: "Test Foo 2"}, Extra: "additional data 1"},
				{grape: grape{ID: "foo2", Name: "Test Foo 2"}, Extra: "additional data 2"},
			},
		},
		{
			name:       "FieldShadowing",
			targetType: reflect.TypeOf(Outer{}),
			wantSchema: &Schema{
				Type: "object",
				Properties: map[string]*Schema{
					// The "integer" from the Outer struct takes precedence.
					"conflict_field": {Type: "integer"},
					// The non-conflicting field from the Inner struct is still present.
					"inner_only": {Type: "string"},
				},
				Required:             []string{"inner_only", "conflict_field"},
				AdditionalProperties: falseSchema(),
				PropertyOrder:        []string{"inner_only", "conflict_field"},
			},
			validInstance: Outer{Inner: &Inner{InnerOnly: "data"}, Conflict: 123},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			schema, err := ForType(tc.targetType, &ForOptions{})
			if err != nil {
				t.Fatalf("ForType() returned an unexpected error: %v", err)
			}

			if diff := cmp.Diff(tc.wantSchema, schema, cmpopts.IgnoreUnexported(Schema{})); diff != "" {
				t.Fatalf("Schema mismatch (-want +got):\n%s", diff)
			}
			resolved, err := schema.Resolve(nil)
			if err != nil {
				t.Fatalf("schema.Resolve() failed: %v", err)
			}
			// Validate a correct instance against the generated schema.
			// Struct validation always fails.
			if err := resolved.Validate(tc.validInstance); err == nil {
				t.Error("struct validation succeeded")
			}
		})
	}
}

func mustMarshal(x any) json.RawMessage {
	data, err := json.Marshal(x)
	if err != nil {
		panic(err)
	}
	return json.RawMessage(data)
}

// loadRemote loads a remote reference used in the test suite.
func loadRemote(uri *url.URL) (*Schema, error) {
	// Anything with localhost:1234 refers to the remotes directory in the test suite repo.
	if uri.Host == "localhost:1234" {
		return loadSchemaFromFile(filepath.FromSlash(filepath.Join("testdata/remotes", uri.Path)))
	}
	// One test needs the meta-schema files.
	const metaPrefix = "https://json-schema.org/draft/2020-12/"
	if after, ok := strings.CutPrefix(uri.String(), metaPrefix); ok {
		return loadSchemaFromFile(filepath.FromSlash("meta-schemas/draft2020-12/" + after + ".json"))
	}
	const metaPrefixDraft7 = "https://json-schema.org/draft-07/"
	if after, ok := strings.CutPrefix(uri.String(), metaPrefixDraft7); ok {
		return loadSchemaFromFile(filepath.FromSlash("meta-schemas/draft7/" + after + ".json"))
	}
	const metaPrefixDraft7s = "http://json-schema.org/draft-07/"
	if after, ok := strings.CutPrefix(uri.String(), metaPrefixDraft7s); ok {
		return loadSchemaFromFile(filepath.FromSlash("meta-schemas/draft7/" + after + ".json"))
	}
	return nil, fmt.Errorf("don't know how to load %s", uri)
}

func loadSchemaFromFile(filename string) (*Schema, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var s Schema
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("unmarshaling JSON at %s: %w", filename, err)
	}
	return &s, nil
}

func TestValidateJSONNumber(t *testing.T) {
	t.Run("oneOf regression", func(t *testing.T) {
		one := 1
		s := &Schema{OneOf: []*Schema{
			{Type: "number", MaxLength: &one},
			{Type: "integer"},
		}}
		r, err := s.Resolve(nil)
		if err != nil {
			t.Fatal(err)
		}

		dec := json.NewDecoder(strings.NewReader(`42`))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			t.Fatal(err)
		}
		err = r.Validate(v)
		if err == nil || !strings.Contains(err.Error(), "oneOf: validated against both") {
			t.Errorf("got %v, want oneOf validation failure", err)
		}

		var f any
		if err := json.Unmarshal([]byte(`42`), &f); err != nil {
			t.Fatal(err)
		}
		err = r.Validate(f)
		if err == nil || !strings.Contains(err.Error(), "oneOf: validated against both") {
			t.Errorf("got %v, want oneOf validation failure", err)
		}
	})

	t.Run("null property regression", func(t *testing.T) {
		var m map[string]json.Number
		if err := json.Unmarshal([]byte(`{"a":null}`), &m); err != nil {
			t.Fatal(err)
		}

		s := &Schema{Properties: map[string]*Schema{
			"a": {Types: []string{"string", "null"}},
		}}
		r, err := s.Resolve(nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Validate(m); err != nil {
			t.Errorf("typed map: got error %v, want nil", err)
		}

		var plain map[string]any
		if err := json.Unmarshal([]byte(`{"a":null}`), &plain); err != nil {
			t.Fatal(err)
		}
		if err := r.Validate(plain); err != nil {
			t.Errorf("any map: got error %v, want nil", err)
		}
	})

	t.Run("large exponent regression", func(t *testing.T) {
		num := json.Number("1e9999999")

		sStr, _ := (&Schema{Type: "string"}).Resolve(nil)
		sNum, _ := (&Schema{Type: "number"}).Resolve(nil)
		sInt, _ := (&Schema{Type: "integer"}).Resolve(nil)
		sNumOrStr, _ := (&Schema{Types: []string{"number", "string"}}).Resolve(nil)

		if err := sStr.Validate(num); err != nil {
			t.Errorf("type:string on 1e9999999: got %v, want nil", err)
		}
		if err := sNumOrStr.Validate(num); err != nil {
			t.Errorf("type:[number,string] on 1e9999999: got %v, want nil", err)
		}
		if err := sNum.Validate(num); err == nil || strings.Contains(err.Error(), "is not a valid JSON value") {
			t.Errorf("type:number on 1e9999999: got %v, want standard type mismatch error", err)
		}
		if err := sInt.Validate(num); err == nil || strings.Contains(err.Error(), "is not a valid JSON value") {
			t.Errorf("type:integer on 1e9999999: got %v, want standard type mismatch error", err)
		}
	})

	t.Run("invalid numeric syntax", func(t *testing.T) {
		invalids := []string{
			"0x10",
			"1/2",
			"+5",
			".5",
			"1_000",
			"0123",
			"0b101",
		}

		sNum, _ := (&Schema{Type: "number"}).Resolve(nil)
		sInt, _ := (&Schema{Type: "integer"}).Resolve(nil)
		sStr, _ := (&Schema{Type: "string"}).Resolve(nil)

		for _, inv := range invalids {
			val := json.Number(inv)
			if err := sNum.Validate(val); err == nil {
				t.Errorf("type:number should reject %q", inv)
			}
			if err := sInt.Validate(val); err == nil {
				t.Errorf("type:integer should reject %q", inv)
			}
			if err := sStr.Validate(val); err != nil {
				t.Errorf("type:string should accept fallback %q, got %v", inv, err)
			}
		}
	})

	t.Run("exact precision", func(t *testing.T) {
		num := json.Number("1.0000000000000000001")

		sNum, _ := (&Schema{Type: "number"}).Resolve(nil)
		sInt, _ := (&Schema{Type: "integer"}).Resolve(nil)

		if err := sNum.Validate(num); err != nil {
			t.Errorf("type:number on %s: got %v, want nil", num, err)
		}
		if err := sInt.Validate(num); err == nil {
			t.Errorf("type:integer on %s: got nil, want type mismatch", num)
		}
	})

	t.Run("const and enum equality", func(t *testing.T) {
		sConstStr, _ := (&Schema{Const: Ptr[any]("42")}).Resolve(nil)
		sConstNum, _ := (&Schema{Const: Ptr[any](42)}).Resolve(nil)
		sEnumStr, _ := (&Schema{Enum: []any{"42"}}).Resolve(nil)
		sEnumNum, _ := (&Schema{Enum: []any{42}}).Resolve(nil)

		num := json.Number("42")
		// json.Number("42") must not equal string "42"
		if err := sConstStr.Validate(num); err == nil {
			t.Errorf("const '42' should not match json.Number('42')")
		}
		if err := sEnumStr.Validate(num); err == nil {
			t.Errorf("enum ['42'] should not match json.Number('42')")
		}

		// json.Number("42") must equal numeric 42
		if err := sConstNum.Validate(num); err != nil {
			t.Errorf("const 42 should match json.Number('42'), got %v", err)
		}
		if err := sEnumNum.Validate(num); err != nil {
			t.Errorf("enum [42] should match json.Number('42'), got %v", err)
		}
	})

	t.Run("nested const and enum", func(t *testing.T) {
		sConst, err := (&Schema{Const: Ptr[any](map[string]any{"a": 1})}).Resolve(nil)
		if err != nil {
			t.Fatal(err)
		}
		sEnum, err := (&Schema{Enum: []any{[]any{1, 2, 3}}}).Resolve(nil)
		if err != nil {
			t.Fatal(err)
		}
		sNotEnum, err := (&Schema{Not: &Schema{Enum: []any{[]any{1, 2, 3}}}}).Resolve(nil)
		if err != nil {
			t.Fatal(err)
		}

		// const with nested json.Number
		instMap := map[string]any{"a": json.Number("1")}
		if err := sConst.Validate(instMap); err != nil {
			t.Errorf("const with nested json.Number: got %v, want nil", err)
		}

		// enum with nested json.Number
		instSlice := []any{json.Number("1"), json.Number("2"), json.Number("3")}
		if err := sEnum.Validate(instSlice); err != nil {
			t.Errorf("enum with nested json.Number: got %v, want nil", err)
		}

		sEnumSingle, err := (&Schema{Enum: []any{[]any{1}}}).Resolve(nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := sEnumSingle.Validate([]any{json.Number("1")}); err != nil {
			t.Errorf("enum [[1]] with [json.Number('1')]: got %v, want nil", err)
		}

		// not/enum with nested json.Number
		if err := sNotEnum.Validate(instSlice); err == nil {
			t.Errorf("not/enum with nested json.Number: want error, got nil")
		}
	})

	t.Run("uniqueItems with json.Number", func(t *testing.T) {
		s, err := (&Schema{UniqueItems: true}).Resolve(nil)
		if err != nil {
			t.Fatal(err)
		}
		inst := []any{json.Number("42"), 42.0}
		if err := s.Validate(inst); err == nil {
			t.Errorf("uniqueItems with json.Number and float: want duplicate error, got nil")
		}
	})

	t.Run("if then with nested json.Number", func(t *testing.T) {
		s := &Schema{
			If: &Schema{
				Properties: map[string]*Schema{
					"meta": {
						Const: Ptr[any](map[string]any{"level": 1}),
					},
				},
				Required: []string{"meta"},
			},
			Then: &Schema{
				Required: []string{"auth"},
			},
		}
		r, err := s.Resolve(nil)
		if err != nil {
			t.Fatal(err)
		}

		dec := json.NewDecoder(strings.NewReader(`{"meta":{"level":1}}`))
		dec.UseNumber()
		var doc any
		if err := dec.Decode(&doc); err != nil {
			t.Fatal(err)
		}

		if err := r.Validate(doc); err == nil || !strings.Contains(err.Error(), `"auth"`) {
			t.Errorf("if/then with nested json.Number: got %v, want missing 'auth' error", err)
		}
	})
}
