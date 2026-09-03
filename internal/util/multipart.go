// internal/util/multipart.go
package util

import (
	"strings"
)

func ExtractLabels(fileNames []string) []string {
	if len(fileNames) <= 1 {
		return []string{""} // No label needed for single files
	}

	// 1. Find the minimum length among all strings to prevent out-of-bounds
	minLen := len(fileNames[0])
	for _, name := range fileNames[1:] {
		if len(name) < minLen {
			minLen = len(name)
		}
	}

	// 2. Find the length of the common prefix
	prefixLen := 0
	for i := 0; i < minLen; i++ {
		char := fileNames[0][i]
		match := true
		for _, name := range fileNames[1:] {
			if name[i] != char {
				match = false
				break
			}
		}
		if !match {
			break
		}
		prefixLen++
	}

	// 3. Find the length of the common suffix
	// CRITICAL: maxSuffixLen ensures the suffix never overlaps with the prefix
	maxSuffixLen := minLen - prefixLen
	suffixLen := 0
	for i := 0; i < maxSuffixLen; i++ {
		// Compare characters starting from the end of the strings
		char := fileNames[0][len(fileNames[0])-1-i]
		match := true
		for _, name := range fileNames[1:] {
			if name[len(name)-1-i] != char {
				match = false
				break
			}
		}
		if !match {
			break
		}
		suffixLen++
	}

	// 4. Extract and trim labels
	labels := make([]string, len(fileNames))
	for i, name := range fileNames {
		label := name[prefixLen : len(name)-suffixLen]
		labels[i] = strings.Trim(label, "-_ ")
	}

	return labels
}
