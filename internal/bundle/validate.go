// Package bundle preserves the bundle validation API used by recording tests.
package bundle

import "github.com/paulsmith/twee/internal/tracebundle"

// ValidateResult reports the result of exhaustively validating and decoding a
// bundle. Events counts records whose common event header parsed.
type ValidateResult = tracebundle.Validation

// Validate opens path once and reports every independently detectable content
// issue. Filesystem failures are returned as errors; malformed readable bundle
// content is returned in ValidateResult.Issues.
func Validate(path string) (ValidateResult, error) {
	_, validation, err := tracebundle.OpenValidated(path)
	return validation, err
}
