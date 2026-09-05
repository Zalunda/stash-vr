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

// DeoVR requires a numeric ID that fits within a 32-bit signed int (Max: 2147483647)
func hashVirtualId(vid string) string {
	h := fnv.New32a()
	h.Write([]byte(vid))
	// Masking with 0x7FFFFFFF strips the sign bit, preventing overflow crashes in Unity
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
				sortedFiles, labels := vd.GetFilesSortedByLabel()

				for _, f := range sortedFiles {
					vid := library.MakeVirtualId(sceneId, f.Id)

					title := vd.Title()
					if len(sortedFiles) > 1 {
						title = title + "-" + labels[f.Id]
					}

					previewData := previewDataDto{
						Id:          hashVirtualId(vid),
						Title:       title,
						VideoLength: int(f.Duration),
						VideoUrl:    getVideoDataUrl(baseUrl, vid),
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
