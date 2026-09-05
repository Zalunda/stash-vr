package deovr

import (
	"fmt"
	"hash/fnv"
	"net/url"
	"stash-vr/internal/api/heatmap"
	"stash-vr/internal/api/internal"
	"stash-vr/internal/library"
	"stash-vr/internal/stash"
	"stash-vr/internal/util"
	"strings"
)

type videoDataDto struct {
	Authorized     string  `json:"authorized"`
	FullAccess     bool    `json:"fullAccess"`
	Title          string  `json:"title"`
	Id             string  `json:"id"`
	VideoLength    int     `json:"videoLength"`
	Is3d           bool    `json:"is3d"`
	ScreenType     string  `json:"screenType"`
	StereoMode     string  `json:"stereoMode"`
	SkipIntro      int     `json:"skipIntro"`
	VideoThumbnail *string `json:"videoThumbnail,omitempty"`
	VideoPreview   *string `json:"videoPreview,omitempty"`
	ThumbnailUrl   *string `json:"thumbnailUrl"`

	TimeStamps []timeStampDto `json:"timeStamps,omitempty"`

	Encodings []encodingDto `json:"encodings"`
}

type timeStampDto struct {
	Ts   int    `json:"ts"`
	Name string `json:"name"`
}

type encodingDto struct {
	Name         string           `json:"name"`
	VideoSources []videoSourceDto `json:"videoSources"`
}

type videoSourceDto struct {
	Resolution int    `json:"resolution"`
	Url        string `json:"url"`
}

func hashVirtualIdVideoData(vid string) string {
	h := fnv.New32a()
	h.Write([]byte(vid))
	return fmt.Sprintf("%d", h.Sum32()&0x7FFFFFFF)
}

func buildVideoData(vd *library.VideoData, baseUrl string, label string, fileId string) (*videoDataDto, error) {
	vid := library.MakeVirtualId(vd.Id(), fileId)

	if len(vd.SceneParts.Files) == 0 {
		return nil, fmt.Errorf("scene %s has no files", vd.Id())
	}

	title := vd.Title()
	if label != "" && len(vd.SceneParts.Files) > 1 {
		title = title + " [" + label + "]"
	}

	duration := vd.SceneParts.Files[0].Duration
	if fileId != "" {
		for _, f := range vd.SceneParts.Files {
			if f.Id == fileId {
				duration = f.Duration
				break
			}
		}
	}

	dto := videoDataDto{
		Authorized:  "1",
		FullAccess:  true,
		Title:       title,
		Id:          hashVirtualIdVideoData(vid),
		VideoLength: int(duration),
		SkipIntro:   0,
	}

	if vd.SceneParts.Paths.Screenshot != nil {
		if vd.SceneParts.Interactive && vd.SceneParts.Paths.Interactive_heatmap != nil {
			dto.ThumbnailUrl = util.Ptr(heatmap.GetCoverUrl(baseUrl, vd.Id()))
		} else {
			dto.ThumbnailUrl = util.Ptr(stash.ApiKeyed(*vd.SceneParts.Paths.Screenshot))
		}
	}

	if vd.SceneParts.Paths.Preview != nil {
		dto.VideoPreview = util.Ptr(stash.ApiKeyed(*vd.SceneParts.Paths.Preview))
	}

	// Pass baseUrl down
	setStreamSources(vd, &dto, fileId, baseUrl)
	setMarkers(vd, &dto)
	set3DFormat(vd, &dto)

	return &dto, nil
}

func setStreamSources(vd *library.VideoData, dto *videoDataDto, fileId string, baseUrl string) {
	streams := []stash.Stream{stash.GetTranscodingStream(vd.SceneParts), stash.GetDirectStream(vd.SceneParts)}
	dto.Encodings = make([]encodingDto, len(streams))
	for i, stream := range streams {
		dto.Encodings[i] = encodingDto{
			Name:         stream.Name,
			VideoSources: make([]videoSourceDto, len(stream.Sources)),
		}
		for j, source := range stream.Sources {

			// Point DeoVR to our interceptor endpoint, hiding the real URL in a query parameter
			redirectUrl := fmt.Sprintf("%s/deovr/play/%s?url=%s", baseUrl, vd.Id(), url.QueryEscape(source.Url))
			if fileId != "" {
				redirectUrl += "&part=" + fileId
			}

			dto.Encodings[i].VideoSources[j] = videoSourceDto{
				Resolution: source.Resolution,
				Url:        redirectUrl,
			}
		}
	}
}

func setMarkers(vd *library.VideoData, dto *videoDataDto) {
	for _, sm := range vd.SceneParts.Scene_markers {
		sb := strings.Builder{}
		sb.WriteString(sm.Primary_tag.Name)
		if sm.Title != "" {
			sb.WriteString(":")
			sb.WriteString(sm.Title)
		}
		ts := timeStampDto{
			Ts:   int(sm.Seconds),
			Name: sb.String(),
		}
		dto.TimeStamps = append(dto.TimeStamps, ts)
	}
}

func set3DFormat(vd *library.VideoData, dto *videoDataDto) {
	for _, t := range vd.SceneParts.Tags {
		switch {
		case util.StrSliceEquals(t.Name, t.Aliases, internal.TagVR_DOME):
			dto.Is3d = true
			dto.ScreenType = "dome"
			dto.StereoMode = "sbs"
			continue
		case util.StrSliceEquals(t.Name, t.Aliases, internal.TagVR_SPHERE):
			dto.Is3d = true
			dto.ScreenType = "sphere"
			dto.StereoMode = "sbs"
			continue
		case util.StrSliceEquals(t.Name, t.Aliases, internal.TagVR_FISHEYE):
			dto.Is3d = true
			dto.ScreenType = "fisheye"
			dto.StereoMode = "sbs"
			continue
		case util.StrSliceEquals(t.Name, t.Aliases, internal.TagVR_MKX200):
			dto.Is3d = true
			dto.ScreenType = "mkx200"
			dto.StereoMode = "sbs"
			continue
		case util.StrSliceEquals(t.Name, t.Aliases, internal.TagVR_RF52):
			dto.Is3d = true
			dto.ScreenType = "rf52"
			dto.StereoMode = "cuv"
			continue
		case util.StrSliceEquals(t.Name, t.Aliases, internal.TagVR_SBS):
			dto.Is3d = true
			dto.StereoMode = "sbs"
			continue
		case util.StrSliceEquals(t.Name, t.Aliases, internal.TagVR_TB):
			dto.Is3d = true
			dto.StereoMode = "tb"
			continue
		}
	}
}
