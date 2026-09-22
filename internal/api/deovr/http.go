package deovr

import (
	"net/http"
	"stash-vr/internal/api/internal"
	"stash-vr/internal/library"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

type httpHandler struct {
	LibraryService *library.Service
}

func (h httpHandler) indexHandler(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	baseUrl := internal.GetBaseUrl(req)

	sections, err := h.LibraryService.GetSections(ctx)
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("failed to get sections")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	vds, err := h.LibraryService.GetScenes(ctx)
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("failed to get scenes")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	dto, err := buildIndex(sections, vds, baseUrl)
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("failed to build index")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if err := internal.WriteJson(ctx, w, dto); err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("error writing response")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func (h httpHandler) videoDataHandler(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	virtualVideoId := chi.URLParam(req, "videoId")
	baseUrl := internal.GetBaseUrl(req)

	realId, targetFileId := library.ParseVirtualId(virtualVideoId)

	vd, err := h.LibraryService.GetScene(ctx, realId, false)
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("failed to get scene data")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	label := vd.GetFileLabels()[targetFileId]

	dto, err := buildVideoData(vd, baseUrl, label, targetFileId)
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("failed to build video data")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if err := internal.WriteJson(ctx, w, dto); err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("error writing response")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}

// playHandler intercepts the actual play request, swaps the primary file, then redirects to the real stream
func (h httpHandler) playHandler(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	realId := chi.URLParam(req, "videoId")
	targetFileId := req.URL.Query().Get("part")
	targetUrl := req.URL.Query().Get("url")

	if targetUrl == "" {
		http.Error(w, "missing stream url", http.StatusBadRequest)
		return
	}

	if targetFileId != "" {
		vd, err := h.LibraryService.GetScene(ctx, realId, false)
		if err == nil && len(vd.SceneParts.Files) > 0 && vd.SceneParts.Files[0].Id != targetFileId {
			log.Ctx(ctx).Info().Str("scene", realId).Str("file", targetFileId).Msg("Switching Primary File for Multi-part Scene")

			if err := h.LibraryService.SetPrimaryFile(ctx, realId, targetFileId); err != nil {
				log.Ctx(ctx).Warn().Err(err).Msg("Failed to switch primary file")
			} else {
				// Refetch scene so cache is updated for the future
				_, _ = h.LibraryService.GetScene(ctx, realId, true)
			}
		}
	}

	// Tell the VR Player to go load the actual video stream
	http.Redirect(w, req, targetUrl, http.StatusFound)
}
