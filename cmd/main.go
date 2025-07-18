package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/pion/webrtc/v3"
	"github.com/restsend/rustpbxgo"
	"github.com/sirupsen/logrus"
)

func main() {
	// 初始化设置
	config, err := LoadConfig()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer config.Cancel()

	// 构建 RustpbxGo 客户端的所有配置选项
	// 处理信号优雅关闭
	sigChan := make(chan bool) // 创建一个用于传递布尔类型数据的通道
	// 给结构体实例赋值
	option := CreateClientOption{
		Endpoint:   config.Endpoint,
		Logger:     config.Logger,
		SigChan:    sigChan,
		BreakOnVad: config.BreakOnVad,
	}
	var recorder *rustpbxgo.RecorderOption
	if config.Record {
		recorder = &rustpbxgo.RecorderOption{
			Samplerate: 16000,
		}
	}
	callOption := rustpbxgo.CallOption{
		Recorder: recorder,
		Denoise:  true,
		VAD: &rustpbxgo.VADOption{
			Type:      config.VADModel,
			Endpoint:  config.VADEndpoint,
			SecretKey: config.VADSecretKey,
		},
		ASR: &rustpbxgo.ASROption{
			Provider:  config.ASRProvider,
			Endpoint:  config.ASREndpoint,
			AppID:     config.ASRAppID,
			SecretID:  config.ASRSecretID,
			SecretKey: config.ASRSecretKey,
			ModelType: config.ASRModelType,
		},
		TTS: &rustpbxgo.TTSOption{
			Provider:  config.TTSProvider,
			Speaker:   config.Speaker,
			Endpoint:  config.TTSEndpoint,
			AppID:     config.TTSAppID,
			SecretID:  config.TTSSecretID,
			SecretKey: config.TTSSecretKey,
		},
	}
	option.CallOption = callOption

	
	// 媒体处理器初始化
	// 创建媒体处理器，用于管理音频流和 SDP 协议
	mediaHandler, err := NewMediaHandler(config.Ctx, config.Logger)
	if err != nil {
		config.Logger.Fatalf("Failed to create media handler: %v", err)
	}
	defer mediaHandler.Stop()

	callType := "webrtc"
	var iceSevers []webrtc.ICEServer
	// 获取 ICE 服务器列表并协商 SDP，准备 WebRTC 通话
	iceUrl := "https://"
	if !config.EndpointSecurity {
		iceUrl = "http://"
	}
	iceUrl = fmt.Sprintf("%s%s/icesevers", iceUrl, config.EndpointHost)
	resp, err := http.Get(iceUrl)
	if err == nil {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		json.Unmarshal(body, &iceSevers)
		config.Logger.WithFields(logrus.Fields{
			"iceservers": len(iceSevers),
		}).Info("get iceservers")
	}

	// 解析 SDP
	localSdp, err := mediaHandler.Setup(config.Codec, iceSevers)
	if err != nil {
		config.Logger.Fatalf("Failed to get local SDP: %v", err)
	}
	config.Logger.Infof("Offer SDP: %v", localSdp)
	callOption.Offer = localSdp

	// 创建 RustpbxGo 客户端连接服务器，通话结束后自动关闭
	client := createClient(config.Ctx, option, "", callOption)
	// 连接服务器
	err = client.Connect(callType)
	if err != nil {
		config.Logger.Fatalf("Failed to connect to server: %v", err)
	}
	defer client.Shutdown()

	// 发起通话请求，收到服务器应答后进行 SDP 协商（WebRTC）
	answer, err := client.Invite(config.Ctx, callOption)
	if err != nil {
		config.Logger.Fatalf("Failed to invite: %v", err)
	}
	config.Logger.Infof("Answer SDP: %v", answer.Sdp)

	// ICE服务器获取与SDP协商
	err = mediaHandler.SetupAnswer(answer.Sdp)
	if err != nil {
		config.Logger.Fatalf("Failed to setup answer: %v", err)
	}

	<-sigChan
	fmt.Println("Shutting down...")
}
