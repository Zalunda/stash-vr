package reviews

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type HiddenPrefs struct {
	Notes    []string `json:"notes"`
	NoteCats []string `json:"noteCats"`
	Cats     []string `json:"cats"`
	Opts     []string `json:"opts"`
}

func GetHiddenPrefs(fileBaseName string) HiddenPrefs {
	prefs := HiddenPrefs{Notes: []string{}, NoteCats: []string{}, Cats: []string{}, Opts: []string{}}
	if fileBaseName == "" {
		return prefs
	}
	b, err := os.ReadFile(filepath.Join("config", fileBaseName+".hidden.json"))
	if err == nil {
		json.Unmarshal(b, &prefs)
	}
	return prefs
}

func SaveHiddenPrefs(fileBaseName string, prefs HiddenPrefs) {
	if fileBaseName == "" {
		return
	}
	b, err := json.MarshalIndent(prefs, "", "  ")
	if err == nil {
		os.WriteFile(filepath.Join("config", fileBaseName+".hidden.json"), b, 0644)
	}
}
