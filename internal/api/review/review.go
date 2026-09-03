package review

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"stash-vr/internal/library"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	optionsDir = "review_options"
	outputDir  = "output/reviews"
)

var defaultCategories = map[string][]string{
	"Video_Good":     {"Great image quality", "Good perspective", "Good lighting"},
	"Video_Bad":      {"Cross eye", "Bad scale", "Too dark", "Compression artifacts"},
	"Acting_Good":    {"Great eye contact", "Nice body", "Enthusiastic", "Good dirty talk"},
	"Acting_Bad":     {"Always looking at the director", "Long pauses", "Bad acting", "Too much moaning"},
	"Funscript_Good": {"Good timing during fast action", "Good timing during teasing", "Matches intensity"},
	"Funscript_Bad":  {"Missing light touch during teasing", "Out of sync", "Too repetitive"},
	"Subtitles_Good": {"Accurate translation", "Good pacing"},
	"Subtitles_Bad":  {"Out of sync", "Missing lines", "Hard to read"},
}

func init() {
	os.MkdirAll(optionsDir, 0755)
	os.MkdirAll(outputDir, 0755)
	for cat, items := range defaultCategories {
		path := filepath.Join(optionsDir, cat+".txt")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			os.WriteFile(path, []byte(strings.Join(items, "\n")), 0644)
		}
	}
}

func readOptions() map[string][]string {
	options := make(map[string][]string)
	files, _ := os.ReadDir(optionsDir)
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".txt") {
			name := strings.TrimSuffix(f.Name(), ".txt")
			name = strings.ReplaceAll(name, "_", " ")
			content, _ := os.ReadFile(filepath.Join(optionsDir, f.Name()))
			lines := strings.Split(string(content), "\n")
			var validLines []string
			for _, l := range lines {
				if trimmed := strings.TrimSpace(l); trimmed != "" {
					validLines = append(validLines, trimmed)
				}
			}
			options[name] = validLines
		}
	}
	return options
}

func formatDuration(seconds float64) string {
	d := time.Duration(seconds) * time.Second
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

func ContextHandler(libraryService *library.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"sceneId":    libraryService.LastWatchedSceneId,
			"sceneTitle": libraryService.LastWatchedSceneTitle,
			"categories": readOptions(),
		})
	}
}

func SubmitHandler(libraryService *library.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(50 << 20); err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		sceneId := r.FormValue("sceneId")
		sceneTitle := r.FormValue("sceneTitle")
		togglesJson := r.FormValue("toggles")

		safeTitle := strings.Map(func(r rune) rune {
			if strings.ContainsRune(`\/:*?"<>|`, r) {
				return '_'
			}
			return r
		}, sceneTitle)
		if safeTitle == "" {
			safeTitle = "Unknown_Scene"
		}

		timestamp := time.Now().Format("2006-01-02_15-04-05")
		baseFilename := fmt.Sprintf("%s_%s", timestamp, safeTitle)

		var txtBuilder strings.Builder
		txtBuilder.WriteString(fmt.Sprintf("Scene: %s\nDate: %s\n\n", sceneTitle, time.Now().Format(time.RFC1123)))

		// Format Heatmap / Watch History
		history := libraryService.GetWatchHistory(sceneId)
		vd, err := libraryService.GetScene(r.Context(), sceneId, false)
		if err == nil && len(history) > 0 {
			txtBuilder.WriteString("Watch History:\n")
			labels := vd.GetValidatedLabels()
			for i, f := range vd.SceneParts.Files {
				intervals, ok := history[f.Id]
				if !ok || len(intervals) == 0 {
					continue
				}

				merged := library.MergeIntervals(intervals)
				var totalWatched float64
				for _, inv := range merged {
					totalWatched += (inv.End - inv.Start)
				}

				label := labels[i]
				if label == "" {
					label = "Main"
				}
				partName := "Part " + label

				if totalWatched >= f.Duration*0.95 {
					txtBuilder.WriteString(fmt.Sprintf("- %s: Watched 100%%\n", partName))
				} else {
					txtBuilder.WriteString(fmt.Sprintf("- %s: ", partName))
					var parts []string
					for _, inv := range merged {
						parts = append(parts, fmt.Sprintf("%s - %s", formatDuration(inv.Start), formatDuration(inv.End)))
					}
					txtBuilder.WriteString(strings.Join(parts, ", ") + "\n")
				}
			}
			txtBuilder.WriteString("\n")
		}
		libraryService.ClearWatchHistory(sceneId)

		var toggles map[string][]string
		json.Unmarshal([]byte(togglesJson), &toggles)
		for cat, items := range toggles {
			if len(items) > 0 {
				txtBuilder.WriteString(fmt.Sprintf("[%s]\n", cat))
				for _, item := range items {
					txtBuilder.WriteString(fmt.Sprintf("- %s\n", item))
				}
				txtBuilder.WriteString("\n")
			}
		}

		os.WriteFile(filepath.Join(outputDir, baseFilename+".txt"), []byte(txtBuilder.String()), 0644)

		file, _, err := r.FormFile("audio")
		if err == nil {
			defer file.Close()
			out, _ := os.Create(filepath.Join(outputDir, baseFilename+".webm"))
			defer out.Close()
			io.Copy(out, file)
		}

		log.Ctx(r.Context()).Info().Str("title", sceneTitle).Msg("Review saved successfully")
		w.WriteHeader(http.StatusOK)
	}
}

func UIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(htmlUI))
	}
}

const htmlUI = `
<!DOCTYPE html>
<html>
<head>
    <title>Review Hub</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        body { background: #121212; color: #eee; font-family: sans-serif; padding: 20px; max-width: 800px; margin: auto; }
        h1, h2 { color: #fff; }
        .category { background: #1e1e1e; padding: 15px; margin-bottom: 15px; border-radius: 8px; }
        .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 10px; }
        label { display: flex; align-items: center; background: #2a2a2a; padding: 10px; border-radius: 5px; cursor: pointer; }
        input[type=checkbox] { margin-right: 10px; transform: scale(1.5); }
        .btn { padding: 15px 25px; font-size: 18px; border: none; border-radius: 8px; cursor: pointer; color: white; width: 100%; margin-top: 10px; }
        #recordBtn { background: #d32f2f; }
        #recordBtn.recording { background: #f44336; animation: pulse 1.5s infinite; }
        #submitBtn { background: #388e3c; }
        @keyframes pulse { 0% { opacity: 1; } 50% { opacity: 0.5; } 100% { opacity: 1; } }
    </style>
</head>
<body>
    <h1>Review: <span id="sceneTitle">Loading...</span></h1>
    <div id="categories"></div>
    <button id="recordBtn" class="btn">🎤 Start Voice Review</button>
    <audio id="audioPlayback" controls style="display:none; width: 100%; margin-top: 10px;"></audio>
    <button id="submitBtn" class="btn">💾 Submit Review</button>
    <script>
        let sceneContext = {};
        let mediaRecorder;
        let audioChunks = [];
        let audioBlob = null;

        fetch('/api/review/context').then(r => r.json()).then(data => {
            sceneContext = data;
            document.getElementById('sceneTitle').innerText = data.sceneTitle || "Watch a video first!";
            const container = document.getElementById('categories');
            for (const [cat, options] of Object.entries(data.categories)) {
                let html = '<div class="category"><h2>' + cat + '</h2><div class="grid">';
                options.forEach(opt => html += '<label><input type="checkbox" data-cat="'+cat+'" value="'+opt+'"> '+opt+'</label>');
                html += '</div></div>';
                container.innerHTML += html;
            }
        });

        const recordBtn = document.getElementById('recordBtn');
        recordBtn.onclick = async () => {
            if (mediaRecorder && mediaRecorder.state === "recording") {
                mediaRecorder.stop();
                recordBtn.innerText = "🎤 Re-record Voice";
                recordBtn.classList.remove("recording");
                return;
            }
            try {
                const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
                mediaRecorder = new MediaRecorder(stream);
                audioChunks = [];
                mediaRecorder.ondataavailable = e => audioChunks.push(e.data);
                mediaRecorder.onstop = () => {
                    audioBlob = new Blob(audioChunks, { type: 'audio/webm' });
                    const player = document.getElementById('audioPlayback');
                    player.src = URL.createObjectURL(audioBlob);
                    player.style.display = "block";
                };
                mediaRecorder.start();
                recordBtn.innerText = "⏹ Stop Recording";
                recordBtn.classList.add("recording");
            } catch (err) { alert("Microphone blocked. Ensure you are using HTTPS."); }
        };

        document.getElementById('submitBtn').onclick = async () => {
            document.getElementById('submitBtn').innerText = "Submitting...";
            const toggles = {};
            document.querySelectorAll('input[type=checkbox]:checked').forEach(cb => {
                const cat = cb.getAttribute('data-cat');
                if (!toggles[cat]) toggles[cat] = [];
                toggles[cat].push(cb.value);
            });
            const formData = new FormData();
            formData.append("sceneId", sceneContext.sceneId);
            formData.append("sceneTitle", sceneContext.sceneTitle);
            formData.append("toggles", JSON.stringify(toggles));
            if (audioBlob) formData.append("audio", audioBlob, "review.webm");

            await fetch('/api/review/submit', { method: 'POST', body: formData });
            document.getElementById('submitBtn').innerText = "✅ Saved!";
            document.getElementById('submitBtn').style.background = "#2e7d32";
        };
    </script>
</body>
</html>
`
