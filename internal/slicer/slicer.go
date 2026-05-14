package slicer

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/asticode/go-astikit"
)

var Err5XX = errors.New("5XX")

type SlicerConfig struct {
	HlsTime        float64
	HlsListSize    int
	IdleTimeout    int
	ReconnectDelay int
	AudioCodec     string
	AudioBitrate   string
}

type Slicer struct {
	channelID  string
	sourceURL  string
	config     SlicerConfig
	store      *ChannelStore
	ChInfo     *ChannelInfo
	mu         sync.RWMutex
	running    bool
	stopCh     chan struct{}
	lastAccess time.Time
	on5XX      func()
}

type ChannelInfo struct {
	VideoCodec  string
	AudioCodec  string
	Width       int
	Height      int
	FrameRate   string
	AudioSample int
	InputFormat string
}

func NewSlicer(channelID, sourceURL string, config SlicerConfig, chInfo *ChannelInfo) *Slicer {
	return &Slicer{
		channelID:  channelID,
		sourceURL:  sourceURL,
		config:     config,
		store:      NewChannelStore(config.HlsListSize),
		ChInfo:     chInfo,
		stopCh:     make(chan struct{}),
		lastAccess: time.Now(),
	}
}

func (s *Slicer) Run() {
	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	for {
		select {
		case <-s.stopCh:
			return
		default:
		}

		if err := s.slice(); err != nil {
			if errors.Is(err, Err5XX) && s.on5XX != nil {
				fmt.Printf("⚠️ 切片器 %s 5XX错误, 释放最旧连接后重试\n", s.channelID)
				s.on5XX()
				select {
				case <-s.stopCh:
					return
				case <-time.After(1 * time.Second):
				}
				continue
			}
			fmt.Printf("⚠️ 切片器 %s 退出: %v，%d秒后重连\n", s.channelID, err, s.config.ReconnectDelay)
		}

		select {
		case <-s.stopCh:
			return
		case <-time.After(time.Duration(s.config.ReconnectDelay) * time.Second):
		}
	}
}

func ptsToSeconds(pts int64, tb astiav.Rational) float64 {
	if tb.Den() == 0 {
		return 0
	}
	return float64(pts) * float64(tb.Num()) / float64(tb.Den())
}

type segmentWriter struct {
	buf []byte
}

func (sw *segmentWriter) write(b []byte) (int, error) {
	sw.buf = append(sw.buf, b...)
	return len(b), nil
}

type audioTranscoder struct {
	decCodecContext *astiav.CodecContext
	decFrame        *astiav.Frame
	encCodec        *astiav.Codec
	encCodecContext *astiav.CodecContext
	encPkt          *astiav.Packet
	filterFrame     *astiav.Frame
	filterGraph     *astiav.FilterGraph
	buffersrcCtx    *astiav.BuffersrcFilterContext
	buffersinkCtx   *astiav.BuffersinkFilterContext
	inputStream     *astiav.Stream
	outputStream    *astiav.Stream
}

func (s *Slicer) slice() error {
	closer := astikit.NewCloser()
	defer closer.Close()

	demuxCtx := astiav.AllocFormatContext()
	if demuxCtx == nil {
		return fmt.Errorf("分配 demux 上下文失败")
	}
	closer.Add(demuxCtx.Free)

	ii := astiav.NewIOInterrupter()
	closer.Add(ii.Free)
	demuxCtx.SetIOInterrupter(ii)

	interruptDone := make(chan struct{})
	go func() {
		defer close(interruptDone)
		select {
		case <-s.stopCh:
			ii.Interrupt()
		case <-interruptDone:
		}
	}()

	dict := astiav.NewDictionary()
	closer.Add(dict.Free)
	dict.Set("reconnect", "1", 0)
	dict.Set("reconnect_streamed", "1", 0)
	dict.Set("reconnect_delay_max", fmt.Sprintf("%d", s.config.ReconnectDelay), 0)
	if s.ChInfo.InputFormat != "" {
		dict.Set("f", s.ChInfo.InputFormat, 0)
	}
	dict.Set("probesize", "32768", 0)
	dict.Set("analyzeduration", "0", 0)

	if err := demuxCtx.OpenInput(s.sourceURL, nil, dict); err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "5XX") || strings.Contains(errMsg, "Server Error") {
			return fmt.Errorf("%w: %s", Err5XX, errMsg)
		}
		return fmt.Errorf("打开源失败: %w", err)
	}
	closer.Add(demuxCtx.CloseInput)

	if err := demuxCtx.FindStreamInfo(nil); err != nil {
		return fmt.Errorf("探测流信息失败: %w", err)
	}

	var videoInStream, audioInStream *astiav.Stream
	for _, st := range demuxCtx.Streams() {
		mt := st.CodecParameters().MediaType()
		if mt == astiav.MediaTypeVideo && videoInStream == nil {
			videoInStream = st
		} else if mt == astiav.MediaTypeAudio && audioInStream == nil {
			audioInStream = st
		}
	}
	if videoInStream == nil {
		return fmt.Errorf("未找到视频流")
	}

	needAudioTranscode := audioInStream != nil && s.config.AudioCodec != "copy"

	var at *audioTranscoder
	sw := &segmentWriter{}

	var audioEncCodecContext *astiav.CodecContext

	if needAudioTranscode {
		var atErr error
		at, atErr = s.createAudioTranscoder(audioInStream, closer)
		if atErr != nil {
			return fmt.Errorf("创建音频转码器失败: %w", atErr)
		}
		audioEncCodecContext = at.encCodecContext
	}

	muxCtx, ioCtx, videoOutStream, audioOutStream, err := s.createMuxer(videoInStream, audioInStream, sw, audioEncCodecContext, closer)
	if err != nil {
		return err
	}

	if needAudioTranscode && at != nil {
		at.outputStream = audioOutStream
	}

	var segStartPts float64
	keyframeSeen := false
	segmentCount := 0

	pkt := astiav.AllocPacket()
	closer.Add(pkt.Free)

	for {
		select {
		case <-s.stopCh:
			return nil
		default:
		}

		if err := demuxCtx.ReadFrame(pkt); err != nil {
			if errors.Is(err, astiav.ErrEof) {
				return nil
			}
			fmt.Printf("⚠️ %s 读帧错误: %v\n", s.channelID, err)
			time.Sleep(time.Duration(s.config.ReconnectDelay) * time.Second)
			continue
		}

		inStream := demuxCtx.Streams()[pkt.StreamIndex()]
		if inStream == nil {
			pkt.Unref()
			continue
		}

		isVideo := inStream.CodecParameters().MediaType() == astiav.MediaTypeVideo
		isAudio := inStream.CodecParameters().MediaType() == astiav.MediaTypeAudio

		if isVideo {
			if !keyframeSeen {
				if pkt.Flags().Has(astiav.PacketFlagKey) {
					keyframeSeen = true
					segStartPts = ptsToSeconds(pkt.Pts(), inStream.TimeBase())
				} else {
					pkt.Unref()
					continue
				}
			}

			if pkt.Flags().Has(astiav.PacketFlagKey) && segmentCount > 0 {
				currentPts := ptsToSeconds(pkt.Pts(), inStream.TimeBase())
				elapsed := currentPts - segStartPts
				if elapsed >= s.config.HlsTime {
					muxCtx.WriteTrailer()
					s.store.AddSegment(sw.buf, elapsed, false)
					segmentCount++
					sw.buf = nil
					segStartPts = currentPts

					muxCtx.Free()
					ioCtx.Free()

					var newAt *audioTranscoder
					var newAudioEncCtx *astiav.CodecContext
					if needAudioTranscode {
						newAt, err = s.createAudioTranscoder(audioInStream, closer)
						if err != nil {
							pkt.Unref()
							return fmt.Errorf("重建音频转码器失败: %w", err)
						}
						newAudioEncCtx = newAt.encCodecContext
					}

					muxCtx, ioCtx, videoOutStream, audioOutStream, err = s.createMuxer(videoInStream, audioInStream, sw, newAudioEncCtx, closer)
					if err != nil {
						pkt.Unref()
						return err
					}

					if needAudioTranscode && newAt != nil {
						newAt.outputStream = audioOutStream
						at = newAt
					}

					outStream := videoOutStream
					pkt.SetStreamIndex(outStream.Index())
					pkt.RescaleTs(inStream.TimeBase(), outStream.TimeBase())
					pkt.SetPos(-1)

					if err := muxCtx.WriteInterleavedFrame(pkt); err != nil {
						fmt.Printf("⚠️ %s 写首帧错误: %v\n", s.channelID, err)
					}
					pkt.Unref()
					continue
				}
			}
		}

		if isVideo {
			outStream := videoOutStream
			pkt.SetStreamIndex(outStream.Index())
			pkt.RescaleTs(inStream.TimeBase(), outStream.TimeBase())
			pkt.SetPos(-1)

			if err := muxCtx.WriteInterleavedFrame(pkt); err != nil {
				fmt.Printf("⚠️ %s 写视频帧错误: %v\n", s.channelID, err)
			}

			if segmentCount == 0 && keyframeSeen {
				segmentCount = 1
			}
		} else if isAudio && audioOutStream != nil {
			if needAudioTranscode && at != nil {
				pkt.RescaleTs(at.inputStream.TimeBase(), at.decCodecContext.TimeBase())
				if err := at.decCodecContext.SendPacket(pkt); err != nil {
					fmt.Printf("⚠️ %s 音频解码发送失败: %v\n", s.channelID, err)
					pkt.Unref()
					continue
				}
				for {
					if err := at.decCodecContext.ReceiveFrame(at.decFrame); err != nil {
						if !errors.Is(err, astiav.ErrEof) && !errors.Is(err, astiav.ErrEagain) {
							fmt.Printf("⚠️ %s 音频解码接收失败: %v\n", s.channelID, err)
						}
						break
					}
					s.filterEncodeWriteFrame(at.decFrame, at, muxCtx)
					at.decFrame.Unref()
				}
			} else {
				pkt.SetStreamIndex(audioOutStream.Index())
				pkt.RescaleTs(inStream.TimeBase(), audioOutStream.TimeBase())
				pkt.SetPos(-1)
				if err := muxCtx.WriteInterleavedFrame(pkt); err != nil {
					fmt.Printf("⚠️ %s 写音频帧错误: %v\n", s.channelID, err)
				}
			}
		}

		pkt.Unref()
	}
}

func (s *Slicer) filterEncodeWriteFrame(f *astiav.Frame, at *audioTranscoder, muxCtx *astiav.FormatContext) {
	if err := at.buffersrcCtx.AddFrame(f, astiav.NewBuffersrcFlags(astiav.BuffersrcFlagKeepRef)); err != nil {
		return
	}
	for {
		if err := at.buffersinkCtx.GetFrame(at.filterFrame, astiav.NewBuffersinkFlags()); err != nil {
			if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
				break
			}
			break
		}
		at.filterFrame.SetPictureType(astiav.PictureTypeNone)
		s.encodeWriteFrame(at.filterFrame, at, muxCtx)
		at.filterFrame.Unref()
	}
}

func (s *Slicer) encodeWriteFrame(f *astiav.Frame, at *audioTranscoder, muxCtx *astiav.FormatContext) {
	if err := at.encCodecContext.SendFrame(f); err != nil {
		return
	}
	for {
		if err := at.encCodecContext.ReceivePacket(at.encPkt); err != nil {
			if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
				break
			}
			break
		}
		at.encPkt.SetStreamIndex(at.outputStream.Index())
		at.encPkt.RescaleTs(at.encCodecContext.TimeBase(), at.outputStream.TimeBase())
		if err := muxCtx.WriteInterleavedFrame(at.encPkt); err != nil {
			fmt.Printf("⚠️ %s 写转码音频帧错误: %v\n", s.channelID, err)
		}
		at.encPkt.Unref()
	}
}

func (s *Slicer) createAudioTranscoder(audioInStream *astiav.Stream, closer *astikit.Closer) (*audioTranscoder, error) {
	at := &audioTranscoder{
		inputStream: audioInStream,
	}

	decCodec := astiav.FindDecoder(audioInStream.CodecParameters().CodecID())
	if decCodec == nil {
		return nil, fmt.Errorf("找不到音频解码器")
	}

	at.decCodecContext = astiav.AllocCodecContext(decCodec)
	if at.decCodecContext == nil {
		return nil, fmt.Errorf("分配音频解码上下文失败")
	}
	closer.Add(at.decCodecContext.Free)

	if err := audioInStream.CodecParameters().ToCodecContext(at.decCodecContext); err != nil {
		return nil, fmt.Errorf("更新音频解码参数失败: %w", err)
	}

	if err := at.decCodecContext.Open(decCodec, nil); err != nil {
		return nil, fmt.Errorf("打开音频解码器失败: %w", err)
	}
	at.decCodecContext.SetTimeBase(audioInStream.TimeBase())

	at.decFrame = astiav.AllocFrame()
	closer.Add(at.decFrame.Free)

	at.encCodec = astiav.FindEncoder(astiav.CodecIDAac)
	if at.encCodec == nil {
		return nil, fmt.Errorf("找不到 AAC 编码器")
	}

	at.encCodecContext = astiav.AllocCodecContext(at.encCodec)
	if at.encCodecContext == nil {
		return nil, fmt.Errorf("分配音频编码上下文失败")
	}
	closer.Add(at.encCodecContext.Free)

	if v := at.encCodec.SupportedChannelLayouts(); len(v) > 0 {
		at.encCodecContext.SetChannelLayout(v[0])
	} else {
		at.encCodecContext.SetChannelLayout(at.decCodecContext.ChannelLayout())
	}
	at.encCodecContext.SetSampleRate(at.decCodecContext.SampleRate())
	if v := at.encCodec.SupportedSampleFormats(); len(v) > 0 {
		at.encCodecContext.SetSampleFormat(v[0])
	} else {
		at.encCodecContext.SetSampleFormat(at.decCodecContext.SampleFormat())
	}
	at.encCodecContext.SetTimeBase(astiav.NewRational(1, at.encCodecContext.SampleRate()))

	if s.config.AudioBitrate != "" {
		var bitrate int
		if _, err := fmt.Sscanf(s.config.AudioBitrate, "%d", &bitrate); err == nil && bitrate > 0 {
			if strings.HasSuffix(s.config.AudioBitrate, "k") || strings.HasSuffix(s.config.AudioBitrate, "K") {
				bitrate *= 1000
			}
			at.encCodecContext.SetBitRate(int64(bitrate))
		}
	}

	if err := at.encCodecContext.Open(at.encCodec, nil); err != nil {
		return nil, fmt.Errorf("打开 AAC 编码器失败: %w", err)
	}

	at.filterGraph = astiav.AllocFilterGraph()
	if at.filterGraph == nil {
		return nil, fmt.Errorf("分配音频 filter graph 失败")
	}
	closer.Add(at.filterGraph.Free)

	outputs := astiav.AllocFilterInOut()
	if outputs == nil {
		return nil, fmt.Errorf("分配 filter outputs 失败")
	}
	closer.Add(outputs.Free)

	inputs := astiav.AllocFilterInOut()
	if inputs == nil {
		return nil, fmt.Errorf("分配 filter inputs 失败")
	}
	closer.Add(inputs.Free)

	buffersrc := astiav.FindFilterByName("abuffer")
	buffersink := astiav.FindFilterByName("abuffersink")
	if buffersrc == nil || buffersink == nil {
		return nil, fmt.Errorf("找不到 audio buffer filter")
	}

	buffersrcParams := astiav.AllocBuffersrcFilterContextParameters()
	defer buffersrcParams.Free()
	buffersrcParams.SetChannelLayout(at.decCodecContext.ChannelLayout())
	buffersrcParams.SetSampleFormat(at.decCodecContext.SampleFormat())
	buffersrcParams.SetSampleRate(at.decCodecContext.SampleRate())
	buffersrcParams.SetTimeBase(at.decCodecContext.TimeBase())

	var err error
	if at.buffersrcCtx, err = at.filterGraph.NewBuffersrcFilterContext(buffersrc, "in"); err != nil {
		return nil, fmt.Errorf("创建 abuffersrc 失败: %w", err)
	}
	if at.buffersinkCtx, err = at.filterGraph.NewBuffersinkFilterContext(buffersink, "out"); err != nil {
		return nil, fmt.Errorf("创建 abuffersink 失败: %w", err)
	}

	if err := at.buffersrcCtx.SetParameters(buffersrcParams); err != nil {
		return nil, fmt.Errorf("设置 buffersrc 参数失败: %w", err)
	}
	if err := at.buffersrcCtx.Initialize(nil); err != nil {
		return nil, fmt.Errorf("初始化 buffersrc 失败: %w", err)
	}

	outputs.SetName("in")
	outputs.SetFilterContext(at.buffersrcCtx.FilterContext())
	outputs.SetPadIdx(0)
	outputs.SetNext(nil)

	inputs.SetName("out")
	inputs.SetFilterContext(at.buffersinkCtx.FilterContext())
	inputs.SetPadIdx(0)
	inputs.SetNext(nil)

	filterContent := fmt.Sprintf("asetnsamples=n=1024:p=0,aformat=sample_fmts=%s:channel_layouts=%s",
		at.encCodecContext.SampleFormat().Name(),
		at.encCodecContext.ChannelLayout().String())

	if err := at.filterGraph.Parse(filterContent, inputs, outputs); err != nil {
		return nil, fmt.Errorf("解析音频 filter 失败: %w", err)
	}
	if err := at.filterGraph.Configure(); err != nil {
		return nil, fmt.Errorf("配置音频 filter 失败: %w", err)
	}

	at.filterFrame = astiav.AllocFrame()
	closer.Add(at.filterFrame.Free)

	at.encPkt = astiav.AllocPacket()
	closer.Add(at.encPkt.Free)

	return at, nil
}

func (s *Slicer) createMuxer(videoInStream, audioInStream *astiav.Stream, sw *segmentWriter, audioEncCodecContext *astiav.CodecContext, closer *astikit.Closer) (*astiav.FormatContext, *astiav.IOContext, *astiav.Stream, *astiav.Stream, error) {
	muxCtx, err := astiav.AllocOutputFormatContext(nil, "mpegts", "")
	if err != nil || muxCtx == nil {
		return nil, nil, nil, nil, fmt.Errorf("分配 mpegts mux 上下文失败: %w", err)
	}

	videoOutStream := muxCtx.NewStream(nil)
	if videoOutStream == nil {
		muxCtx.Free()
		return nil, nil, nil, nil, fmt.Errorf("创建视频输出流失败")
	}
	if err := videoInStream.CodecParameters().Copy(videoOutStream.CodecParameters()); err != nil {
		muxCtx.Free()
		return nil, nil, nil, nil, fmt.Errorf("拷贝视频编码参数失败: %w", err)
	}
	videoOutStream.CodecParameters().SetCodecTag(0)

	var audioOutStream *astiav.Stream
	if audioInStream != nil {
		audioOutStream = muxCtx.NewStream(nil)
		if audioOutStream == nil {
			muxCtx.Free()
			return nil, nil, nil, nil, fmt.Errorf("创建音频输出流失败")
		}

		if s.config.AudioCodec != "copy" && audioEncCodecContext != nil {
			if err := audioOutStream.CodecParameters().FromCodecContext(audioEncCodecContext); err != nil {
				muxCtx.Free()
				return nil, nil, nil, nil, fmt.Errorf("设置转码音频参数失败: %w", err)
			}
			audioOutStream.SetTimeBase(audioEncCodecContext.TimeBase())
		} else {
			if err := audioInStream.CodecParameters().Copy(audioOutStream.CodecParameters()); err != nil {
				muxCtx.Free()
				return nil, nil, nil, nil, fmt.Errorf("拷贝音频编码参数失败: %w", err)
			}
			audioOutStream.CodecParameters().SetCodecTag(0)
		}
	}

	ioCtx, err := astiav.AllocIOContext(4096, true, nil, nil, sw.write)
	if err != nil {
		muxCtx.Free()
		return nil, nil, nil, nil, fmt.Errorf("分配 IO 上下文失败: %w", err)
	}

	muxCtx.SetPb(ioCtx)

	if err := muxCtx.WriteHeader(nil); err != nil {
		muxCtx.Free()
		ioCtx.Free()
		return nil, nil, nil, nil, fmt.Errorf("写 mux header 失败: %w", err)
	}

	return muxCtx, ioCtx, videoOutStream, audioOutStream, nil
}

func (s *Slicer) Stop() {
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
}

func (s *Slicer) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *Slicer) KeepAlive() {
	s.mu.Lock()
	s.lastAccess = time.Now()
	s.mu.Unlock()
}

func (s *Slicer) LastAccess() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastAccess
}

func (s *Slicer) GetStore() *ChannelStore {
	return s.store
}
