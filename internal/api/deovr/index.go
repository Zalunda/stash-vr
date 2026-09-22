package deovr

import (
	"fmt"
	"hash/fnv"
	"stash-vr/internal/library"
	"stash-vr/internal/stash"
	"stash-vr/internal/util"
)

type indexDto struct {
	Authorized string     `json:"authorized"`
	Scenes     []sceneDto `json:"scenes"`
}

type sceneDto struct {
	Name string           `json:"name"`
	List []previewDataDto `json:"list"`
}

type previewDataDto struct {
	Id           string  `json:"id"`
	ThumbnailUrl *string `json:"thumbnailUrl"`
	Title        string  `json:"title"`
	VideoLength  int     `json:"videoLength"`
	VideoUrl     string  `json:"video_url"`
}

func safeHashVirtualId(sceneId string, fileId string) string {
	h := fnv.New32a()
	if fileId == "" {
		h.Write([]byte(sceneId))
	} else {
		h.Write([]byte(sceneId + "_" + fileId))
	}
	return fmt.Sprintf("%d", h.Sum32()&0x7FFFFFFF)
}

func buildIndex(sections []library.Section, vds map[string]*library.VideoData, baseUrl string) (indexDto, error) {
	index := indexDto{Authorized: "1", Scenes: make([]sceneDto, 0, len(sections))}

	for _, section := range sections {
		s := sceneDto{
			Name: section.Name,
			List: make([]previewDataDto, 0),
		}

		for _, sceneId := range section.Ids {
			if vd, ok := vds[sceneId]; ok {

				// GetPlaybackItems handles the config toggle automatically
				for _, item := range vd.GetPlaybackItems() {
					title := vd.Title()
					if item.Label != "" {
						title = title + " [" + item.Label + "]"
					}

					videoUrl := getVideoDataUrl(baseUrl, sceneId)
					if item.FileId != "" {
						videoUrl += "?part=" + item.FileId
					}

					previewData := previewDataDto{
						Id:          safeHashVirtualId(sceneId, item.FileId),
						Title:       title,
						VideoLength: int(item.File.Duration),
						VideoUrl:    videoUrl,
					}

					if vd.SceneParts.Paths.Screenshot != nil {
						previewData.ThumbnailUrl = util.Ptr(stash.ApiKeyed(*vd.SceneParts.Paths.Screenshot))
					}

					s.List = append(s.List, previewData)
				}
			}
		}

		if len(s.List) > 0 {
			index.Scenes = append(index.Scenes, s)
		}
	}

	return index, nil
}
