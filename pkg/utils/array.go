package utils

func ClearDuplicates[T comparable](slice []T) []T {
	seen := make(map[T]bool)
	for _, val := range slice {
		seen[val] = true
	}

	var result []T
	for key := range seen {
		result = append(result, key)
	}

	return result
}
