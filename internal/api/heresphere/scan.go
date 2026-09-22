package heresphere

import (
	"context"
	"stash-vr/internal/library"
	"stash-vr/internal/util"
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
		for _, item := range vd.GetPlaybackItems() {
			scanData := videoDataToScanDataDto(ctx, vd, baseUrl, item)
			scanDoc.ScanData = append(scanDoc.ScanData, scanData)
		}
	}

	log.Ctx(ctx).Debug().Int("scenes", len(scanDoc.ScanData)).Msg("/scan")
	return &scanDoc, nil
}

func videoDataToScanDataDto(ctx context.Context, vd *library.VideoData, baseUrl string, item library.PlaybackItem) scanDataDto {
	id := library.MakeVirtualId(vd.Id(), item.FileId)

	title := vd.Title()
	if item.Label != "" {
		title = title + " [" + item.Label + "]"
	}

	// HereSphere expects Duration to be in Milliseconds
	durationMs := item.File.Duration * 1000

	scanData := scanDataDto{
		id:        id,
		Link:      getVideoDataUrl(baseUrl, id),
		Title:     title,
		DateAdded: vd.SceneParts.Created_at.Format(time.DateOnly),
		Duration:  durationMs,
		Tags:      getTags(vd, item.File),
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
