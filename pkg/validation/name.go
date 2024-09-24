package validation

import "regexp"

// create a lower kebab case only regex
var lowerKebabCase, _ = regexp.Compile("^[a-z0-9]+(-[a-z0-9])*$")

func IsValidResourceName(name string) bool {
	return lowerKebabCase.Match([]byte(name))
}
