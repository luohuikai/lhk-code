package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gen2brain/malgo"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/restsend/rustpbxgo"
	"github.com/shenjinti/go711"
	"github.com/shenjinti/go722"
	"github.com/sirupsen/logrus"
)

// 定义客户端创建选项结构体
type CreateClientOption struct {
	Endpoint string         // 服务器的连接地址
	Logger   *logrus.Logger // 日志记录器
	SigChan  chan bool      // 信号通道
	// LLMHandler     *LLMHandler          // 大语言模型处理器
	// OpenaiKey      string               // OpenAI的API密钥
	// OpenaiEndpoint string               // OpenAI服务的接口地址
	// OpenaiModel    string               // 大语言模型名称
	// SystemPrompt   string               // 系统提示词
	BreakOnVad bool                 // 是否在语音活动检测（VAD）时中断 TTS 播报
	CallOption rustpbxgo.CallOption // 通话相关配置选项
}

// MediaHandler handles WebRTC and audio encoding
type MediaHandler struct {
	ctx            context.Context                // 管理上下文
	cancel         context.CancelFunc             // 管理取消操作
	logger         *logrus.Logger                 // 日志记录器
	peerConnection *webrtc.PeerConnection         // WebRTC对等连接对象
	audioTrack     *webrtc.TrackLocalStaticSample // 本地音频轨道对象
	buffer         []byte                         // 存储捕获的音频数据的缓冲区
	bufferMutex    sync.Mutex                     // 保护buffer的互斥锁
	connected      bool                           // 表示WebRTC连接是否已建立
	mu             sync.Mutex                     // 保护其他操作的互斥锁
	sequenceNumber uint16                         // RTP数据包的序列号
	timestamp      uint32                         // RTP数据包的时间戳
	playbackBuffer []byte                         // 存储播放的音频数据的缓冲区
	playbackMutex  *sync.Mutex                    // 保护playbackBuffer的互斥锁
	playbackDevice *malgo.Device                  // 音频播放设备对象
	playbackCtx    *malgo.AllocatedContext        // 音频播放上下文对象
	captureDevice  *malgo.Device                  // 音频捕获设备对象
}

// 创建客户端
func createClient(ctx context.Context, option CreateClientOption, id string, callOption rustpbxgo.CallOption) *rustpbxgo.Client {
	//创建客户端对象
	client := rustpbxgo.NewClient(option.Endpoint,
		rustpbxgo.WithLogger(option.Logger),
		rustpbxgo.WithContext(ctx),
		rustpbxgo.WithID(id),
	)

	// 绑定事件处理函数
	// 连接关闭，记录日志并通知主程序退出
	client.OnClose = func(reason string) {
		option.Logger.Infof("Connection closed: %s", reason)
		option.SigChan <- true
	}
	// 收到事件：记录事件日志
	client.OnEvent = func(event string, payload string) {
		option.Logger.Debugf("Received event: %s %s", event, payload)
	}
	// 发生错误：记录错误日志
	client.OnError = func(event rustpbxgo.ErrorEvent) {
		option.Logger.Errorf("Error: %v", event)
	}
	// 收到DTMF（按键音）：记录按键信息
	client.OnDTMF = func(event rustpbxgo.DTMFEvent) {
		option.Logger.Infof("DTMF: %s", event.Digit)
	}
	// 收到语音识别最终结果
	client.OnAsrFinal = func(event rustpbxgo.AsrFinalEvent) {
		// 保存对话历史
		if event.Text != "" {
			client.History("user", event.Text)
		}

		// 打断TTS
		client.Interrupt()
		startTime := time.UnixMilli(int64(*event.StartTime))
		endTime := time.UnixMilli(int64(*event.EndTime))
		option.Logger.Debugf("ASR Delta: %s startTime: %s endTime: %s", event.Text, startTime.String(), endTime.String())
		if event.Text == "" {
			return
		}

		// 显示用户讲话内容
		option.Logger.Infof("User said: %s", event.Text)

		// 调用 TTS 讲出内容
		ttsCmd := rustpbxgo.TtsCommand{
			Command: "tts",
			Text:    event.Text,
			Speaker: callOption.TTS.Speaker,
		}
		ttsData, err := json.Marshal(ttsCmd)
		if err != nil {
			option.Logger.Errorf("Failed to marshal TTS command: %v", err)
			return
		}
		err = client.GetConn().WriteMessage(websocket.TextMessage, ttsData)
		if err != nil {
			option.Logger.Errorf("Failed to send TTS command: %v", err)
		}
	}
	// 收到语音识别中间结果：根据配置决定是否打断TTS
	client.OnAsrDelta = func(event rustpbxgo.AsrDeltaEvent) {
		startTime := time.UnixMilli(int64(*event.StartTime))
		endTime := time.UnixMilli(int64(*event.EndTime))
		option.Logger.Debugf("ASR Delta: %s startTime: %s endTime: %s", event.Text, startTime.String(), endTime.String())
		if option.BreakOnVad {
			return
		}
		if err := client.Interrupt(); err != nil {
			option.Logger.Warnf("Failed to interrupt TTS: %v", err)
		}
	}
	// 检测到用户说话：根据配置决定是否打断TTS
	client.OnSpeaking = func(event rustpbxgo.SpeakingEvent) {
		option.Logger.Infof("Speaking...")
		if !option.BreakOnVad {
			return
		}
		option.Logger.Infof("Interrupting TTS")
		if err := client.Interrupt(); err != nil {
			option.Logger.Warnf("Failed to interrupt TTS: %v", err)
		}
	}

	return client
}

// NewMediaHandler creates a new media handler
func NewMediaHandler(ctx context.Context, logger *logrus.Logger) (*MediaHandler, error) {
	// 创建一个可取消的上下文
	ctx, cancel := context.WithCancel(ctx)

	// 初始化音频播放上下文
	playbackCtx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize playback context: %w", err)
	}

	// 返回一个新的MediaHandler实例
	return &MediaHandler{
		ctx:            ctx,
		cancel:         cancel,
		logger:         logger,
		buffer:         make([]byte, 0, 16000), // Buffer for 1 second of audio at 16kHz
		sequenceNumber: 0,
		timestamp:      0,
		playbackCtx:    playbackCtx,
	}, nil
}

// Setup 函数用于设置 WebRTC 连接，创建一个 offer 并设置为本地描述，等待 ICE 收集完成，最后返回 offer 的 SDP 信息
func (mh *MediaHandler) Setup(codec string, iceServers []webrtc.ICEServer) (string, error) {
	mediaEngine := webrtc.MediaEngine{}
	var codecParams webrtc.RTPCodecParameters
	// 根据传入的 codec 参数选择合适的音频编解码器
	if codec == "g722" {
		codecParams = webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{
				MimeType:  webrtc.MimeTypeG722,
				ClockRate: 8000,
			},
			PayloadType: 9,
		}
	} else {
		codecParams = webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{
				MimeType:  webrtc.MimeTypePCMU,
				ClockRate: 8000,
			},
			PayloadType: 0,
		}
	}

	// 注册音频编解码器到 mediaEngine
	mediaEngine.RegisterCodec(codecParams, webrtc.RTPCodecTypeAudio)

	// 创建一个新的 WebRTC 对等连接
	api := webrtc.NewAPI(webrtc.WithMediaEngine(&mediaEngine))
	peerConnection, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers: iceServers,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create peer connection: %w", err)
	}
	mh.peerConnection = peerConnection

	// 创建一个本地音频轨道并添加到对等连接中
	audioTrack, err := webrtc.NewTrackLocalStaticSample(codecParams.RTPCodecCapability, "rustpbxgo-audio", "rustpbxgo-audio")
	if err != nil {
		return "", fmt.Errorf("failed to create audio track: %w", err)
	}
	// Add track to peer connection
	_, err = peerConnection.AddTrack(audioTrack)
	if err != nil {
		return "", fmt.Errorf("failed to add track to peer connection: %w", err)
	}
	mh.audioTrack = audioTrack
	// 处理远程音频轨道的添加事件，解码接收到的 RTP 数据包并添加到播放缓冲区
	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		mh.logger.Infof("Track remote added %v %s", track.ID(), track.Codec().MimeType)
		g722Decoder := go722.NewG722Decoder(go722.Rate64000, 0)
		go func() {
			for mh.connected {
				if mh.ctx.Err() != nil {
					return
				}
				rtpPacket, _, err := track.ReadRTP()
				if err != nil {
					mh.logger.Errorf("Failed to read RTP packet: %v", err)
					break
				}
				var audioData []byte
				if codec == "g722" {
					audioData = g722Decoder.Decode(rtpPacket.Payload)
				} else {
					audioData, _ = go711.DecodePCMU(rtpPacket.Payload)
				}
				// Add to playback buffer
				mh.playbackMutex.Lock()
				mh.playbackBuffer = append(mh.playbackBuffer, audioData...)
				mh.playbackMutex.Unlock()
			}
		}()
	})

	// 处理对等连接状态变化、ICE 收集状态变化、ICE 候选和 ICE 连接状态变化事件
	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		mh.logger.Infof("Peer connection state: %v", state)
		if state == webrtc.PeerConnectionStateConnected {
			mh.connected = true
			mh.initPlaybackDevice(codec)
			mh.startAudioCapture(codec)
			go mh.encodeAndSendAudio(codec)
		}
	})
	peerConnection.OnICEGatheringStateChange(func(state webrtc.ICEGathererState) {
		mh.logger.Infof("ICE gathering state: %v", state)
	})
	peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		mh.logger.Infof("ICE candidate: %v", candidate)
	})
	peerConnection.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		mh.logger.Infof("ICE connection state: %v", state)
	})

	// 创建一个 offer 并设置为本地描述
	offer, err := peerConnection.CreateOffer(nil)
	if err != nil {
		return "", fmt.Errorf("failed to create offer: %w", err)
	}
	peerConnection.SetLocalDescription(offer)

	// 等待 ICE 收集完成或超时
	select {
	case <-webrtc.GatheringCompletePromise(peerConnection):
		mh.logger.Info("ICE Gathering complete")
	case <-time.After(20 * time.Second):
		mh.logger.Warn("ICE Gathering timeout")
		return "", fmt.Errorf("gathering timeout")
	}

	// 返回 offer 的 SDP 信息
	offerSdp := peerConnection.LocalDescription().SDP
	return offerSdp, nil
}

// SetupAnswer 函数用于设置远程描述，接收一个 SDP 答案并将其设置为对等连接的远程描述
func (mh *MediaHandler) SetupAnswer(answer string) error {
	remoteOffer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answer,
	}
	return mh.peerConnection.SetRemoteDescription(remoteOffer)
}

// initPlaybackDevice 函数用于初始化音频播放设备
func (mh *MediaHandler) initPlaybackDevice(codec string) error {
	// 根据 codec 参数设置设备配置
	deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	deviceConfig.Playback.Format = malgo.FormatS16
	deviceConfig.Playback.Channels = 1
	deviceConfig.SampleRate = 8000
	deviceConfig.Alsa.NoMMap = 1
	if codec == "g722" {
		deviceConfig.SampleRate = 16000
	}

	// 创建一个播放缓冲区和互斥锁
	mh.playbackBuffer = make([]byte, 0, 16000)
	mh.playbackMutex = &sync.Mutex{}

	// 使用 malgo.InitDevice 
	// 处理设备的数据回调函数，将播放缓冲区的数据复制到输出样本中v
	playbackDevice, err := malgo.InitDevice(mh.playbackCtx.Context, deviceConfig, malgo.DeviceCallbacks{
		Data: func(outputSamples, inputSamples []byte, frameCount uint32) {
			if !mh.connected {
				return
			}
			mh.playbackMutex.Lock()
			n := copy(outputSamples, mh.playbackBuffer)
			mh.playbackBuffer = mh.playbackBuffer[n:]
			mh.playbackMutex.Unlock()
		},
	})
	if err != nil {
		return fmt.Errorf("failed to initialize playback device: %w", err)
	}
	mh.playbackDevice = playbackDevice
	mh.logger.Info("Playback device initialized")

	// 启动播放设备
	return playbackDevice.Start()
}

// startAudioCapture 函数用于初始化音频捕获设备
func (mh *MediaHandler) startAudioCapture(codec string) error {
	// 根据 codec 参数设置设备配置
	deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceConfig.Capture.Format = malgo.FormatS16
	deviceConfig.Capture.Channels = 1
	deviceConfig.SampleRate = 8000
	deviceConfig.Alsa.NoMMap = 1
	if codec == "g722" {
		deviceConfig.SampleRate = 16000
	}

	// 使用 malgo.InitDevice 初始化捕获设备
	// 处理设备的数据回调函数，将输入样本添加到捕获缓冲区
	captureDevice, err := malgo.InitDevice(mh.playbackCtx.Context, deviceConfig, malgo.DeviceCallbacks{
		Data: func(outputSamples, inputSamples []byte, frameCount uint32) {
			if !mh.connected {
				return
			}
			mh.bufferMutex.Lock()
			mh.buffer = append(mh.buffer, inputSamples...)
			mh.bufferMutex.Unlock()
		},
	})
	if err != nil {
		return fmt.Errorf("failed to initialize capture device: %v", err)
	}
	mh.captureDevice = captureDevice

	// 启动捕获设备
	err = captureDevice.Start()
	if err != nil {
		return fmt.Errorf("failed to start capture device: %v", err)
	}
	mh.logger.Info("Capture device initialized")
	return nil
}

// encodeAndSendAudio 函数用于编码音频数据并通过 WebRTC 发送
func (mh *MediaHandler) encodeAndSendAudio(codec string) {
	// 使用 time.NewTicker 创建一个定时器，每 20 毫秒触发一次
	ticker := time.NewTicker(20 * time.Millisecond)
	// 根据采样率计算 20 毫秒的音频数据帧大小
	framesize := int(20 * int(mh.captureDevice.SampleRate()) / 1000 * 2)
	// 创建一个 G.722 编码器
	g722Encoder := go722.NewG722Encoder(go722.Rate64000, 0)
	// 循环检查是否连接，处理定时器事件或上下文取消事件
	for mh.connected {
		select {
		case <-ticker.C:
		case <-mh.ctx.Done():
			return
		}
		mh.bufferMutex.Lock()
		if len(mh.buffer) < framesize { // 20ms at 8khz
			mh.bufferMutex.Unlock()
			continue
		}

		// 从捕获缓冲区中取出足够的音频数据
		audioData := mh.buffer[:framesize]
		mh.buffer = mh.buffer[framesize:]
		mh.bufferMutex.Unlock()
		var payload []byte
		// 根据 codec 参数选择合适的编码器进行编码
		if codec == "g722" {
			payload = g722Encoder.Encode(audioData)
		} else {
			payload, _ = go711.EncodePCMU(audioData)
		}
		// 创建一个媒体样本
		sample := media.Sample{
			Data:      payload,
			Duration:  20 * time.Millisecond,
			Timestamp: time.Now(),
		}
		// 通过本地音频轨道发送
		err := mh.audioTrack.WriteSample(sample)
		if err != nil {
			mh.logger.Errorf("Failed to send audio sample: %v", err)
			continue
		}
	}
}

// Stop 函数用于停止媒体处理程序
func (mh *MediaHandler) Stop() error {
	// 加锁以确保线程安全
	mh.mu.Lock()
	defer mh.mu.Unlock()

	if !mh.connected {
		return nil
	}
	// 取消上下文
	mh.cancel()
	// 停止并释放播放设备、捕获设备和播放上下文
	if mh.playbackDevice != nil {
		mh.playbackDevice.Stop()
		mh.playbackDevice.Uninit()
	}
	if mh.captureDevice != nil {
		mh.captureDevice.Stop()
		mh.captureDevice.Uninit()
	}
	if mh.playbackCtx != nil {
		mh.playbackCtx.Uninit()
	}
	// 关闭 WebRTC 对等连接
	if mh.peerConnection != nil {
		mh.peerConnection.Close()
	}

	// 将连接状态设置为 false
	mh.connected = false
	// 记录停止信息并返回 nil
	mh.logger.Info("Media handler stopped")
	return nil
}
