package heresphere

import (
	"stash-vr/internal/library"
)

type indexDto struct {
	Access  int          `json:"access"`
	Library []libraryDto `json:"library"`
}

type libraryDto struct {
	Name string   `json:"name"`
	List []string `json:"list"`
}

func buildIndex(sections []library.Section, vds map[string]*library.VideoData, baseUrl string) (indexDto, error) {
	index := indexDto{Access: 1, Library: make([]libraryDto, 0, len(sections))}

	for _, section := range sections {
		l := libraryDto{
			Name: section.Name,
			List: make([]string, 0),
		}

		for _, sceneId := range section.Ids {
			if vd, ok := vds[sceneId]; ok {
				for _, f := range vd.SceneParts.Files {
					vid := library.MakeVirtualId(sceneId, f.Id)
					l.List = append(l.List, getVideoDataUrl(baseUrl, vid))
				}
			}
		}
		index.Library = append(index.Library, l)
	}
	return index, nil
}
