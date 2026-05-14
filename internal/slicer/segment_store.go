package slicer

import (
	"fmt"
	"sync"
)

type Segment struct {
	Index int
	Data  []byte
	Duration float64
	Discontinuity bool
}

type ChannelStore struct {
	mu       sync.RWMutex
	segments []Segment
	seqNum   int
	maxList  int
	m3u8Cache []byte
	m3u8Dirty bool
	duration  float64
}

func NewChannelStore(maxListSize int) *ChannelStore {
	return &ChannelStore{
		maxList: maxListSize,
	}
}

func (cs *ChannelStore) AddSegment(data []byte, duration float64, discontinuity bool) int {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	seg := Segment{
		Index:        cs.seqNum,
		Data:         data,
		Duration:     duration,
		Discontinuity: discontinuity,
	}
	cs.seqNum++
	cs.segments = append(cs.segments, seg)
	cs.duration += duration

	if len(cs.segments) > cs.maxList {
		cs.segments = cs.segments[len(cs.segments)-cs.maxList:]
	}

	cs.m3u8Dirty = true
	return seg.Index
}

func (cs *ChannelStore) GetSegment(index int) ([]byte, bool) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	for _, seg := range cs.segments {
		if seg.Index == index {
			return seg.Data, true
		}
	}
	return nil, false
}

func (cs *ChannelStore) GetM3U8() []byte {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if !cs.m3u8Dirty && cs.m3u8Cache != nil {
		return cs.m3u8Cache
	}

	cs.m3u8Cache = cs.generateM3U8()
	cs.m3u8Dirty = false
	return cs.m3u8Cache
}

func (cs *ChannelStore) generateM3U8() []byte {
	if len(cs.segments) == 0 {
		return []byte("#EXTM3U\n")
	}

	var b []byte
	b = append(b, "#EXTM3U\n"...)
	b = append(b, "#EXT-X-VERSION:3\n"...)
	b = append(b, fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", int(cs.maxDuration()+0.999))...)

	firstSeq := cs.segments[0].Index
	b = append(b, fmt.Sprintf("#EXT-X-MEDIA-SEQUENCE:%d\n", firstSeq)...)

	for i, seg := range cs.segments {
		if seg.Discontinuity && (i > 0 || firstSeq > 0) {
			b = append(b, "#EXT-X-DISCONTINUITY\n"...)
		}
		b = append(b, fmt.Sprintf("#EXTINF:%.3f,\n", seg.Duration)...)
		b = append(b, fmt.Sprintf("seg%05d.ts\n", seg.Index)...)
	}

	return b
}

func (cs *ChannelStore) maxDuration() float64 {
	max := 1.0
	for _, seg := range cs.segments {
		if seg.Duration > max {
			max = seg.Duration
		}
	}
	return max
}

func (cs *ChannelStore) SegmentCount() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return len(cs.segments)
}

func (cs *ChannelStore) TotalDuration() float64 {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.duration
}
