package prompt

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed harbourmaster.md
var Harbourmaster string

//go:embed pilot.md
var Pilot string

//go:embed commodore.md
var Commodore string

//go:embed specialist.md
var Specialist string

//go:embed search_specialist.md
var SearchSpecialist string

//go:embed lookout.md
var Lookout string

// Validate checks that every embedded prompt has content.
func Validate() error {
	prompts := map[string]string{
		"harbourmaster":     Harbourmaster,
		"pilot":             Pilot,
		"commodore":         Commodore,
		"specialist":        Specialist,
		"search_specialist": SearchSpecialist,
		"lookout":           Lookout,
	}
	for name, p := range prompts {
		if len(strings.TrimSpace(p)) == 0 {
			return fmt.Errorf("embedded prompt %q is empty — check internal/prompt/ directory", name)
		}
	}
	return nil
}
