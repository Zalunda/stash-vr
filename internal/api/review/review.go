package review

import (
	"encoding/json"
	"net/http"
	"stash-vr/internal/api/internal"
	"stash-vr/internal/library"
	"stash-vr/internal/reviews"
	"stash-vr/internal/static"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
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
	r.Post("/hidden", h.hiddenHandler)
	return r
}

func (h *handler) contextHandler(w http.ResponseWriter, r *http.Request) {
	sceneId := r.URL.Query().Get("sceneId")

	if sceneId == "" {
		sceneId = reviews.GetLastActiveSceneId()
	}

	var session *reviews.Session
	var ok bool

	if sceneId != "" {
		session, ok = reviews.GetSession(sceneId)
	}

	// If we still don't have a valid session, tell the user why.
	if !ok {
		if !reviews.HasConfigs() {
			http.Error(w, "The Review feature is not configured. Please create a .review.json configuration file in the 'config' folder.", http.StatusNotImplemented)
		} else {
			http.Error(w, "Waiting for scene... Please play a video in your VR player that has tags matching your review triggers.", http.StatusNotFound)
		}
		return
	}

	state := session.Derive()

	var flatNotes []reviews.TimedNote
	for _, notes := range state.TimedNotes {
		flatNotes = append(flatNotes, notes...)
	}

	err := internal.WriteJson(r.Context(), w, map[string]any{
		"sceneId":       session.SceneId,
		"sceneTitle":    session.SceneTitle,
		"categories":    session.MatchedConfig.GlobalFlags,
		"visualNotes":   session.VisualNotes,
		"recordedNotes": flatNotes,
		"selectedFlags": state.GlobalFlags,
		"hiddenPrefs":   reviews.GetHiddenPrefs(session.RootConfigFileBase),
	})
	if err != nil {
		log.Ctx(r.Context()).Error().Err(err).Msg("failed to write review context")
	}
}

func (h *handler) submitHandler(w http.ResponseWriter, r *http.Request) {
	sceneId := r.FormValue("sceneId")
	togglesJson := r.FormValue("toggles")

	var toggles map[string][]string
	if err := json.Unmarshal([]byte(togglesJson), &toggles); err == nil {
		reviews.RecordFlagsSync(sceneId, toggles)
	} else {
		log.Ctx(r.Context()).Warn().Err(err).Msg("failed to unmarshal toggles")
	}
	w.WriteHeader(http.StatusOK)
}

func (h *handler) noteHandler(w http.ResponseWriter, r *http.Request) {
	sceneId := r.FormValue("sceneId")
	noteName := r.FormValue("noteName")
	configName := r.FormValue("configName")
	action := r.FormValue("action") // "point", "start", "end"

	// Fetch the updated, authoritative count and success status from the backend
	count, success := reviews.AddUINote(sceneId, noteName, configName, action)

	err := internal.WriteJson(r.Context(), w, map[string]any{
		"count":   count,
		"success": success,
	})
	if err != nil {
		log.Ctx(r.Context()).Error().Err(err).Msg("failed to write note response")
	}
}

func (h *handler) uiHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFileFS(w, r, static.Fs, "review.html")
}

func (h *handler) hiddenHandler(w http.ResponseWriter, r *http.Request) {
	sceneId := r.FormValue("sceneId")
	prefsJson := r.FormValue("prefs")

	session, ok := reviews.GetSession(sceneId)
	if !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	var prefs reviews.HiddenPrefs
	if err := json.Unmarshal([]byte(prefsJson), &prefs); err == nil {
		reviews.SaveHiddenPrefs(session.RootConfigFileBase, prefs) // <-- Updated
	} else {
		log.Ctx(r.Context()).Warn().Err(err).Msg("failed to unmarshal hidden prefs")
	}
	w.WriteHeader(http.StatusOK)
}
