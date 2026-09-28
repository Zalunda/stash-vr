package reviews

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"stash-vr/internal/config"
	"stash-vr/internal/library"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	EventPlayStart   = "PlayStart"
	EventPlayStop    = "PlayStop"
	EventNoteAdded   = "NoteAdded"
	EventNoteChanged = "NoteChanged"
	EventNoteRemoved = "NoteRemoved"
	EventFlagsSync   = "FlagsSync"
)

type TimeInterval struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

type TimedNote struct {
	FileLabel  string   `json:"fileLabel,omitempty"`
	ConfigName string   `json:"configName,omitempty"`
	StartTime  float64  `json:"startTime"`
	EndTime    *float64 `json:"endTime,omitempty"`
	Note       string   `json:"note,omitempty"`
	Sentiment  string   `json:"sentiment,omitempty"`
}

type SessionEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Injected  bool      `json:"injected,omitempty"` // Marks synthesized events

	// Play Events
	FileLabel string  `json:"fileLabel,omitempty"`
	VideoTime float64 `json:"videoTime,omitempty"`

	// Note Events
	NoteName   string   `json:"noteName,omitempty"`
	ConfigName string   `json:"configName,omitempty"`
	OldStart   *float64 `json:"oldStart,omitempty"`
	OldEnd     *float64 `json:"oldEnd,omitempty"`
	NewStart   *float64 `json:"newStart,omitempty"`
	NewEnd     *float64 `json:"newEnd,omitempty"`

	// Flag Events
	Flags map[string][]string `json:"flags,omitempty"`
}

type DerivedState struct {
	Played      map[string][]TimeInterval
	TimedNotes  map[string][]TimedNote
	GlobalFlags map[string][]string
}

type Session struct {
	mu                 sync.Mutex
	SceneId            string
	SceneTitle         string
	SafeTitle          string
	RootConfigFileBase string
	MatchedConfig      Config
	VisualNotes        []VisualNoteDef
	FileDurations      map[string]float64
	Events             []SessionEvent

	IsPlaying         bool
	LastPlayRealTime  time.Time
	LastPlayVideoTime float64
	CurrentFileLabel  string

	saveCh chan struct{}
}

var (
	globalMu          sync.Mutex
	activeSessions    = make(map[string]*Session)
	lastActiveSceneId string
)

func init() {
	// Background garbage collector to prevent memory leaks from abandoned sessions
	go func() {
		for {
			time.Sleep(15 * time.Minute)
			now := time.Now()

			globalMu.Lock()
			for id, s := range activeSessions {
				s.mu.Lock()
				// If not playing, and haven't touched it in 2 hours, drop it from RAM.
				if !s.IsPlaying && now.Sub(s.LastPlayRealTime) > 2*time.Hour {
					close(s.saveCh)
					delete(activeSessions, id)
				}
				s.mu.Unlock()
			}
			globalMu.Unlock()
		}
	}()
}

func getSafeTitle(title string) string {
	safe := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/:*?"<>|`, r) {
			return '_'
		}
		return r
	}, title)
	if safe == "" {
		return "Unknown_Scene"
	}
	return safe
}

func EnsureSession(vd *library.VideoData) {
	sceneId := vd.SceneId()
	title := vd.Title()

	globalMu.Lock()
	defer globalMu.Unlock()

	lastActiveSceneId = sceneId

	if _, ok := activeSessions[sceneId]; !ok {
		var tags []string
		for _, t := range vd.SceneParts.Tags {
			tags = append(tags, t.Name)
		}

		matchedConfigs := GetMatchedConfigs(tags)
		var mergedConfig Config
		mergedConfig.GlobalFlags = make([]GlobalFlagDef, 0)

		for _, c := range matchedConfigs {
			configName := c.Name
			if configName == "" {
				configName = c.Id
			}
			for _, flag := range c.GlobalFlags {
				flag.Category = fmt.Sprintf("%s: %s", configName, flag.Category)
				mergedConfig.GlobalFlags = append(mergedConfig.GlobalFlags, flag)
			}
		}
		mergedConfig.GlobalFlags = removeDuplicateGlobalFlags(mergedConfig.GlobalFlags)

		var rootConfigFileBase string
		if len(matchedConfigs) > 0 {
			// Grab the filename base from the matched root config
			rootConfigFileBase = matchedConfigs[len(matchedConfigs)-1].FileBaseName
		} else {
			rootConfigFileBase = "default.review"
		}

		safeTitle := getSafeTitle(title)
		s := &Session{
			SceneId:            sceneId,
			SceneTitle:         title,
			SafeTitle:          safeTitle,
			RootConfigFileBase: rootConfigFileBase,
			MatchedConfig:      mergedConfig,
			VisualNotes:        GetVisualNotes(tags),
			FileDurations:      make(map[string]float64),
			saveCh:             make(chan struct{}, 1),
		}
		rawPath := filepath.Join("review-notes", safeTitle+".raw.json")
		if b, err := os.ReadFile(rawPath); err == nil {
			json.Unmarshal(b, &s.Events)
		}

		activeSessions[sceneId] = s

		// Start async save worker
		go s.saveWorker()
	}
}

func GetSession(sceneId string) (*Session, bool) {
	globalMu.Lock()
	defer globalMu.Unlock()
	s, ok := activeSessions[sceneId]
	return s, ok
}

func GetLastActiveSceneId() string {
	globalMu.Lock()
	defer globalMu.Unlock()
	return lastActiveSceneId
}

// --- ASYNC SAVING ---

func (s *Session) saveWorker() {
	for range s.saveCh {
		s.mu.Lock()
		state := s.deriveInternal()

		eventsCopy := make([]SessionEvent, len(s.Events))
		copy(eventsCopy, s.Events)

		safeTitle := s.SafeTitle
		sceneTitle := s.SceneTitle
		s.mu.Unlock()

		// Check if we should write to disk
		hasReviewContent := false
		for _, ev := range eventsCopy {
			if ev.Type != EventPlayStart && ev.Type != EventPlayStop {
				hasReviewContent = true
				break
			}
		}

		if !hasReviewContent && !config.Application().AlwaysWriteReviewNotes {
			continue // Skip disk I/O entirely, it's just play/stop data!
		}

		// Disk I/O performed outside of the Session lock!
		rawPath := filepath.Join("review-notes", safeTitle+".raw.json")
		b, err := json.MarshalIndent(eventsCopy, "", "  ")
		if err != nil {
			log.Ctx(context.Background()).Warn().Err(err).Msg("failed to marshal events")
		} else if err := os.WriteFile(rawPath, b, 0644); err != nil {
			log.Ctx(context.Background()).Warn().Err(err).Msg("failed to write raw events file")
		}

		saveDerivedStateToFile(state, safeTitle, sceneTitle)
	}
}

// triggers an async write if one isn't already queued. Must be called inside s.mu.Lock()
func (s *Session) triggerSave() {
	select {
	case s.saveCh <- struct{}{}:
	default: // already queued
	}
}

// --- EVENT RECORDERS ---

// Note: Ensure s.mu.Lock() is held before calling this.
func (s *Session) appendEvent(ev SessionEvent) {
	s.Events = append(s.Events, ev)
	s.triggerSave()
}

func RecordPlayStart(sceneId, label string, videoTimeSec float64, fileDuration float64) {
	label = getLabelOrDefault(label)

	globalMu.Lock()
	lastActiveSceneId = sceneId
	s, ok := activeSessions[sceneId]
	globalMu.Unlock()

	if !ok {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	s.CurrentFileLabel = label
	s.FileDurations[label] = fileDuration

	if s.IsPlaying {
		elapsed := now.Sub(s.LastPlayRealTime).Seconds()
		expectedVideoTime := s.LastPlayVideoTime + elapsed

		// If the newly reported time is off by more than 2 seconds, it's a seek.
		if math.Abs(videoTimeSec-expectedVideoTime) > 2.0 {
			if dur, ok := s.FileDurations[label]; ok && expectedVideoTime > dur {
				expectedVideoTime = dur
			}
			// INJECT the missing PlayStop.
			s.appendEvent(SessionEvent{
				Timestamp: now, Type: EventPlayStop, FileLabel: label,
				VideoTime: expectedVideoTime, Injected: true,
			})
		} else {
			// Duplicate play event at roughly the same time, just update trackers and ignore.
			s.LastPlayRealTime = now
			s.LastPlayVideoTime = videoTimeSec
			return
		}
	}

	s.IsPlaying = true
	s.LastPlayRealTime = now
	s.LastPlayVideoTime = videoTimeSec
	s.appendEvent(SessionEvent{Timestamp: now, Type: EventPlayStart, FileLabel: label, VideoTime: videoTimeSec})
}

func RecordPlayStop(sceneId, label string, videoTimeSec float64) {
	label = getLabelOrDefault(label)

	globalMu.Lock()
	s, ok := activeSessions[sceneId]
	globalMu.Unlock()

	if !ok {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if s.IsPlaying {
		elapsed := now.Sub(s.LastPlayRealTime).Seconds()
		expectedVideoTime := s.LastPlayVideoTime + elapsed

		if math.Abs(videoTimeSec-expectedVideoTime) > 2.0 {
			videoTimeSec = expectedVideoTime
		}
		if dur, ok := s.FileDurations[label]; ok && videoTimeSec > dur {
			videoTimeSec = dur
		}
	}

	s.appendEvent(SessionEvent{Timestamp: now, Type: EventPlayStop, FileLabel: label, VideoTime: videoTimeSec})
	s.IsPlaying = false
}

func RecordFlagsSync(sceneId string, flags map[string][]string) {
	globalMu.Lock()
	s, ok := activeSessions[sceneId]
	globalMu.Unlock()

	if !ok {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.appendEvent(SessionEvent{Timestamp: time.Now(), Type: EventFlagsSync, Flags: flags})
}

func AddUINote(sceneId, noteName, configName, action string) (int, bool) {
	globalMu.Lock()
	s, ok := activeSessions[sceneId]
	globalMu.Unlock()

	if !ok {
		return 0, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	label := getLabelOrDefault(s.CurrentFileLabel)

	vidTime := s.LastPlayVideoTime
	now := time.Now()
	if s.IsPlaying {
		vidTime += now.Sub(s.LastPlayRealTime).Seconds()
	}
	if dur, ok := s.FileDurations[label]; ok && vidTime > dur {
		vidTime = dur
	}
	if vidTime < 0 {
		vidTime = 0
	}

	isDuplicate := false
	success := true

	if action == "start" || action == "point" {
		for i := len(s.Events) - 1; i >= 0; i-- {
			ev := s.Events[i]
			if ev.Type == EventNoteAdded && ev.FileLabel == label && ev.NoteName == noteName {
				if ev.NewStart != nil && math.Abs(*ev.NewStart-vidTime) < 1.0 {
					isDuplicate = true
					break
				}
			}
		}

		if !isDuplicate {
			s.appendEvent(SessionEvent{
				Timestamp:  now,
				Type:       EventNoteAdded,
				FileLabel:  label,
				NoteName:   noteName,
				ConfigName: configName,
				NewStart:   &vidTime,
				NewEnd:     nil,
			})
		}
	} else if action == "end" {
		var oldStart *float64
		var oldEnd *float64
		for i := len(s.Events) - 1; i >= 0; i-- {
			ev := s.Events[i]
			if (ev.Type == EventNoteAdded || ev.Type == EventNoteChanged) && ev.FileLabel == label && ev.NoteName == noteName {
				oldStart = ev.NewStart
				oldEnd = ev.NewEnd
				break
			}
		}

		if oldStart != nil {
			// Ensure the user didn't scrub backwards before closing the note
			if vidTime < *oldStart {
				success = false
			} else {
				s.appendEvent(SessionEvent{
					Timestamp:  now,
					Type:       EventNoteChanged,
					FileLabel:  label,
					NoteName:   noteName,
					ConfigName: configName,
					OldStart:   oldStart,
					OldEnd:     oldEnd,
					NewStart:   oldStart,
					NewEnd:     &vidTime,
				})
			}
		} else {
			success = false // Mark as failed if we couldn't find a note to close
		}
	}

	state := s.deriveInternal()
	count := 0
	for _, notes := range state.TimedNotes {
		for _, n := range notes {
			if n.Note == noteName {
				count++
			}
		}
	}
	return count, success
}

// --- STATE DERIVATION ---

// Public thread-safe method
func (s *Session) Derive() DerivedState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deriveInternal()
}

// Internal lock-free method
func (s *Session) deriveInternal() DerivedState {
	state := DerivedState{
		Played:      make(map[string][]TimeInterval),
		TimedNotes:  make(map[string][]TimedNote),
		GlobalFlags: make(map[string][]string),
	}

	playingStart := make(map[string]float64)

	for _, ev := range s.Events {
		switch ev.Type {
		case EventPlayStart:
			playingStart[ev.FileLabel] = ev.VideoTime
		case EventPlayStop:
			if start, ok := playingStart[ev.FileLabel]; ok {
				if ev.VideoTime >= start {
					// Normal forward interval
					state.Played[ev.FileLabel] = append(state.Played[ev.FileLabel], TimeInterval{Start: start, End: ev.VideoTime})
				} else {
					// Time went backward (corrupted stop event).
					if start-ev.VideoTime < 2.0 {
						state.Played[ev.FileLabel] = append(state.Played[ev.FileLabel], TimeInterval{Start: start, End: start + 1.0})
					}
				}
				delete(playingStart, ev.FileLabel)
			}
		case EventFlagsSync:
			state.GlobalFlags = ev.Flags
		case EventNoteAdded:
			state.TimedNotes[ev.FileLabel] = append(state.TimedNotes[ev.FileLabel], TimedNote{
				FileLabel: ev.FileLabel, ConfigName: ev.ConfigName,
				StartTime: *ev.NewStart, EndTime: ev.NewEnd, Note: ev.NoteName,
			})
		case EventNoteRemoved:
			var filtered []TimedNote
			for _, n := range state.TimedNotes[ev.FileLabel] {
				if !(n.Note == ev.NoteName && floatEquals(n.StartTime, *ev.OldStart) && floatPtrEquals(n.EndTime, ev.OldEnd)) {
					filtered = append(filtered, n)
				}
			}
			state.TimedNotes[ev.FileLabel] = filtered
		case EventNoteChanged:
			notes := state.TimedNotes[ev.FileLabel]
			// Iterate backwards to update the MOST RECENTLY dropped pin
			for i := len(notes) - 1; i >= 0; i-- {
				n := notes[i]
				if n.Note == ev.NoteName && floatEquals(n.StartTime, *ev.OldStart) && floatPtrEquals(n.EndTime, ev.OldEnd) {
					state.TimedNotes[ev.FileLabel][i].StartTime = *ev.NewStart
					state.TimedNotes[ev.FileLabel][i].EndTime = ev.NewEnd
					break
				}
			}
		}
	}

	for lbl, intervals := range state.Played {
		state.Played[lbl] = mergeIntervals(intervals)
	}

	for lbl, notes := range state.TimedNotes {
		sort.Slice(notes, func(i, j int) bool {
			return notes[i].StartTime < notes[j].StartTime
		})
		state.TimedNotes[lbl] = notes
	}

	return state
}

// --- FILE WRITER ---

func formatTimeSec(sec float64) string {
	h := int(sec) / 3600
	m := (int(sec) % 3600) / 60
	sFloat := math.Mod(sec, 60.0)

	// %04.1f guarantees 4 characters total (e.g., "05.4") with 1 decimal place
	return fmt.Sprintf("%02d:%02d:%04.1f", h, m, sFloat)
}

func formatDurationSec(sec float64) string {
	h := int(sec) / 3600
	m := (int(sec) % 3600) / 60
	sFloat := math.Mod(sec, 60.0)

	if h > 0 {
		return fmt.Sprintf("%d:%02d:%04.1f", h, m, sFloat)
	}
	return fmt.Sprintf("%d:%04.1f", m, sFloat)
}

func saveDerivedStateToFile(state DerivedState, safeTitle string, sceneTitle string) {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("Scene: %s\n\n", sceneTitle))

	if len(state.Played) > 0 {
		b.WriteString("Played:\n")
		var labels []string
		for lbl := range state.Played {
			labels = append(labels, lbl)
		}
		sort.Strings(labels)

		for _, lbl := range labels {
			intervals := state.Played[lbl]

			var lastSolidEnd float64 = -1.0
			var hasMicroPlays bool = false
			var solidCount int = 0

			for _, inv := range intervals {
				duration := inv.End - inv.Start

				// Filter out micro-plays (scrubbing/skipping)
				if duration <= 3.0 {
					hasMicroPlays = true
					continue
				}

				solidCount++
				gapText := ""

				if lastSolidEnd != -1.0 {
					gap := inv.Start - lastSolidEnd
					if gap > 2.0 {
						// If they scrubbed through this gap, it's a skip. If they just clicked ahead, it's a jump.
						if hasMicroPlays {
							gapText = fmt.Sprintf(" SKIPPED %s", formatDurationSec(gap))
						} else {
							gapText = fmt.Sprintf(" JUMPED %s", formatDurationSec(gap))
						}
					}
				} else if inv.Start > 5.0 {
					// Gap before the very first play block starts
					if hasMicroPlays {
						gapText = fmt.Sprintf(" SKIPPED %s", formatDurationSec(inv.Start))
					} else {
						gapText = fmt.Sprintf(" JUMPED %s", formatDurationSec(inv.Start))
					}
				}

				b.WriteString(fmt.Sprintf("%s: %s-%s%s\n", lbl, formatTimeSec(inv.Start), formatTimeSec(inv.End), gapText))

				lastSolidEnd = inv.End
				hasMicroPlays = false
			}

			// If they opened the file and ONLY fast forwarded through it without ever stopping
			if solidCount == 0 && hasMicroPlays {
				b.WriteString(fmt.Sprintf("%s: SCRUBBED ONLY\n", lbl))
			}
		}
		b.WriteString("\n")
	}

	var allNotes []TimedNote
	for _, notes := range state.TimedNotes {
		allNotes = append(allNotes, notes...)
	}

	if len(allNotes) > 0 {
		b.WriteString("========================================\n")
		b.WriteString("           TIMELINE LOG\n")
		b.WriteString("========================================\n")

		sort.Slice(allNotes, func(i, j int) bool {
			if allNotes[i].FileLabel == allNotes[j].FileLabel {
				return allNotes[i].StartTime < allNotes[j].StartTime
			}
			return allNotes[i].FileLabel < allNotes[j].FileLabel
		})

		for _, n := range allNotes {
			timeStr := formatTimeSec(n.StartTime)
			if n.EndTime != nil && *n.EndTime > n.StartTime {
				timeStr = fmt.Sprintf("%s->%s", timeStr, formatTimeSec(*n.EndTime))
			}
			b.WriteString(fmt.Sprintf("%s: %-23s %s\n", n.FileLabel, timeStr, n.Note))
		}
		b.WriteString("\n")

		b.WriteString("========================================\n")
		b.WriteString("         FEEDBACK SUMMARY\n")
		b.WriteString("========================================\n")

		summary := make(map[string]map[string][]string)
		for _, n := range allNotes {
			sent := "General"
			if n.Sentiment != "" {
				sent = n.Sentiment
			}

			if summary[sent] == nil {
				summary[sent] = make(map[string][]string)
			}

			timeStr := formatTimeSec(n.StartTime)
			if n.EndTime != nil && *n.EndTime > n.StartTime {
				timeStr = fmt.Sprintf("%s->%s", timeStr, formatTimeSec(*n.EndTime))
			}
			summary[sent][n.Note] = append(summary[sent][n.Note], fmt.Sprintf("  - %s: %s", n.FileLabel, timeStr))
		}

		printCategory(&b, summary, "issue", "--- ISSUES & CRITIQUES ---")
		printCategory(&b, summary, "positive", "--- POSITIVES & HIGHLIGHTS ---")
		printCategory(&b, summary, "General", "--- OTHER NOTES ---")
	}

	hasFlags := false
	for cat, flags := range state.GlobalFlags {
		if len(flags) > 0 {
			if !hasFlags {
				b.WriteString("========================================\n")
				b.WriteString("           GLOBAL FLAGS\n")
				b.WriteString("========================================\n")
				hasFlags = true
			}
			b.WriteString(fmt.Sprintf("[%s]\n", cat))
			for _, flag := range flags {
				b.WriteString(fmt.Sprintf("- %s\n", flag))
			}
		}
	}

	path := filepath.Join("review-notes", safeTitle+"-ReviewNote.txt")
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		log.Ctx(context.Background()).Warn().Err(err).Msg("failed to write txt review file")
	}
}

func printCategory(b *strings.Builder, summary map[string]map[string][]string, sentimentKey, title string) {
	if categoryMap, ok := summary[sentimentKey]; ok && len(categoryMap) > 0 {
		b.WriteString(title + "\n")

		var names []string
		for k := range categoryMap {
			names = append(names, k)
		}
		sort.Strings(names)

		for _, name := range names {
			b.WriteString(fmt.Sprintf("%s:\n", name))
			for _, instance := range categoryMap[name] {
				b.WriteString(fmt.Sprintf("%s\n", instance))
			}
		}
		b.WriteString("\n")
	}
}

// Utils
func getLabelOrDefault(label string) string {
	if label == "" {
		return "Main"
	}
	return label
}
func mergeIntervals(intervals []TimeInterval) []TimeInterval {
	if len(intervals) == 0 {
		return nil
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].Start < intervals[j].Start })
	merged := []TimeInterval{intervals[0]}
	for _, curr := range intervals[1:] {
		last := &merged[len(merged)-1]
		if curr.Start <= last.End+2.0 {
			if curr.End > last.End {
				last.End = curr.End
			}
		} else {
			merged = append(merged, curr)
		}
	}
	return merged
}

func floatEquals(a, b float64) bool { return math.Abs(a-b) < 0.05 }
func floatPtrEquals(a, b *float64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return floatEquals(*a, *b)
}
