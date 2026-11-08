package jsonschema

import (
	"encoding/json"
	"strings"
	"testing"
)

// Numeric errors name the instance and the bound as the user wrote them,
// not as big.Rat fractions and %f-padded floats ("0/1 is less than
// 1.000000").
func TestNumericErrorMessages(t *testing.T) {
	f := func(x float64) *float64 { return &x }
	for _, c := range []struct {
		schema   *Schema
		instance any
		want     string
	}{
		{&Schema{Type: "integer", Minimum: f(1)}, 0, "minimum: 0 is less than 1"},
		{&Schema{Type: "integer", Maximum: f(50)}, 51, "maximum: 51 is greater than 50"},
		{&Schema{Type: "number", Minimum: f(0.5)}, 0.25, "minimum: 0.25 is less than 0.5"},
		{&Schema{Type: "number", ExclusiveMaximum: f(2)}, 2.5, "exclusiveMaximum: 2.5 is greater than or equal to 2"},
		{&Schema{Type: "number", MultipleOf: f(0.5)}, 0.3, "multipleOf: 0.3 is not a multiple of 0.5"},
		{&Schema{Type: "number", Minimum: f(1)}, 1e-10, "minimum: 1e-10 is less than 1"},
		// json.Number keeps digits float64 would drop.
		{&Schema{Minimum: f(1)}, json.Number("0.30000000000000000001"), "minimum: 0.30000000000000000001 is less than 1"},
		{&Schema{Maximum: f(1)}, json.Number("10000000000000000001"), "maximum: 10000000000000000001 is greater than 1"},
	} {
		rs, err := c.schema.Resolve(nil)
		if err != nil {
			t.Fatal(err)
		}
		err = rs.Validate(c.instance)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("Validate(%v) = %v, want it to contain %q", c.instance, err, c.want)
		}
	}
}
