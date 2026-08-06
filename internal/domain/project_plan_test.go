package domain

import "testing"

func TestProjectPlanTransitionOnlyAllowsDraftToActiveAndActiveToClosed(t *testing.T) {
	for _, transition := range [][3]any{
		{"draft", "active", true},
		{"active", "closed", true},
		{"draft", "closed", false},
		{"active", "draft", false},
		{"closed", "active", false},
	} {
		from, to, want := transition[0].(string), transition[1].(string), transition[2].(bool)
		if got := ValidProjectPlanTransition(from, to); got != want {
			t.Errorf("ValidProjectPlanTransition(%q, %q) = %t, want %t", from, to, got, want)
		}
	}
}
