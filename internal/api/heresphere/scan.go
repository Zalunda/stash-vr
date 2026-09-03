package heresphere

import (
	"context"
	"slices"
	"stash-vr/internal/library"
	"stash-vr/internal/stash/gql"
	"stash-vr/internal/util"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type scanDocDto struct {
	ScanData []scanDataDto `json:"scanData"`
}

type scanDataDto struct {
	id           string
	Link         string   `json:"link"`
	Title        string   `json:"title"`
	DateReleased *string  `json:"dateReleased,omitempty"`
	DateAdded    string   `json:"dateAdded,omitempty"`
	Duration     float64  `json:"duration,omitempty"`
	Rating       *float32 `json:"rating,omitempty"`
	Favorites    *int     `json:"favorites,omitempty"`
	Comments     *int     `json:"comments,omitempty"`
	IsFavorite   *bool    `json:"isFavorite,omitempty"`
	Tags         []tagDto `json:"tags,omitempty"`
}

func buildScan(ctx context.Context, vds map[string]*library.VideoData, baseUrl string) (*scanDocDto, error) {
	scanDoc := scanDocDto{ScanData: make([]scanDataDto, 0)}

	for _, vd := range vds {
		// 1. Get alphabetically sorted files
		files := getSortedFiles(vd)

		// 2. Get the validated labels using our helper
		labels := vd.GetValidatedLabels()

		// 3. Create a virtual scan entry for every file in the scene
		for i, f := range files {
			scanData := videoDataToScanDataDto(ctx, vd, baseUrl, f.Id, f.Duration, labels[i])
			scanDoc.ScanData = append(scanDoc.ScanData, scanData)
		}
	}

	log.Ctx(ctx).Debug().Int("scenes", len(scanDoc.ScanData)).Msg("/scan")
	return &scanDoc, nil
}

func videoDataToScanDataDto(ctx context.Context, vd *library.VideoData, baseUrl string, fileId string, duration float64, label string) scanDataDto {
	// Generate the virtual ID: "123_456"
	id := library.MakeVirtualId(vd.Id(), fileId)

	// Append the label to the title if it's a multipart scene
	title := vd.Title()
	if len(vd.SceneParts.Files) > 1 && label != "" {
		title = title + " [" + label + "]"
	}

	scanData := scanDataDto{
		id:        id,
		Link:      getVideoDataUrl(baseUrl, id),
		Title:     title,
		DateAdded: vd.SceneParts.Created_at.Format(time.DateOnly),
		Duration:  duration, // Specific file duration
		Tags:      getTags(vd),
	}
	if vd.SceneParts.Date != nil {
		scanData.DateReleased = util.Ptr(util.NormalizeDate(*vd.SceneParts.Date))
	}
	if vd.SceneParts.Rating100 != nil {
		scanData.Rating = util.Ptr(float32(*vd.SceneParts.Rating100) / 20.0)
	}
	if vd.SceneParts.O_counter != nil {
		scanData.Favorites = vd.SceneParts.O_counter
	}
	if vd.SceneParts.Play_count != nil {
		scanData.Comments = util.Ptr(*vd.SceneParts.Play_count)
	}
	if isFavorite(vd) {
		scanData.IsFavorite = util.Ptr(true)
	}
	return scanData
}

// --- HELPER FUNCTIONS ---

func getSortedFiles(vd *library.VideoData) []*gql.ScenePartsFilesVideoFile {
	files := make([]*gql.ScenePartsFilesVideoFile, len(vd.SceneParts.Files))
	copy(files, vd.SceneParts.Files)
	slices.SortFunc(files, func(a, b *gql.ScenePartsFilesVideoFile) int {
		return strings.Compare(a.Basename, b.Basename)
	})
	return files
}
