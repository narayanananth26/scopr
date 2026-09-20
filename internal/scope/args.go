package scope

import (
	"errors"
	"strings"

	"scopr/internal/scopefile"
)

const Prefix = "@"

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
