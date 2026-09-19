package scope

import (
	"errors"
	"strings"

	"scopr/internal/scopefile"
)

// Prefix marks an argument as a saved scope rather than a repository. Separate
// syntax keeps the namespaces apart, so a scope and a repo may share a name.
const Prefix = "@"

// ResolveArgs expands any @name argument to the repositories that scope holds,
// keeping position, then resolves the whole list. A repository that came from
// a scope reports that scope when it fails to resolve.
//
// Expansion failures stop the call: resolving a name list that is already
// wrong would report errors for repositories nobody asked for.
func ResolveArgs(root string, args []string) (Scope, error) {
	names, err := expand(root, args)
	if err != nil {
		return Scope{}, err
	}
	return resolveNamed(root, names)
}

func expand(root string, args []string) ([]named, error) {
	var (
		names    []named
		problems []error
	)

	for _, arg := range args {
		from, ok := strings.CutPrefix(arg, Prefix)
		if !ok {
			names = append(names, named{name: arg})
			continue
		}

		repos, err := scopefile.Load(root, from)
		if err != nil {
			problems = append(problems, err)
			continue
		}

		for _, r := range repos {
			names = append(names, named{name: r, from: from})
		}
	}

	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	return names, nil
}
