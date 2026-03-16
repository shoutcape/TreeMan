// Package validate provides input validation for treeman commands.
package validate

import (
	"errors"
	"regexp"
	"strings"
)

// invalidBranchChars matches characters git forbids in branch names.
// Mirrors the regex in wt.sh:623: [[:space:]~^:?*[\]
var invalidBranchChars = regexp.MustCompile(`[\s~^:?*\[\\]`)

// prNumberPattern matches a non-empty string of digits only.
var prNumberPattern = regexp.MustCompile(`^[0-9]+$`)

// BranchName returns an error if name is empty or contains characters that
// git does not allow in branch names.
//
// Mirrors the validation in wt.sh:617-626.
func BranchName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("branch name must not be empty")
	}
	if invalidBranchChars.MatchString(name) {
		return errors.New("branch name contains invalid characters")
	}
	return nil
}

// PRNumber returns an error if input is empty or not a positive integer string.
//
// Mirrors _wt_validate_pr_number in wt.sh:254.
func PRNumber(input string) error {
	if !prNumberPattern.MatchString(input) {
		return errors.New("PR/MR number must be numeric")
	}
	return nil
}
