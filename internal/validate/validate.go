// Package validate provides input validation for treeman commands.
package validate

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// invalidBranchChars matches characters git forbids in branch names.
var invalidBranchChars = regexp.MustCompile(`[\s~^:?*\[\\]`)

// prNumberPattern matches a non-empty string of digits only.
var prNumberPattern = regexp.MustCompile(`^[0-9]+$`)

// BranchName returns an error if name is empty or contains characters that
// git does not allow in branch names.
func BranchName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("branch name must not be empty")
	}
	if invalidBranchChars.MatchString(name) {
		return errors.New("branch name contains invalid characters")
	}
	return nil
}

// PRNumber parses input as a positive PR/MR number representable by an int.
func PRNumber(input string) (int, error) {
	if !prNumberPattern.MatchString(input) {
		return 0, errors.New("PR/MR number must be numeric")
	}

	number, err := strconv.Atoi(input)
	if err != nil {
		return 0, errors.New("PR/MR number is too large")
	}
	if number <= 0 {
		return 0, errors.New("PR/MR number must be positive")
	}
	return number, nil
}
