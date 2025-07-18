package main

import (
	"context"
	"encoding/json"
	// "flag"
	"fmt"
	"io"
	"net/http"
	// "net/url"
	"os"
	// "strings"
	"time"

	"github.com/gorilla/websocket"
	// "github.com/joho/godotenv"
	"github.com/pion/webrtc/v3"
	"github.com/restsend/rustpbxgo"
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

		// // startTime = time.Now()
		// segment := "text"
		// playID := "playID"
		// autoHangup := false

		// client.TTS(segment, "", playID, autoHangup, nil)
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

func main() {
	// 初始化设置
	config, err := LoadConfig()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	// 客户端选项和通话参数构建
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


// 加载 .env 环境变量文件
	// godotenv.Load()
	// // 命令行设置
	// // ws://175.27.250.177:8080
	// var endpoint string = "ws://192.168.1.134:8080"
	// var codec string = "g722"
	// var breakOnVad bool = false
	// var speaker string = "601003"
	// var record bool = false
	// var ttsProvider string = "tencent"
	// var asrProvider string = "tencent"
	// var asrEndpoint string = "asr.tencentcloudapi.com"
	// var asrAppID string = "1369156880"
	// var asrSecretID string = "YOUR_TENCENT_SECRET_ID"
	// var asrSecretKey string = "YOUR_TENCENT_SECRET_KEY"
	// var asrModelType string = "16k_zh"
	// var ttsEndpoint string = "tts.tencentcloudapi.com"
	// var ttsAppID string = "1369156880"
	// var ttsSecretID string = "YOUR_TENCENT_SECRET_ID"
	// var ttsSecretKey string = "YOUR_TENCENT_SECRET_KEY"
	// var vadModel string = "silero"
	// var vadEndpoint string = ""
	// var vadSecretKey string = ""

	// // 解析命令行参数，初始化各类变量
	// // 作用：加载.env文件中的环境变量，并通过命令行参数或默认值初始化所有配置项
	// flag.StringVar(&endpoint, "endpoint", endpoint, "Endpoint to connect to")
	// flag.StringVar(&codec, "codec", codec, "Codec to use: g722, pcmu")
	// flag.BoolVar(&breakOnVad, "break-on-vad", breakOnVad, "Break on VAD")
	// flag.BoolVar(&record, "record", record, "Record the call")
	// flag.StringVar(&ttsProvider, "tts", ttsProvider, "TTS provider to use: tencent, voiceapi")
	// flag.StringVar(&asrProvider, "asr", asrProvider, "ASR provider to use: tencent, voiceapi")
	// flag.StringVar(&speaker, "speaker", speaker, "Speaker to use")
	// flag.StringVar(&asrEndpoint, "asr-endpoint", asrEndpoint, "ASR endpoint to use")
	// flag.StringVar(&asrModelType, "asr-model-type", asrModelType, "ASR model type to use")
	// flag.StringVar(&asrAppID, "asr-app-id", asrAppID, "ASR app id to use")
	// flag.StringVar(&asrSecretID, "asr-secret-id", asrSecretID, "ASR secret id to use")
	// flag.StringVar(&asrSecretKey, "asr-secret-key", asrSecretKey, "ASR secret key to use")
	// flag.StringVar(&ttsEndpoint, "tts-endpoint", ttsEndpoint, "TTS endpoint to use")
	// flag.StringVar(&ttsAppID, "tts-app-id", ttsAppID, "TTS app id to use")
	// flag.StringVar(&ttsSecretID, "tts-secret-id", ttsSecretID, "TTS secret id to use")
	// flag.StringVar(&ttsSecretKey, "tts-secret-key", ttsSecretKey, "TTS secret key to use")
	// flag.StringVar(&vadModel, "vad-model", vadModel, "VAD model to use")
	// flag.StringVar(&vadEndpoint, "vad-endpoint", vadEndpoint, "VAD endpoint to use")
	// flag.StringVar(&vadSecretKey, "vad-secret-key", vadSecretKey, "VAD secret key to use")

	// flag.Parse()                  // 解析命令行参数
	// u, err := url.Parse(endpoint) // 解析URL字符串
	// if err != nil {
	// 	fmt.Printf("Failed to prase endpoint: %v", err)
	// 	os.Exit(1)
	// }
	// endpointHost := u.Host                                 // 提取主机名
	// endpointSecurity := strings.ToLower(u.Scheme) == "wss" // 判断是否使用安全协议wss

	// // 创建日志记录器和可取消的上下文对象，方便后续日志输出和优雅退出
	// logger := logrus.New()            // 创建一个新的 logrus.Logger 实例
	// logger.SetOutput(os.Stdout)       // 设置日志的输出目标为 标准输出（控制台）
	// logger.SetLevel(logrus.InfoLevel) // 设置日志的 最低记录级别（只有等于或高于该级别的日志会被记录）

	// // 创建上下文取消
	// ctx, cancel := context.WithCancel(context.Background())
	// defer cancel()