package jsontext

import "testing"

func TestValidateObjectStringFieldMatchesJSONFieldCasing(t *testing.T) {
	for _, raw := range []string{
		`{"label":"\ud800"}`,
		`{"Label":"\ud800"}`,
		`{"LABEL":"\ud800"}`,
		`{"lAbEl":"\ud800"}`,
	} {
		if err := ValidateObjectStringField([]byte(raw), "label"); err == nil {
			t.Errorf("ValidateObjectStringField(%s) accepted malformed surrogate", raw)
		}
	}
	if err := ValidateObjectStringField([]byte(`{"other":"\ud800"}`), "label"); err != nil {
		t.Fatalf("unrelated field: %v", err)
	}
}

func TestValidateStringSurrogates(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want bool
	}{
		{`"plain"`, true},
		{`"literal �"`, true},
		{`"\ud83d\ude00"`, true},
		{`"\ud800"`, false},
		{`"\ud800x"`, false},
		{`"\ud800\u0041"`, false},
		{`"\udc00"`, false},
	} {
		err := ValidateStringSurrogates([]byte(test.raw))
		if (err == nil) != test.want {
			t.Errorf("ValidateStringSurrogates(%s) = %v, valid=%t", test.raw, err, test.want)
		}
	}
}
