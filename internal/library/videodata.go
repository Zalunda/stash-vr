package library

import (
	"fmt"
	"slices"
	"sort"
	"stash-vr/internal/stash/gql"
	"stash-vr/internal/util"
	"strings"
)

type VideoData struct {
	SceneParts *gql.SceneParts
}

func (vd VideoData) Title() string {
	return util.FirstNonEmpty(vd.SceneParts.Title, &vd.SceneParts.Files[0].Basename)
}

func (vd VideoData) Id() string {
	return vd.SceneParts.Id
}

func ParseVirtualId(virtualId string) (string, string) {
	parts := strings.Split(virtualId, "_")
	if len(parts) == 2 {
		return parts[0], parts[1] // Returns SceneID, FileID
	}
	return virtualId, ""
}

func MakeVirtualId(sceneId string, fileId string) string {
	return sceneId + "_" + fileId
}

// GetFileLabels returns a map of FileID to its extracted label
func (vd VideoData) GetFileLabels() map[string]string {
	files := vd.SceneParts.Files
	labelsMap := make(map[string]string, len(files))

	if len(files) == 0 {
		return labelsMap
	}
	if len(files) == 1 {
		labelsMap[files[0].Id] = ""
		return labelsMap
	}

	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.Basename
	}

	extracted := ExtractLabels(names)
	for i, f := range files {
		labelsMap[f.Id] = extracted[i]
	}

	return labelsMap
}

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
	labelsValid := true

	for i, name := range fileNames {
		label := name[prefixLen : len(name)-suffixLen]
		label = strings.Trim(label, "-_ ")
		labels[i] = label

		// Validation: empty or too long
		if len(label) == 0 || len(label) > 3 {
			labelsValid = false
		}
	}

	// 5. Fallback: Sort filenames alphabetically and assign 001, 002, 003...
	if !labelsValid {
		// Create indices array [0, 1, 2...]
		indices := make([]int, len(fileNames))
		for i := range indices {
			indices[i] = i
		}

		// Sort the indices based on the alphabetical order of the filenames
		sort.SliceStable(indices, func(i, j int) bool {
			return fileNames[indices[i]] < fileNames[indices[j]]
		})

		// Assign labels in sorted order, but place them back in their original array positions
		for i, originalIndex := range indices {
			labels[originalIndex] = fmt.Sprintf("%03d", i+1)
		}
	}

	return labels
}

// GetFilesSortedByLabel returns a copy of files sorted by their label, and the label map
func (vd VideoData) GetFilesSortedByLabel() ([]*gql.ScenePartsFilesVideoFile, map[string]string) {
	labels := vd.GetFileLabels()

	sorted := make([]*gql.ScenePartsFilesVideoFile, len(vd.SceneParts.Files))
	copy(sorted, vd.SceneParts.Files)

	slices.SortFunc(sorted, func(a, b *gql.ScenePartsFilesVideoFile) int {
		return strings.Compare(labels[a.Id], labels[b.Id])
	})

	return sorted, labels
}
