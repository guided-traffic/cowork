package api

import (
	"maps"
	"slices"

	"github.com/oapi-codegen/nullable"
)

// nullableOf sets a nullable field explicitly: the value, or null. An unset
// Nullable would encode as the zero value, never as null.
func nullableOf[T any](v *T) nullable.Nullable[T] {
	if v == nil {
		return nullable.NewNullNullable[T]()
	}
	return nullable.NewNullableWithValue(*v)
}

func nullableString(v *string) nullable.Nullable[string] { return nullableOf(v) }

func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}
