package reviews

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
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
	VideoTime float64 `json:"videoTime,omitempty"` // In Seconds

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
	SceneId       string
	SceneTitle    string
	SafeTitle     string
	MatchedConfig Config
	FileDurations map[string]float64
	Events        []SessionEvent

	LastCursors map[string]TimedNote

	// Playback State for detecting Seek gaps
	IsPlaying         bool
	LastPlayRealTime  time.Time
	LastPlayVideoTime float64
}

var (
	mu             sync.Mutex
	activeSessions = make(map[string]*Session)
)

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

func EnsureSession(sceneId, title string, tags []string) {
	mu.Lock()
	defer mu.Unlock()

	if _, ok := activeSessions[sceneId]; !ok {
		matchedConfigs := GetMatchedConfigs(tags)
		var mergedConfig Config
		mergedConfig.GlobalFlags = make([]GlobalFlagDef, 0)

		for _, c := range matchedConfigs {
			mergedConfig.GlobalFlags = append(mergedConfig.GlobalFlags, c.GlobalFlags...)
		}
		mergedConfig.GlobalFlags = removeDuplicateGlobalFlags(mergedConfig.GlobalFlags)

		safeTitle := getSafeTitle(title)
		s := &Session{
			SceneId:       sceneId,
			SceneTitle:    title,
			SafeTitle:     safeTitle,
			MatchedConfig: mergedConfig,
			FileDurations: make(map[string]float64),
			LastCursors:   make(map[string]TimedNote),
		}

		rawPath := filepath.Join("review-notes", safeTitle+".raw.json")
		if b, err := os.ReadFile(rawPath); err == nil {
			json.Unmarshal(b, &s.Events)
		}

		activeSessions[sceneId] = s
	}
}

func GetSession(sceneId string) (*Session, bool) {
	mu.Lock()
	defer mu.Unlock()
	s, ok := activeSessions[sceneId]
	return s, ok
}

// --- EVENT RECORDERS ---

func (s *Session) appendEvent(ev SessionEvent) {
	s.Events = append(s.Events, ev)
	rawPath := filepath.Join("review-notes", s.SafeTitle+".raw.json")
	b, _ := json.MarshalIndent(s.Events, "", "  ")
	os.WriteFile(rawPath, b, 0644)
	s.SaveToFile()
}

func RecordPlayStart(sceneId, label string, videoTimeSec float64) {
	mu.Lock()
	defer mu.Unlock()
	if s, ok := activeSessions[sceneId]; ok {
		now := time.Now()

		if s.IsPlaying {
			elapsed := now.Sub(s.LastPlayRealTime).Seconds()
			expectedVideoTime := s.LastPlayVideoTime + elapsed

			// If the newly reported time is off by more than 2 seconds, it's a seek!
			if math.Abs(videoTimeSec-expectedVideoTime) > 2.0 {
				if dur, ok := s.FileDurations[label]; ok && expectedVideoTime > dur {
					expectedVideoTime = dur
				}
				// INJECT the missing PlayStop!
				s.appendEvent(SessionEvent{
					Timestamp: now, Type: EventPlayStop, FileLabel: label,
					VideoTime: expectedVideoTime, Injected: true, // Tagged here!
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
}

func RecordPlayStop(sceneId, label string, videoTimeSec float64) {
	mu.Lock()
	defer mu.Unlock()
	if s, ok := activeSessions[sceneId]; ok {
		now := time.Now()

		if s.IsPlaying {
			elapsed := now.Sub(s.LastPlayRealTime).Seconds()
			expectedVideoTime := s.LastPlayVideoTime + elapsed

			// HereSphere sends ~0 on Close/Stop.
			// If the reported time deviates significantly from reality, trust our math!
			if math.Abs(videoTimeSec-expectedVideoTime) > 2.0 {
				videoTimeSec = expectedVideoTime
			}

			// Cap to duration just in case
			if dur, ok := s.FileDurations[label]; ok && videoTimeSec > dur {
				videoTimeSec = dur
			}
		}

		s.appendEvent(SessionEvent{Timestamp: now, Type: EventPlayStop, FileLabel: label, VideoTime: videoTimeSec})
		s.IsPlaying = false
	}
}

func RecordFlagsSync(sceneId string, flags map[string][]string) {
	mu.Lock()
	defer mu.Unlock()
	if s, ok := activeSessions[sceneId]; ok {
		s.appendEvent(SessionEvent{Timestamp: time.Now(), Type: EventFlagsSync, Flags: flags})
	}
}

func RecordTagsDiff(sceneId, label string, duration float64, incomingNotes []TimedNote) {
	mu.Lock()
	defer mu.Unlock()
	s, ok := activeSessions[sceneId]
	if !ok {
		return
	}

	if s.LastCursors == nil {
		s.LastCursors = make(map[string]TimedNote)
	}
	s.FileDurations[label] = duration

	var newEvents []SessionEvent

	for _, n := range incomingNotes {
		cursorKey := label + "|" + n.Note
		last, exists := s.LastCursors[cursorKey]

		if !exists {
			// Untouched tags start at 0, with NO end time (since we fixed http.go)
			last = TimedNote{StartTime: 0.0, EndTime: nil}
		}

		startChanged := !floatEquals(n.StartTime, last.StartTime)
		endChanged := !floatPtrEquals(n.EndTime, last.EndTime)

		if startChanged {
			// ALWAYS drop a new pin when Start moves
			newEvents = append(newEvents, SessionEvent{
				Timestamp: time.Now(), Type: EventNoteAdded, FileLabel: label,
				NoteName: n.Note, ConfigName: n.ConfigName,
				NewStart: &n.StartTime, NewEnd: n.EndTime,
			})
		} else if endChanged {
			// Update the last pin when End moves
			newEvents = append(newEvents, SessionEvent{
				Timestamp: time.Now(), Type: EventNoteChanged, FileLabel: label,
				NoteName: n.Note, ConfigName: n.ConfigName,
				OldStart: &last.StartTime, OldEnd: last.EndTime,
				NewStart: &n.StartTime, NewEnd: n.EndTime,
			})
		}

		s.LastCursors[cursorKey] = n
	}

	for _, ev := range newEvents {
		s.appendEvent(ev)
	}
}

// --- STATE DERIVATION ---

func (s *Session) Derive() DerivedState {
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
					// Do NOT flip them! If the gap is small (jitter), clamp it to 1 second.
					// Otherwise, ignore the corrupted interval completely.
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

func GetSessionState(sceneId string) (DerivedState, bool) {
	mu.Lock()
	defer mu.Unlock()
	if s, ok := activeSessions[sceneId]; ok {
		return s.Derive(), true
	}
	return DerivedState{}, false
}

// --- FILE WRITER ---

func formatTimeSec(sec float64) string {
	h := int(sec) / 3600
	m := (int(sec) % 3600) / 60
	s := int(sec) % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func (s *Session) SaveToFile() {
	state := s.Derive()
	var b strings.Builder

	b.WriteString(fmt.Sprintf("Scene: %s\n\n", s.SceneTitle))

	// --- 1. PLAYED TIMES ---
	if len(state.Played) > 0 {
		b.WriteString("Played:\n")
		var labels []string
		for lbl := range state.Played {
			labels = append(labels, lbl)
		}
		sort.Strings(labels)

		for _, lbl := range labels {
			for _, inv := range state.Played[lbl] {
				b.WriteString(fmt.Sprintf("%s: %s-%s\n", lbl, formatTimeSec(inv.Start), formatTimeSec(inv.End)))
			}
		}
		b.WriteString("\n")
	}

	// Group and sort notes
	var allNotes []TimedNote
	for _, notes := range state.TimedNotes {
		allNotes = append(allNotes, notes...)
	}

	if len(allNotes) > 0 {
		// --- 2. CHRONOLOGICAL TIMELINE LOG ---
		b.WriteString("========================================\n")
		b.WriteString("           TIMELINE LOG\n")
		b.WriteString("========================================\n")

		// Sort chronologically
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
			b.WriteString(fmt.Sprintf("%s: %-19s %s\n", n.FileLabel, timeStr, n.Note))
		}
		b.WriteString("\n")

		// --- 3. FEEDBACK SUMMARY ---
		b.WriteString("========================================\n")
		b.WriteString("         FEEDBACK SUMMARY\n")
		b.WriteString("========================================\n")

		// Group by Sentiment -> Note Name -> Instances
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

	// --- 4. GLOBAL FLAGS ---
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

	path := filepath.Join("review-notes", s.SafeTitle+"-ReviewNote.txt")
	os.WriteFile(path, []byte(b.String()), 0644)
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
