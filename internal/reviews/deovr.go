package reviews

import (
	"math"
	"time"
)

// RecordDeoVRTelemetry translates DeoVR's stateless pings into
// discrete PlayStart and PlayStop events for the review timeline.
func RecordDeoVRTelemetry(sceneId, label string, videoTimeSec float64, fileDuration float64) {
	label = getLabelOrDefault(label)
	mu.Lock()
	defer mu.Unlock()

	lastActiveSceneId = sceneId

	s, ok := activeSessions[sceneId]
	if !ok {
		return
	}

	now := time.Now()
	s.CurrentFileLabel = label
	s.FileDurations[label] = fileDuration

	// DeoVR usually pings every few seconds.
	// If it's been more than 10 seconds since the last ping, assume playback was stopped/paused.
	const pingTimeout = 10 * time.Second

	if s.IsPlaying && now.Sub(s.LastPlayRealTime) > pingTimeout {
		// End the previous playback interval at the time of the LAST known ping
		s.appendEvent(SessionEvent{
			Timestamp: s.LastPlayRealTime,
			Type:      EventPlayStop,
			FileLabel: label,
			VideoTime: s.LastPlayVideoTime,
		})
		s.IsPlaying = false
	}

	if !s.IsPlaying {
		// Start a new playback interval
		s.appendEvent(SessionEvent{
			Timestamp: now,
			Type:      EventPlayStart,
			FileLabel: label,
			VideoTime: videoTimeSec,
		})
		s.IsPlaying = true
	} else {
		// Already playing. Check if the user seeked (jumped time significantly)
		elapsedReal := now.Sub(s.LastPlayRealTime).Seconds()
		expectedVideoTime := s.LastPlayVideoTime + elapsedReal

		if math.Abs(videoTimeSec-expectedVideoTime) > 2.0 { // 2-second seek tolerance
			// User seeked. Stop the old interval, start a new one.
			s.appendEvent(SessionEvent{
				Timestamp: now,
				Type:      EventPlayStop,
				FileLabel: label,
				VideoTime: s.LastPlayVideoTime, // Close at the previous position
			})
			s.appendEvent(SessionEvent{
				Timestamp: now,
				Type:      EventPlayStart,
				FileLabel: label,
				VideoTime: videoTimeSec, // Re-open at the seeked position
			})
		}
	}

	s.LastPlayRealTime = now
	s.LastPlayVideoTime = videoTimeSec
}
