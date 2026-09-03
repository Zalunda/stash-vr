package library

import (
	"maps"
	"slices"
	"sync"

	"github.com/Khan/genqlient/graphql"
	"golang.org/x/sync/singleflight"
)

type TimeInterval struct {
	Start float64
	End   float64
}

type Service struct {
	StashClient graphql.Client
	vdCache     map[string]*VideoData
	muVdCache   sync.RWMutex
	single      singleflight.Group
	Stats       Stats

	tagCache map[string]*Tag

	// For Review Hub & Heatmaps
	LastWatchedSceneId    string
	LastWatchedSceneTitle string
	muWatchHistory        sync.Mutex
	WatchHistory          map[string]map[string][]TimeInterval // sceneId -> fileId -> intervals
}

func (s *Service) AddWatchInterval(sceneId, fileId string, start, end float64) {
	if start >= end {
		return
	}
	s.muWatchHistory.Lock()
	defer s.muWatchHistory.Unlock()
	if s.WatchHistory == nil {
		s.WatchHistory = make(map[string]map[string][]TimeInterval)
	}
	if s.WatchHistory[sceneId] == nil {
		s.WatchHistory[sceneId] = make(map[string][]TimeInterval)
	}
	s.WatchHistory[sceneId][fileId] = append(s.WatchHistory[sceneId][fileId], TimeInterval{Start: start, End: end})
}

func (s *Service) GetWatchHistory(sceneId string) map[string][]TimeInterval {
	s.muWatchHistory.Lock()
	defer s.muWatchHistory.Unlock()
	if s.WatchHistory == nil || s.WatchHistory[sceneId] == nil {
		return nil
	}
	history := make(map[string][]TimeInterval)
	for k, v := range s.WatchHistory[sceneId] {
		history[k] = append([]TimeInterval(nil), v...)
	}
	return history
}

func (s *Service) ClearWatchHistory(sceneId string) {
	s.muWatchHistory.Lock()
	defer s.muWatchHistory.Unlock()
	if s.WatchHistory != nil {
		delete(s.WatchHistory, sceneId)
	}
}

func MergeIntervals(intervals []TimeInterval) []TimeInterval {
	if len(intervals) == 0 {
		return nil
	}
	slices.SortFunc(intervals, func(a, b TimeInterval) int {
		if a.Start < b.Start {
			return -1
		}
		if a.Start > b.Start {
			return 1
		}
		return 0
	})
	merged := []TimeInterval{intervals[0]}
	for _, curr := range intervals[1:] {
		last := &merged[len(merged)-1]
		if curr.Start <= last.End+2.0 { // Allow 2 seconds overlap/gap
			if curr.End > last.End {
				last.End = curr.End
			}
		} else {
			merged = append(merged, curr)
		}
	}
	return merged
}

func (libraryService *Service) snapshot() map[string]*VideoData {
	libraryService.muVdCache.RLock()
	defer libraryService.muVdCache.RUnlock()
	return maps.Clone(libraryService.vdCache)
}

func NewService(client graphql.Client) *Service {
	return &Service{
		StashClient: client,
		vdCache:     make(map[string]*VideoData),
	}
}

type Stats struct {
	Links  int
	Scenes int
}
