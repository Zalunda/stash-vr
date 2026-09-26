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
	Name          string            `json:"name"` // Optional clean name for UI groupings
	Prefix        string            `json:"prefix"`
	TriggerTags   []string          `json:"triggerTags"`
	Imports       []string          `json:"imports"`
	TimelineNotes []TimelineNoteDef `json:"timelineNotes"`
	GlobalFlags   []GlobalFlagDef   `json:"globalFlags"`
}

type VisualNoteDef struct {
	Category  string `json:"category"`
	NoteName  string `json:"noteName"`
	Label     string `json:"label"`
	Type      string `json:"type"`
	Sentiment string `json:"sentiment"`
	ConfigId  string `json:"configId"`
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
}

// GetMatchedConfigs finds the matching root configs and dynamically resolves their imports
func GetMatchedConfigs(sceneTags []string) []Config {
	var roots []Config
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
			roots = append(roots, c)
		}
		if isFallback {
			fallbacks = append(fallbacks, c)
		}
	}

	var startNodes []Config
	if len(roots) > 0 {
		startNodes = roots
	} else {
		startNodes = fallbacks
	}

	// Resolve the imports graph while keeping configs distinct
	visited := make(map[string]bool)
	var result []Config

	var visit func(c Config)
	visit = func(c Config) {
		if visited[c.Id] {
			return
		}
		visited[c.Id] = true
		result = append(result, c)
		for _, imp := range c.Imports {
			if imported, ok := activeConfigs[imp]; ok {
				visit(imported)
			}
		}
	}

	for _, c := range startNodes {
		visit(c)
	}

	return result
}

// GetVisualNotes structures the notes for the Grid UI and groups them by their native config
func GetVisualNotes(sceneTags []string) []VisualNoteDef {
	configs := GetMatchedConfigs(sceneTags)
	var notes []VisualNoteDef
	encountered := map[string]bool{}

	for _, c := range configs {
		catName := c.Name
		if catName == "" {
			catName = c.Id // Fallback if "name" isn't in the JSON
		}

		for _, n := range c.TimelineNotes {
			rawName := fmt.Sprintf("[%s] %s", c.Prefix, n.Label)
			if !encountered[rawName] {
				encountered[rawName] = true
				notes = append(notes, VisualNoteDef{
					Category:  catName,
					NoteName:  rawName,
					Label:     n.Label,
					Type:      n.Type,
					Sentiment: n.Sentiment,
					ConfigId:  c.Id,
				})
			}
		}
	}
	return notes
}

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

func contains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
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
