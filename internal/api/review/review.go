package review

import (
	"encoding/json"
	"net/http"
	"stash-vr/internal/library"
	"stash-vr/internal/reviews"

	"github.com/go-chi/chi/v5"
)

type handler struct {
	lib *library.Service
}

func Router(lib *library.Service) chi.Router {
	h := &handler{lib: lib}
	r := chi.NewRouter()
	r.Get("/", h.uiHandler)
	r.Get("/context", h.contextHandler)
	r.Post("/submit", h.submitHandler)
	r.Post("/note", h.noteHandler)
	return r
}

func (h *handler) contextHandler(w http.ResponseWriter, r *http.Request) {
	sceneId := r.URL.Query().Get("sceneId")

	if sceneId == "" {
		sceneId = reviews.GetLastActiveSceneId()
	}
	if sceneId == "" {
		http.Error(w, "No active scene. Please play a video first.", http.StatusBadRequest)
		return
	}

	session, ok := reviews.GetSession(sceneId)
	if !ok {
		vd, err := h.lib.GetScene(r.Context(), sceneId, false)
		if err != nil {
			http.Error(w, "Scene not found in library", http.StatusNotFound)
			return
		}

		var tags []string
		for _, t := range vd.SceneParts.Tags {
			tags = append(tags, t.Name)
		}

		reviews.EnsureSession(sceneId, vd.Title(), tags)
		session, ok = reviews.GetSession(sceneId)
		if !ok {
			http.Error(w, "Failed to initialize session", http.StatusInternalServerError)
			return
		}
	}

	state := session.Derive()

	var flatNotes []reviews.TimedNote
	for _, notes := range state.TimedNotes {
		flatNotes = append(flatNotes, notes...)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"sceneId":       session.SceneId,
		"sceneTitle":    session.SceneTitle,
		"categories":    session.MatchedConfig.GlobalFlags,
		"visualNotes":   session.VisualNotes,
		"recordedNotes": flatNotes,
		"selectedFlags": state.GlobalFlags,
	})
}

func (h *handler) submitHandler(w http.ResponseWriter, r *http.Request) {
	sceneId := r.FormValue("sceneId")
	togglesJson := r.FormValue("toggles")

	var toggles map[string][]string
	if err := json.Unmarshal([]byte(togglesJson), &toggles); err == nil {
		reviews.RecordFlagsSync(sceneId, toggles)
	}
	w.WriteHeader(http.StatusOK)
}

func (h *handler) noteHandler(w http.ResponseWriter, r *http.Request) {
	sceneId := r.FormValue("sceneId")
	noteName := r.FormValue("noteName")
	configName := r.FormValue("configName")
	action := r.FormValue("action") // "point", "start", "end"

	// Fetch the updated, authoritative count from the backend
	count := reviews.AddUINote(sceneId, noteName, configName, action)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"count": count})
}

func (h *handler) uiHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(htmlUI))
}

const htmlUI = `
<!DOCTYPE html>
<html>
<head>
    <title>Review Hub</title>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        body { background: #121212; color: #eee; font-family: sans-serif; padding: 20px; max-width: 1000px; margin: auto; }

        /* Layout */
        .section-box { border: 1px solid #333; border-radius: 8px; padding: 15px; margin-bottom: 20px; background: #1a1a1a; }
        .section-title { margin-top: 0; font-size: 1.2em; color: #bbb; border-bottom: 1px solid #333; padding-bottom: 10px; margin-bottom: 15px; }
        .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 10px; }

        /* Cards */
        .card { background: #242424; border: 1px solid #444; border-radius: 6px; padding: 10px; display: flex; flex-direction: column; justify-content: space-between; }
        .card-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 10px; }
        .card-title { display: flex; align-items: center; font-weight: bold; font-size: 0.95em; }
        .sentiment { font-size: 1.2em; margin-right: 6px; }

        /* Badge & Animations */
        .badge { background: #555; color: #fff; border-radius: 12px; padding: 2px 8px; font-size: 0.8em; font-weight: bold; display: inline-block; }
        @keyframes badgeBump {
            0% { transform: scale(1); background: #555; }
            50% { transform: scale(1.5); background: #4caf50; }
            100% { transform: scale(1); background: #555; }
        }
        .badge-flash { animation: badgeBump 0.5s ease; }

        /* Buttons */
        .btn-row { display: flex; gap: 5px; }
        .btn { padding: 10px; border: none; border-radius: 4px; font-size: 0.9em; font-weight: bold; cursor: pointer; color: white; background: #333; transition: background 0.2s; white-space: nowrap; }
        .btn:active { filter: brightness(1.5); }
        .btn-point { background: #0277bd; flex: 1; }
        .btn-in { background: #0277bd; flex: 2; }
        .btn-out { background: #424242; flex: 1.2; }

        /* Visual Feedback class */
        .btn.saved { background: #388e3c !important; }

        /* Global Flags */
        label { display: flex; align-items: center; background: #2a2a2a; padding: 10px; border-radius: 5px; cursor: pointer; }
        input[type=checkbox] { margin-right: 10px; transform: scale(1.5); cursor: pointer; }
        .error { color: #ff5252; font-weight: bold; }

        /* Toast */
        #toast { display: none; position: fixed; bottom: 20px; right: 20px; background: #388e3c; color: white; padding: 12px 24px; border-radius: 8px; font-weight: bold; font-size: 1.1em; z-index: 1000; box-shadow: 0px 4px 10px rgba(0,0,0,0.5); }
    </style>
</head>
<body>
    <h1>Review: <span id="sceneTitle">Loading...</span></h1>

    <div id="timelineContainer"></div>
    <div id="flagsContainer"></div>
    <div id="toast">✅ Saved!</div>

    <script>
        let activeSceneId = new URLSearchParams(window.location.search).get('sceneId') || '';

        function getSentimentIcon(sentiment) {
            if (sentiment === 'positive') return '👍';
            if (sentiment === 'issue') return '👎';
            if (sentiment === 'highlight') return '⭐';
            return '📌';
        }

        function showToast(msg) {
            const toast = document.getElementById('toast');
            toast.innerText = msg;
            toast.style.display = 'block';
            setTimeout(() => { toast.style.display = 'none'; }, 2000);
        }

        fetch('/review/context?sceneId=' + activeSceneId)
            .then(r => {
                if (!r.ok) throw new Error("Status " + r.status);
                return r.json();
            })
            .then(data => {
                activeSceneId = data.sceneId;
                document.getElementById('sceneTitle').innerText = data.sceneTitle || "Unknown Scene";

                // 1. Calculate Occurrences Badge
                const counts = {};
                if (data.recordedNotes) {
                    data.recordedNotes.forEach(n => {
                        counts[n.note] = (counts[n.note] || 0) + 1;
                    });
                }

                // 2. Group Notes by Category
                const groups = {};
                if (data.visualNotes) {
                    data.visualNotes.forEach(n => {
                        if (!groups[n.category]) groups[n.category] = [];
                        groups[n.category].push(n);
                    });
                }

                // 3. Render Timeline Notes
                const timelineHtml = [];
                for (const [catName, notes] of Object.entries(groups)) {
                    let section = '<div class="section-box"><h2 class="section-title">' + catName + '</h2><div class="grid">';

                    notes.forEach(n => {
                        const count = counts[n.noteName] || 0;
                        const badgeStyle = count > 0 ? 'display: inline-block;' : 'display: none;';
                        const badge = '<span class="badge" style="' + badgeStyle + '">' + count + '</span>';
                        const icon = getSentimentIcon(n.sentiment);

                        section += '<div class="card"><div class="card-header">' +
                            '<div class="card-title"><span class="sentiment">' + icon + '</span>' + n.label + '</div>' + badge +
                            '</div><div class="btn-row">';

                        if (n.type === 'point') {
                            section += '<button class="btn btn-point" onclick="addNote(\''+n.noteName+'\', \''+n.configId+'\', \'point\', this)">📍 Mark</button>';
                        } else {
                            section += '<button class="btn btn-in" onclick="addNote(\''+n.noteName+'\', \''+n.configId+'\', \'start\', this)">📍 Mark [</button>' +
                                       '<button class="btn btn-out" onclick="addNote(\''+n.noteName+'\', \''+n.configId+'\', \'end\', this)">] Close</button>';
                        }
                        section += '</div></div>';
                    });

                    section += '</div></div>';
                    timelineHtml.push(section);
                }

                document.getElementById('timelineContainer').innerHTML = timelineHtml.join('');

                // 4. Render Global Flags (Auto-save) with pre-selections
                if (data.categories && data.categories.length > 0) {
                    let flagsHtml = '<div class="section-box"><h2 class="section-title">Global Flags</h2>';
                    data.categories.forEach(catDef => {
                        flagsHtml += '<h3>' + catDef.category + '</h3><div class="grid" style="margin-bottom: 15px;">';
                        if (catDef.options) {

                            // Get the array of selected options for this category from the backend
                            const selectedForCat = (data.selectedFlags && data.selectedFlags[catDef.category]) ? data.selectedFlags[catDef.category] : [];

                            catDef.options.forEach(opt => {
                                // Check if this specific option was previously saved
                                const isChecked = selectedForCat.includes(opt) ? 'checked' : '';
                                flagsHtml += '<label><input type="checkbox" data-cat="'+catDef.category+'" value="'+opt+'" onchange="submitFlags()" '+isChecked+'> '+opt+'</label>';
                            });
                        }
                        flagsHtml += '</div>';
                    });
                    flagsHtml += '</div>';
                    document.getElementById('flagsContainer').innerHTML = flagsHtml;
                }
            })
            .catch(err => {
                document.getElementById('sceneTitle').innerHTML = '<span class="error">Failed to load scene context. (' + err.message + ')</span>';
            });

        function addNote(name, config, action, btnElement) {
            const formData = new FormData();
            formData.append("sceneId", activeSceneId);
            formData.append("noteName", name);
            formData.append("configName", config);
            formData.append("action", action);

            // Button Visual Feedback (Turns green, no size shift)
            btnElement.classList.add('saved');
            setTimeout(() => { btnElement.classList.remove('saved'); }, 1000);

            fetch('/review/note', { method: 'POST', body: formData })
                .then(r => r.json())
                .then(data => {
                    // Update the badge with the server's definitive count!
                    const card = btnElement.closest('.card');
                    if (card) {
                        const badge = card.querySelector('.badge');
                        if (badge) {
                            badge.innerText = data.count;
                            badge.style.display = data.count > 0 ? 'inline-block' : 'none';

                            // Badge Visual Feedback (Pops size and flashes green)
                            badge.classList.remove('badge-flash');
                            void badge.offsetWidth; // Trigger DOM reflow to restart animation
                            badge.classList.add('badge-flash');
                        }
                    }
                });
        }

        function submitFlags() {
            const toggles = {};
            document.querySelectorAll('input[type=checkbox]:checked').forEach(cb => {
                const cat = cb.getAttribute('data-cat');
                if (!toggles[cat]) toggles[cat] = [];
                toggles[cat].push(cb.value);
            });

            const formData = new FormData();
            formData.append("sceneId", activeSceneId);
            formData.append("toggles", JSON.stringify(toggles));

            fetch('/review/submit', { method: 'POST', body: formData })
                .then(() => showToast("✅ Global Flags Saved!"));
        }
    </script>
</body>
</html>
`
