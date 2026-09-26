package reviews

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type TimelineNoteDef struct {
	Label     string `json:"label"`
	Type      string `json:"type"`      // "point" or "range"
	Sentiment string `json:"sentiment"` // "positive", "issue", "highlight", etc.
}

type GlobalFlagDef struct {
	Category string   `json:"category"`
	Type     string   `json:"type"` // "single-select", "multi-select", etc.
	Options  []string `json:"options"`
}

type Config struct {
	Id            string            `json:"id"`
	Prefix        string            `json:"prefix"`
	TriggerTags   []string          `json:"triggerTags"`
	Imports       []string          `json:"imports"`
	TimelineNotes []TimelineNoteDef `json:"timelineNotes"`
	GlobalFlags   []GlobalFlagDef   `json:"globalFlags"`
}

var activeConfigs = make(map[string]Config)

func init() {
	// Create the required folders
	os.MkdirAll("config", 0755)
	os.MkdirAll("review-notes", 0755)
	LoadConfigs()
}

func LoadConfigs() {
	files, _ := filepath.Glob(filepath.Join("config", "*.review.json"))

	// 1. Load all configurations
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err == nil {
			var c Config
			if json.Unmarshal(b, &c) == nil {
				activeConfigs[c.Id] = c
			}
		}
	}

	// 2. Resolve Imports (Merge configs)
	for k, c := range activeConfigs {
		for _, imp := range c.Imports {
			if imported, ok := activeConfigs[imp]; ok {
				c.TimelineNotes = append(c.TimelineNotes, imported.TimelineNotes...)
				c.GlobalFlags = append(c.GlobalFlags, imported.GlobalFlags...)
			}
		}
		c.TimelineNotes = removeDuplicateTimelineNotes(c.TimelineNotes)
		c.GlobalFlags = removeDuplicateGlobalFlags(c.GlobalFlags)
		activeConfigs[k] = c
	}
}

// Matches scenes based on explicit tags, or falls back to "*" configs
func GetMatchedConfigs(sceneTags []string) []Config {
	var matched []Config
	var fallbacks []Config

	for _, c := range activeConfigs {
		isSpecificMatch := false
		isFallback := false

		for _, t := range c.TriggerTags {
			if t == "*" {
				isFallback = true
			} else if contains(sceneTags, t) {
				isSpecificMatch = true
			}
		}

		if isSpecificMatch {
			matched = append(matched, c)
		}
		if isFallback {
			fallbacks = append(fallbacks, c)
		}
	}

	// If the scene had explicit review tags, return those. Otherwise, return fallbacks.
	if len(matched) > 0 {
		return matched
	}
	return fallbacks
}

// Matches the visual tag back to its config and sentiment
func GetNoteDetails(visualNoteName string) (configId string, sentiment string) {
	for _, c := range activeConfigs {
		for _, n := range c.TimelineNotes {
			expectedName := fmt.Sprintf("[%s] %s", c.Prefix, n.Label)
			if expectedName == visualNoteName {
				return c.Id, n.Sentiment
			}
		}
	}
	return "Review", "General"
}

// Helpers
func contains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

func removeDuplicateTimelineNotes(elements []TimelineNoteDef) []TimelineNoteDef {
	encountered := map[string]bool{}
	var result []TimelineNoteDef
	for _, v := range elements {
		if !encountered[v.Label] {
			encountered[v.Label] = true
			result = append(result, v)
		}
	}
	return result
}

func removeDuplicateGlobalFlags(elements []GlobalFlagDef) []GlobalFlagDef {
	encountered := map[string]bool{}
	var result []GlobalFlagDef
	for _, v := range elements {
		if !encountered[v.Category] {
			encountered[v.Category] = true
			result = append(result, v)
		}
	}
	return result
}
