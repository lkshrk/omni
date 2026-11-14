package config

import "testing"

func TestValidateMCPSecretReferences(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		value   string
		message string
	}{
		{"exact", "${LITELLM_API}", ""},
		{"embedded", "Bearer ${TOKEN}", ""},
		{"plain", "plain", ""},
		{"default", "${TOKEN:-x}", "use an exact ${VAR} reference; defaults are not supported"},
		{"default assign", "${TOKEN:=x}", "use an exact ${VAR} reference; defaults are not supported"},
		{"error form", "${TOKEN:?x}", "use an exact ${VAR} reference"},
		{"dash form", "${TOKEN-x}", "use an exact ${VAR} reference"},
		{"empty", "${}", "use an exact ${VAR} reference"},
		{"nested brace", "${A{B}", "use an exact ${VAR} reference"},
		{"unterminated", "${TOKEN", "use an exact ${VAR} reference; unterminated ${"},
		{"good and bad", "${GOOD} ${TOKEN:-x}", "use an exact ${VAR} reference; defaults are not supported"},
		{"env prefix", "${env:TOKEN}", "write ${TOKEN} instead of ${env:TOKEN}"},
		{"angle", "<TOKEN>", "write ${TOKEN} instead of <TOKEN>"},
		{"angle embedded", "Bearer <TOKEN>", "write ${TOKEN} instead of <TOKEN>"},
		{"lowercase angle", "<token>", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := AgentMCPServer{
				Env:     map[string]string{"KEY": tc.value},
				Headers: map[string]string{"Authorization": tc.value},
			}
			errs := validateMCPSecretReferences("$.agents.mcp[0]", server)
			if tc.message == "" {
				if len(errs) != 0 {
					t.Fatalf("unexpected errors: %v", errs)
				}
				return
			}
			if len(errs) != 2 {
				t.Fatalf("got %d errors, want 2: %v", len(errs), errs)
			}
			want := map[string]bool{
				"$.agents.mcp[0].env.KEY":               true,
				"$.agents.mcp[0].headers.Authorization": true,
			}
			for _, e := range errs {
				if !want[e.Path] || e.Message != tc.message {
					t.Fatalf("unexpected error %+v", e)
				}
			}
		})
	}
}
