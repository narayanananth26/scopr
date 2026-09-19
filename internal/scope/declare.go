package scope

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AgentName is the subagent type injected via --agents. The declaration tells
// the session to prefer it, since --append-system-prompt does not reach
// subagents.
const AgentName = "scoped"

// Declaration is the text passed to --append-system-prompt.
//
// Written as project documentation rather than as a directive block: a scope
// phrased as an instruction with a challenge token is refused as prompt
// injection.
func Declaration(s Scope) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## Repository scope\n\n")
	fmt.Fprintf(&b, "The working directory is %s.\n\n", s.Primary().Path)

	if others := s.Others(); len(others) > 0 {
		b.WriteString("These repositories are also in scope:\n\n")
		for _, r := range others {
			fmt.Fprintf(&b, "- %s\n", r.Path)
		}
		b.WriteString("\n")
	}

	b.WriteString("No other repository in this workspace is in scope. ")
	b.WriteString("Work outside the paths above is out of scope for this session.\n\n")

	if len(s.Others()) > 0 {
		fmt.Fprintf(&b, "When work belongs to a repository other than %s, cd into it first "+
			"so that searches and relative paths resolve there.\n\n", s.Primary().Path)
	}

	fmt.Fprintf(&b, "Spawn subagents as the %q agent type; it carries this same scope. "+
		"Other agent types do not.\n", AgentName)

	return b.String()
}

type agentDef struct {
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
}

// AgentsJSON is the value passed to --agents. It defines one agent carrying
// the same scope text, because --append-system-prompt is not inherited by
// subagents.
func AgentsJSON(s Scope) (string, error) {
	agents := map[string]agentDef{
		AgentName: {
			Description: fmt.Sprintf("Works within the repository scope rooted at %s.", s.Primary().Path),
			Prompt:      Declaration(s),
		},
	}

	out, err := json.Marshal(agents)
	if err != nil {
		return "", fmt.Errorf("marshal agent definition: %w", err)
	}
	return string(out), nil
}
