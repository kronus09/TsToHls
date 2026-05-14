package slicer

import (
	"fmt"
	"sync"
	"time"

	"tstohls/internal/db"
)

type SlicerStore struct {
	mu        sync.RWMutex
	slicers   map[string]*Slicer
	config    SlicerConfig
	maxProcs  int
}

var Default = &SlicerStore{
	slicers: make(map[string]*Slicer),
}

func (ss *SlicerStore) Init(config SlicerConfig, maxProcs int) {
	ss.mu.Lock()
	ss.config = config
	ss.maxProcs = maxProcs
	ss.mu.Unlock()

	go ss.cleanupLoop()
}

func (ss *SlicerStore) GetOrCreate(channelID string) (*Slicer, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if sl, ok := ss.slicers[channelID]; ok && sl.IsRunning() {
		sl.KeepAlive()
		return sl, nil
	}

	if len(ss.slicers) >= ss.maxProcs {
		ss.killOldestLocked()
	}

	ch, err := db.GetChannelByID(channelID)
	if err != nil {
		return nil, fmt.Errorf("频道 %s 不存在: %w", channelID, err)
	}
	if !ch.Enabled {
		return nil, fmt.Errorf("频道 %s 已禁用", channelID)
	}

	chInfo := &ChannelInfo{
		VideoCodec:  ch.VideoCodec,
		AudioCodec:  ch.AudioCodec,
		Width:       ch.Width,
		Height:      ch.Height,
		FrameRate:   ch.FrameRate,
		AudioSample: ch.AudioSample,
		InputFormat: ch.InputFormat,
	}

	sl := NewSlicer(channelID, ch.Url, ss.config, chInfo)
	ss.slicers[channelID] = sl

	fmt.Printf("🎬 启动切片器: %s → %s\n", channelID, ch.Url)
	go sl.Run()

	return sl, nil
}

func (ss *SlicerStore) Get(channelID string) *Slicer {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.slicers[channelID]
}

func (ss *SlicerStore) KeepAlive(channelID string) {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	if sl, ok := ss.slicers[channelID]; ok {
		sl.KeepAlive()
	}
}

func (ss *SlicerStore) GetActiveCount() int {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	count := 0
	for _, sl := range ss.slicers {
		if sl.IsRunning() {
			count++
		}
	}
	return count
}

func (ss *SlicerStore) GetActiveIDs() []string {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	var ids []string
	for id, sl := range ss.slicers {
		if sl.IsRunning() {
			ids = append(ids, id)
		}
	}
	return ids
}

func (ss *SlicerStore) killOldestLocked() {
	var oldestID string
	var oldestTime time.Time

	for id, sl := range ss.slicers {
		if !sl.IsRunning() {
			delete(ss.slicers, id)
			continue
		}
		if oldestID == "" || sl.LastAccess().Before(oldestTime) {
			oldestID = id
			oldestTime = sl.LastAccess()
		}
	}

	if oldestID != "" {
		oldest := ss.slicers[oldestID]
		oldest.Stop()
		delete(ss.slicers, oldestID)
		fmt.Printf("⚠️ 已终止最旧的切片器: %s\n", oldestID)

		deadline := time.Now().Add(3 * time.Second)
		for oldest.IsRunning() && time.Now().Before(deadline) {
			ss.mu.Unlock()
			time.Sleep(100 * time.Millisecond)
			ss.mu.Lock()
		}
	}
}

func (ss *SlicerStore) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		ss.mu.Lock()
		now := time.Now()
		for id, sl := range ss.slicers {
			if !sl.IsRunning() {
				delete(ss.slicers, id)
				continue
			}
			if now.Sub(sl.LastAccess()) > time.Duration(ss.config.IdleTimeout)*time.Second {
				sl.Stop()
				delete(ss.slicers, id)
				fmt.Printf("⏰ 已清理闲置切片器: %s\n", id)
			}
		}
		ss.mu.Unlock()
	}
}
