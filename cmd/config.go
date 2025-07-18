package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
)

// Config 定义配置结构体
type Config struct {
	Endpoint         string
	Codec            string
	BreakOnVad       bool
	Speaker          string
	Record           bool
	TTSProvider      string
	ASRProvider      string
	ASREndpoint      string
	ASRAppID         string
	ASRSecretID      string
	ASRSecretKey     string
	ASRModelType     string
	TTSEndpoint      string
	TTSAppID         string
	TTSSecretID      string
	TTSSecretKey     string
	VADModel         string
	VADEndpoint      string
	VADSecretKey     string
	EndpointHost     string
	EndpointSecurity bool
	Logger           *logrus.Logger
	Ctx              context.Context
	Cancel           context.CancelFunc
}

func LoadConfig() (*Config, error) {
	// 加载 .env 环境变量文件
	godotenv.Load()

	// 命令行设置
	// ws://175.27.250.177:8080
	var endpoint string = "ws://175.27.250.177:8080"
	var codec string = "g722"
	var breakOnVad bool = false
	var speaker string = "601003"
	var record bool = false
	var ttsProvider string = "tencent"
	var asrProvider string = "tencent"
	var asrEndpoint string = "asr.tencentcloudapi.com"
	var asrAppID string = "1369156880"
	var asrSecretID string = "YOUR_TENCENT_SECRET_ID"
	var asrSecretKey string = "YOUR_TENCENT_SECRET_KEY"
	var asrModelType string = "16k_zh"
	var ttsEndpoint string = "tts.tencentcloudapi.com"
	var ttsAppID string = "1369156880"
	var ttsSecretID string = "YOUR_TENCENT_SECRET_ID"
	var ttsSecretKey string = "YOUR_TENCENT_SECRET_KEY"
	var vadModel string = "silero"
	var vadEndpoint string = ""
	var vadSecretKey string = ""

	// 解析命令行参数，初始化各类变量
	// 作用：加载.env文件中的环境变量，并通过命令行参数或默认值初始化所有配置项
	flag.StringVar(&endpoint, "endpoint", endpoint, "Endpoint to connect to")
	flag.StringVar(&codec, "codec", codec, "Codec to use: g722, pcmu")
	flag.BoolVar(&breakOnVad, "break-on-vad", breakOnVad, "Break on VAD")
	flag.BoolVar(&record, "record", record, "Record the call")
	flag.StringVar(&ttsProvider, "tts", ttsProvider, "TTS provider to use: tencent, voiceapi")
	flag.StringVar(&asrProvider, "asr", asrProvider, "ASR provider to use: tencent, voiceapi")
	flag.StringVar(&speaker, "speaker", speaker, "Speaker to use")
	flag.StringVar(&asrEndpoint, "asr-endpoint", asrEndpoint, "ASR endpoint to use")
	flag.StringVar(&asrModelType, "asr-model-type", asrModelType, "ASR model type to use")
	flag.StringVar(&asrAppID, "asr-app-id", asrAppID, "ASR app id to use")
	flag.StringVar(&asrSecretID, "asr-secret-id", asrSecretID, "ASR secret id to use")
	flag.StringVar(&asrSecretKey, "asr-secret-key", asrSecretKey, "ASR secret key to use")
	flag.StringVar(&ttsEndpoint, "tts-endpoint", ttsEndpoint, "TTS endpoint to use")
	flag.StringVar(&ttsAppID, "tts-app-id", ttsAppID, "TTS app id to use")
	flag.StringVar(&ttsSecretID, "tts-secret-id", ttsSecretID, "TTS secret id to use")
	flag.StringVar(&ttsSecretKey, "tts-secret-key", ttsSecretKey, "TTS secret key to use")
	flag.StringVar(&vadModel, "vad-model", vadModel, "VAD model to use")
	flag.StringVar(&vadEndpoint, "vad-endpoint", vadEndpoint, "VAD endpoint to use")
	flag.StringVar(&vadSecretKey, "vad-secret-key", vadSecretKey, "VAD secret key to use")

	flag.Parse()                  // 解析命令行参数
	u, err := url.Parse(endpoint) // 解析URL字符串
	if err != nil {
		fmt.Printf("Failed to prase endpoint: %v", err)
		os.Exit(1)
	}
	endpointHost := u.Host                                 // 提取主机名
	endpointSecurity := strings.ToLower(u.Scheme) == "wss" // 判断是否使用安全协议wss

	// 创建日志记录器和可取消的上下文对象，方便后续日志输出和优雅退出
	logger := logrus.New()            // 创建一个新的 logrus.Logger 实例
	logger.SetOutput(os.Stdout)       // 设置日志的输出目标为 标准输出（控制台）
	logger.SetLevel(logrus.InfoLevel) // 设置日志的 最低记录级别（只有等于或高于该级别的日志会被记录）

	// 创建上下文取消
	ctx, cancel := context.WithCancel(context.Background())

	config := &Config{
		Endpoint:         endpoint,
		Codec:            codec,
		BreakOnVad:       breakOnVad,
		Speaker:          speaker,
		Record:           record,
		TTSProvider:      ttsProvider,
		ASRProvider:      asrProvider,
		ASREndpoint:      asrEndpoint,
		ASRAppID:         asrAppID,
		ASRSecretID:      asrSecretID,
		ASRSecretKey:     asrSecretKey,
		ASRModelType:     asrModelType,
		TTSEndpoint:      ttsEndpoint,
		TTSAppID:         ttsAppID,
		TTSSecretID:      ttsSecretID,
		TTSSecretKey:     ttsSecretKey,
		VADModel:         vadModel,
		VADEndpoint:      vadEndpoint,
		VADSecretKey:     vadSecretKey,
		EndpointHost:     endpointHost,
		EndpointSecurity: endpointSecurity,
		Logger:           logger,
		Ctx:              ctx,
		Cancel:           cancel,
	}

	return config, nil
}
