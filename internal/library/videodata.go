package library

import (
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
