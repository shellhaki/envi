package api

import "testing"

// The operator list is parsed from one environment variable, so that parsing
// is the whole access-control surface. A stray space or a capital letter must
// not be the difference between an operator and a stranger.
func TestOperatorsParsing(t *testing.T) {
	cases := []struct {
		name  string
		value string
		allow []string
		deny  []string
	}{
		{
			name:  "one address, however it is typed",
			value: "ops@example.com",
			allow: []string{"ops@example.com", "OPS@Example.com", "  ops@example.com  "},
			deny:  []string{"other@example.com", ""},
		},
		{
			name:  "several, with the spacing people actually type",
			value: "a@example.com, b@example.com ,C@EXAMPLE.COM",
			allow: []string{"a@example.com", "b@example.com", "c@example.com"},
			deny:  []string{"d@example.com"},
		},
		{
			name:  "unset means nobody",
			value: "",
			deny:  []string{"", "ops@example.com"},
		},
		{
			// A user row with an empty email must not match an empty slot in
			// the list, which is the way this kind of check usually fails open.
			name:  "empty entries admit nobody",
			value: ",,  ,,",
			deny:  []string{"", " ", "ops@example.com"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ENVI_ADMINS", tc.value)
			allowed := operators()

			for _, email := range tc.allow {
				if _, ok := allowed[normaliseEmail(email)]; !ok {
					t.Errorf("%q was refused, want allowed", email)
				}
			}
			for _, email := range tc.deny {
				if _, ok := allowed[normaliseEmail(email)]; ok {
					t.Errorf("%q was allowed, want refused", email)
				}
			}
		})
	}
}

// With no ENVI_ADMINS set the map is empty, which is what makes RequireOperator
// refuse everybody rather than everybody.
func TestNoOperatorsConfiguredMeansEmpty(t *testing.T) {
	t.Setenv("ENVI_ADMINS", "")
	if n := len(operators()); n != 0 {
		t.Fatalf("operators() returned %d entries with ENVI_ADMINS unset", n)
	}
}
