package prompt

import "testing"

func TestValidate_SucceedsWithNonEmptyPrompts(t *testing.T) {
	if err := Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}
