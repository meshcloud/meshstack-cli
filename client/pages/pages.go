// Package pages reads the listings of the generated clients, which page through meshStack lazily.
package pages

import "iter"

// ReadAll returns the items read before an error, together with it.
func ReadAll[T any](seq iter.Seq2[T, error]) ([]T, error) {
	var items []T
	for item, err := range seq {
		if err != nil {
			return items, err
		}
		items = append(items, item)
	}
	return items, nil
}
