package library

import (
	"fmt"
	"stash-vr/internal/stash/gql"
	"stash-vr/internal/util"
	"strconv"
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

func (vd *VideoData) GetValidatedLabels() []string {
	if len(vd.SceneParts.Files) <= 1 {
		return []string{""} // No label needed for single files
	}

	fileNames := make([]string, len(vd.SceneParts.Files))
	for i, f := range vd.SceneParts.Files {
		fileNames[i] = f.Basename
	}
	labels := util.ExtractLabels(fileNames)

	labelsValid := true
	for _, lbl := range labels {
		if len(lbl) > 3 {
			labelsValid = false
			break
		}
	}

	if !labelsValid {
		newLabels := make([]string, len(vd.SceneParts.Files))
		seen := make(map[string]struct{})
		hasDuplicate := false

		for i, f := range vd.SceneParts.Files {
			fileIdInt, err := strconv.Atoi(f.Id)
			var lbl string
			if err == nil {
				lbl = fmt.Sprintf("%03d", fileIdInt%1000)
			} else {
				lbl = fmt.Sprintf("%03d", i+1)
			}

			if _, ok := seen[lbl]; ok {
				hasDuplicate = true
				break
			}
			seen[lbl] = struct{}{}
			newLabels[i] = lbl
		}

		if hasDuplicate {
			for i := range vd.SceneParts.Files {
				newLabels[i] = fmt.Sprintf("%03d", i+1)
			}
		}
		labels = newLabels
	}

	return labels
}
