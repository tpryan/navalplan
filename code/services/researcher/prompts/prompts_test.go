package prompts

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	if err := Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil for the real embedded prompt files", err)
	}
}

func TestPromptsAreNonEmpty(t *testing.T) {
	all := map[string]string{
		"Harbourmaster":    Harbourmaster,
		"Pilot":            Pilot,
		"Commodore":        Commodore,
		"Specialist":       Specialist,
		"SearchSpecialist": SearchSpecialist,
		"Lookout":          Lookout,
	}

	for name, p := range all {
		if strings.TrimSpace(p) == "" {
			t.Errorf("prompts.%s is empty", name)
		}
	}
}
