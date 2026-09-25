package review

import (
	"encoding/json"
	"net/http"
	"stash-vr/internal/reviews"

	"github.com/go-chi/chi/v5"
)

func Router() chi.Router {
	r := chi.NewRouter()
	r.Get("/", uiHandler)
	r.Get("/context", contextHandler)
	r.Post("/submit", submitHandler)
	return r
}

func contextHandler(w http.ResponseWriter, r *http.Request) {
	sceneId := r.URL.Query().Get("sceneId")

	session, ok := reviews.GetSession(sceneId)
	if !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	state := session.Derive()

	var flatNotes []reviews.TimedNote
	for _, notes := range state.TimedNotes {
		flatNotes = append(flatNotes, notes...)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"sceneId":    session.SceneId,
		"sceneTitle": session.SceneTitle,
		"categories": session.MatchedConfig.GlobalFlags,
		"timedNotes": flatNotes,
	})
}

func submitHandler(w http.ResponseWriter, r *http.Request) {
	sceneId := r.FormValue("sceneId")
	togglesJson := r.FormValue("toggles")

	var toggles map[string][]string
	if err := json.Unmarshal([]byte(togglesJson), &toggles); err == nil {
		reviews.RecordFlagsSync(sceneId, toggles)
	}

	w.WriteHeader(http.StatusOK)
}

func uiHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(htmlUI))
}

const htmlUI = `
<!DOCTYPE html>
<html>
<head>
    <title>Review Hub</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        body { background: #121212; color: #eee; font-family: sans-serif; padding: 20px; max-width: 800px; margin: auto; }
        .category { background: #1e1e1e; padding: 15px; margin-bottom: 15px; border-radius: 8px; }
        .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 10px; }
        label { display: flex; align-items: center; background: #2a2a2a; padding: 10px; border-radius: 5px; cursor: pointer; }
        input[type=checkbox] { margin-right: 10px; transform: scale(1.5); }
        .btn { padding: 15px 25px; font-size: 18px; border: none; background: #388e3c; color: white; width: 100%; cursor: pointer; border-radius: 8px;}
    </style>
</head>
<body>
    <h1>Review: <span id="sceneTitle">Loading...</span></h1>
    <div id="categories"></div>
    <button id="submitBtn" class="btn">💾 Finalize Review</button>

    <script>
        const urlParams = new URLSearchParams(window.location.search);
        const sceneId = urlParams.get('sceneId') || '';

        fetch('/review/context?sceneId=' + sceneId)
            .then(r => r.json())
            .then(data => {
                document.getElementById('sceneTitle').innerText = data.sceneTitle || "Error";
                const container = document.getElementById('categories');
                for (const [cat, options] of Object.entries(data.categories)) {
                    let html = '<div class="category"><h2>' + cat + '</h2><div class="grid">';
                    options.forEach(opt => {
                        html += '<label><input type="checkbox" data-cat="'+cat+'" value="'+opt+'"> '+opt+'</label>';
                    });
                    html += '</div></div>';
                    container.innerHTML += html;
                }
            });

        document.getElementById('submitBtn').onclick = async () => {
            const toggles = {};
            document.querySelectorAll('input[type=checkbox]:checked').forEach(cb => {
                const cat = cb.getAttribute('data-cat');
                if (!toggles[cat]) toggles[cat] = [];
                toggles[cat].push(cb.value);
            });

            const formData = new FormData();
            formData.append("sceneId", sceneId);
            formData.append("toggles", JSON.stringify(toggles));

            await fetch('/review/submit', { method: 'POST', body: formData });
            document.getElementById('submitBtn').innerText = "✅ Saved!";
        };
    </script>
</body>
</html>
`
