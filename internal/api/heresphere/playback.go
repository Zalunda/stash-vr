package heresphere

import (
	"context"
	"stash-vr/internal/library"
	"time"

	"github.com/rs/zerolog/log"
)

func newPlayback(vd *library.VideoData, videoStartTimeSec float64) *playbackState {
	realId, fileId := library.ParseVirtualId(vd.Id())

	// FIX: If it's a single-file scene, get the actual file ID so the Review Hub can map it
	if fileId == "" && len(vd.SceneParts.Files) > 0 {
		fileId = vd.SceneParts.Files[0].Id
	}

	return &playbackState{
		videoId:        vd.Id(),
		sceneId:        realId,
		fileId:         fileId,
		videoDuration:  vd.SceneParts.Files[0].Duration,
		lastPlayTime:   time.Now(),
		videoStartTime: videoStartTimeSec,
		isPlaying:      true,
	}
}

func (ps *playbackState) handleStop(ctx context.Context, libraryService *library.Service, minPlayFraction *float64, stopTimeSec float64) {
	if ps.isPlaying {
		currentPlayDuration := time.Since(ps.lastPlayTime)
		ps.accumulatedPlayTime += currentPlayDuration

		if !ps.thresholdReached && minPlayFraction != nil && ps.accumulatedPlayTime.Seconds() >= ps.videoDuration*(*minPlayFraction) {
			ps.thresholdReached = true
			log.Ctx(ctx).Debug().Str("total play time", ps.accumulatedPlayTime.Round(time.Second).String()).Msg("Incrementing play count")
			err := libraryService.IncrementPlayCount(ctx, ps.videoId)
			if err != nil {
				log.Ctx(ctx).Warn().Err(err).Msg("Failed to increment play count")
			}
		}

		log.Ctx(ctx).Debug().Str("duration", currentPlayDuration.Round(time.Second).String()).Msg("Adding play duration")
		err := libraryService.AddPlayDuration(ctx, ps.videoId, currentPlayDuration)
		if err != nil {
			log.Ctx(ctx).Warn().Err(err).Msg("Failed to add play duration")
		}

		// FIX: If HereSphere omitted the time on evClose (0), calculate it using real-world elapsed time
		if stopTimeSec <= 0 {
			stopTimeSec = ps.videoStartTime + currentPlayDuration.Seconds()
		}

		// Cap at video duration just in case
		if stopTimeSec > ps.videoDuration {
			stopTimeSec = ps.videoDuration
		}

		if stopTimeSec > ps.videoStartTime {
			libraryService.AddWatchInterval(ps.sceneId, ps.fileId, ps.videoStartTime, stopTimeSec)
		}
	}
	ps.isPlaying = false
}

func (ps *playbackState) handleResume(resumeTimeSec float64) {
	if !ps.isPlaying {
		ps.lastPlayTime = time.Now()
		ps.videoStartTime = resumeTimeSec
	}
	ps.isPlaying = true
}

type playbackState struct {
	videoId       string
	sceneId       string
	fileId        string
	videoDuration float64

	accumulatedPlayTime time.Duration
	thresholdReached    bool
	lastPlayTime        time.Time
	videoStartTime      float64
	isPlaying           bool
}
