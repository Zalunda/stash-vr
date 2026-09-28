package reviews

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"stash-vr/internal/config"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
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
	Name          string            `json:"name"`
	Prefix        string            `json:"prefix"`
	TriggerTags   []string          `json:"triggerTags"`
	Imports       []string          `json:"imports"`
	TimelineNotes []TimelineNoteDef `json:"timelineNotes"`
	GlobalFlags   []GlobalFlagDef   `json:"globalFlags"`
	FileBaseName  string            `json:"-"`
}

type VisualNoteDef struct {
	Category  string `json:"category"`
	NoteName  string `json:"noteName"`
	Label     string `json:"label"`
	Type      string `json:"type"`
	Sentiment string `json:"sentiment"`
	ConfigId  string `json:"configId"`
}

var (
	activeConfigsMu sync.RWMutex
	activeConfigs   = make(map[string]Config)
)

func LoadConfigs() {
	pattern := filepath.Join(config.Application().ConfigPath, "*.review.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		log.Error().Err(err).Str("pattern", pattern).Msg("Failed to glob review configs")
		return
	}

	newConfigs := make(map[string]Config)

	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			log.Error().Err(err).Str("file", f).Msg("Failed to read review config")
			continue
		}

		var c Config
		if err := json.Unmarshal(b, &c); err != nil {
			log.Error().Err(err).Str("file", f).Msg("Failed to parse/unmarshal review config JSON. Check for syntax errors!")
			continue
		}

		base := filepath.Base(f)
		c.FileBaseName = strings.TrimSuffix(base, ".json")
		newConfigs[c.Id] = c
		log.Debug().Str("id", c.Id).Str("file", f).Msg("Loaded review config")
	}

	// Safely swap the map out in memory
	activeConfigsMu.Lock()
	activeConfigs = newConfigs
	activeConfigsMu.Unlock()

	log.Info().Int("count", len(newConfigs)).Msg("Review configs loaded into memory")
}

func HasConfigs() bool {
	activeConfigsMu.RLock()
	defer activeConfigsMu.RUnlock()
	return len(activeConfigs) > 0
}

// GetMatchedConfigs finds the matching root configs and dynamically resolves their imports
func GetMatchedConfigs(sceneTags []string) []Config {
	activeConfigsMu.RLock()
	defer activeConfigsMu.RUnlock()

	var roots []Config
	var fallbacks []Config

	for _, c := range activeConfigs {
		isSpecificMatch := false
		isFallback := false

		for _, t := range c.TriggerTags {
			if t == "*" {
				isFallback = true
			} else if slices.Contains(sceneTags, t) {
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

		// 1. Visit dependencies FIRST (Post-order traversal)
		for _, imp := range c.Imports {
			if imported, ok := activeConfigs[imp]; ok {
				visit(imported)
			}
		}

		// 2. Append this config AFTER its dependencies
		result = append(result, c)
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
	activeConfigsMu.RLock()
	defer activeConfigsMu.RUnlock()

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
