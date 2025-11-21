// Package validation provides helpers for defensive programming and contract enforcement.
package validation

import "fmt"

// AssertNotNil panics if the provided pointer is nil.
// It is intended for use in constructors and configuration phases where
// dependencies are mandatory (Fail Fast principle).
//
// Usage:
//
//	validation.AssertNotNil(db, "database pool")
func AssertNotNil[T any](ptr *T, name string) {
	if ptr == nil {
		panic(fmt.Sprintf("critical error: %s cannot be nil", name))
	}
}

// Note: We use panic here because this is for PROGRAMMER ERROR (misconfiguration),
// not for runtime errors (like "network down").
