// internal/util/multipart.go
package util

import "strings"

func ExtractLabels(fileNames []string) []string {
	if len(fileNames) <= 1 {
		return []string{""} // No label needed for single files
	}
	prefix := getCommonPrefix(fileNames)
	suffix := getCommonSuffix(fileNames)

	var labels []string
	for _, name := range fileNames {
		label := name[len(prefix) : len(name)-len(suffix)]
		label = strings.Trim(label, "-_ ")
		if label == "" {
			label = "Default"
		}
		labels = append(labels, label)
	}
	return labels
}

func getCommonPrefix(strs []string) string {
	if len(strs) == 0 {
		return ""
	}
	prefix := strs[0]
	for _, s := range strs[1:] {
		for !strings.HasPrefix(s, prefix) {
			if len(prefix) == 0 {
				return ""
			}
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

func getCommonSuffix(strs []string) string {
	if len(strs) == 0 {
		return ""
	}
	suffix := strs[0]
	for _, s := range strs[1:] {
		for !strings.HasSuffix(s, suffix) {
			if len(suffix) == 0 {
				return ""
			}
			suffix = suffix[1:]
		}
	}
	return suffix
}
