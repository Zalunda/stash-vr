package reviews

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	Name        string              `json:"name"`
	TriggerTags []string            `json:"triggerTags"`
	Imports     []string            `json:"imports"`
	TimedNotes  []string            `json:"timedNotes"`
	RangedNotes []string            `json:"rangedNotes"` // Added RangedNotes
	GlobalFlags map[string][]string `json:"globalFlags"`
}

var activeConfigs = make(map[string]Config)

func init() {
	os.MkdirAll("config", 0755)
	os.MkdirAll("review-notes", 0755)
	LoadConfigs()
}

func LoadConfigs() {
	files, _ := filepath.Glob(filepath.Join("config", "*.json"))

	for _, f := range files {
		b, err := os.ReadFile(f)
		if err == nil {
			var c Config
			if json.Unmarshal(b, &c) == nil {
				if c.GlobalFlags == nil {
					c.GlobalFlags = make(map[string][]string)
				}
				activeConfigs[c.Name] = c
			}
		}
	}

	for k, c := range activeConfigs {
		for _, imp := range c.Imports {
			if imported, ok := activeConfigs[imp]; ok {
				c.TimedNotes = append(c.TimedNotes, imported.TimedNotes...)
				c.RangedNotes = append(c.RangedNotes, imported.RangedNotes...) // Merge RangedNotes
				for cat, flags := range imported.GlobalFlags {
					if c.GlobalFlags == nil {
						c.GlobalFlags = make(map[string][]string)
					}
					c.GlobalFlags[cat] = append(c.GlobalFlags[cat], flags...)
				}
			}
		}
		c.TimedNotes = removeDuplicates(c.TimedNotes)
		c.RangedNotes = removeDuplicates(c.RangedNotes)
		activeConfigs[k] = c
	}
}

func GetMatchedConfigs(sceneTags []string) []Config {
	var matched []Config
	for _, c := range activeConfigs {
		for _, t := range sceneTags {
			if contains(c.TriggerTags, t) {
				matched = append(matched, c)
				break
			}
		}
	}
	return matched
}

func contains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

func removeDuplicates(elements []string) []string {
	encountered := map[string]bool{}
	var result []string
	for v := range elements {
		if !encountered[elements[v]] {
			encountered[elements[v]] = true
			result = append(result, elements[v])
		}
	}
	return result
}

func GetConfigNameForNote(note string) string {
	for _, c := range activeConfigs {
		// Check both arrays for the note name
		if contains(c.TimedNotes, note) || contains(c.RangedNotes, note) {
			return c.Name
		}
	}
	return "Review"
}
