// Package prompts embeds the system-instruction text for each of the
// researcher service's agents, keeping the prompt content next to the
// go:embed directives that load it.
package prompts

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

// Validate checks that every embedded prompt has content, so that an
// accidentally empty file fails fast at startup with a clear message
// instead of producing an agent with a blank system instruction.
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
			return fmt.Errorf("embedded prompt %q is empty — check prompts/ directory", name)
		}
	}
	return nil
}
