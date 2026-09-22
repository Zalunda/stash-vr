package library

import (
	"fmt"
	"slices"
	"sort"
	"stash-vr/internal/config"
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

// Ensure MakeVirtualId ignores empty fileIds
func MakeVirtualId(sceneId string, fileId string) string {
	if fileId == "" {
		return sceneId
	}
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

// PlaybackItem represents a single playable entry, masking whether it's multipart or not.
type PlaybackItem struct {
	FileId string
	Label  string
	File   *gql.ScenePartsFilesVideoFile
}

// GetPlaybackItems handles the "expand feature" toggle configuration natively.
func (vd VideoData) GetPlaybackItems() []PlaybackItem {
	files := vd.SceneParts.Files

	// 1. Initial check: Is expansion enabled and do we have multiple files?
	shouldExpand := config.Application().EnablePartsExpansion && len(files) > 1

	// 2. Duration Heuristic: If all files have essentially the same duration,
	// they are likely just different resolutions/encodings of the same video.
	if shouldExpand {
		minDur := files[0].Duration
		maxDur := files[0].Duration
		for _, f := range files[1:] {
			if f.Duration < minDur {
				minDur = f.Duration
			}
			if f.Duration > maxDur {
				maxDur = f.Duration
			}
		}

		// If the difference between the longest and shortest file is <= 1.0 seconds, do not expand.
		if maxDur-minDur <= 1.0 {
			shouldExpand = false
		}
	}

	// 3. Return vanilla behavior if we shouldn't expand
	if !shouldExpand {
		// Safety check in case a scene genuinely has 0 files attached in Stash
		if len(files) == 0 {
			return []PlaybackItem{}
		}
		return []PlaybackItem{{
			FileId: "", // Empty ID prevents virtual routing
			Label:  "",
			File:   files[0],
		}}
	}

	// 4. Return expanded multi-part items
	sorted, labels := vd.GetFilesSortedByLabel()
	items := make([]PlaybackItem, len(sorted))
	for i, f := range sorted {
		items[i] = PlaybackItem{
			FileId: f.Id,
			Label:  labels[f.Id],
			File:   f,
		}
	}
	return items
}
