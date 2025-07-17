package main

import (
	"context"
	"time"
	"testing"

	"github.com/pion/webrtc/v3"
	"github.com/sirupsen/logrus"
)

// 测试 NewMediaHandler 函数
func TestNewMediaHandler(t *testing.T) {
	ctx := context.Background()
	logger := logrus.New()
	handler, err := NewMediaHandler(ctx, logger)
	if err != nil {
		t.Errorf("NewMediaHandler returned an error: %v", err)
	}
	if handler == nil {
		t.Errorf("NewMediaHandler returned nil")
	}
}

// 测试 Setup 函数
func TestMediaHandler_Setup(t *testing.T) {
	ctx := context.Background()
	logger := logrus.New()
	handler, err := NewMediaHandler(ctx, logger)
	if err != nil {
		t.Fatalf("NewMediaHandler returned an error: %v", err)
	}

	iceServers := []webrtc.ICEServer{
		{
			URLs: []string{"stun:stun.l.google.com:19302"},
		},
	}

	offerSdp, err := handler.Setup("g722", iceServers)
	if err != nil {
		t.Errorf("Setup returned an error: %v", err)
	}
	if offerSdp == "" {
		t.Errorf("Setup returned an empty offer SDP")
	}
}

// 测试 SetupAnswer 函数
func TestMediaHandler_SetupAnswer(t *testing.T) {
	ctx := context.Background()
	logger := logrus.New()
	handler, err := NewMediaHandler(ctx, logger)
	if err != nil {
		t.Fatalf("NewMediaHandler returned an error: %v", err)
	}

	iceServers := []webrtc.ICEServer{
		{
			URLs: []string{"stun:stun.l.google.com:19302"},
		},
	}

	// 生成 offer
	offerSdp, err := handler.Setup("g722", iceServers)
	if err != nil {
		t.Fatalf("Setup returned an error: %v", err)
	}

	// 创建一个模拟的远程 PeerConnection 来生成 answer
	mediaEngine := webrtc.MediaEngine{}
	codecParams := webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeG722,
			ClockRate: 8000,
		},
		PayloadType: 9,
	}
	mediaEngine.RegisterCodec(codecParams, webrtc.RTPCodecTypeAudio)

	api := webrtc.NewAPI(webrtc.WithMediaEngine(&mediaEngine))
	remotePeerConnection, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers: iceServers,
	})
	if err != nil {
		t.Fatalf("Failed to create remote peer connection: %v", err)
	}
	defer remotePeerConnection.Close()

	// 设置远程 offer
	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offerSdp,
	}
	err = remotePeerConnection.SetRemoteDescription(offer)
	if err != nil {
		t.Fatalf("Failed to set remote offer: %v", err)
	}

	// 创建 answer
	answer, err := remotePeerConnection.CreateAnswer(nil)
	if err != nil {
		t.Fatalf("Failed to create answer: %v", err)
	}
	err = remotePeerConnection.SetLocalDescription(answer)
	if err != nil {
		t.Fatalf("Failed to set local answer: %v", err)
	}

	// 等待 ICE 收集完成
	select {
	case <-webrtc.GatheringCompletePromise(remotePeerConnection):
		logger.Info("Remote ICE Gathering complete")
	case <-time.After(20 * time.Second):
		logger.Warn("Remote ICE Gathering timeout")
		t.Fatalf("Remote gathering timeout")
	}

	// 获取 answer SDP
	answerSdp := remotePeerConnection.LocalDescription().SDP

	// 设置 answer
	err = handler.SetupAnswer(answerSdp)
	if err != nil {
		t.Errorf("SetupAnswer returned an error: %v", err)
	}
}

// 测试 Stop 函数
func TestMediaHandler_Stop(t *testing.T) {
	ctx := context.Background()
	logger := logrus.New()
	handler, err := NewMediaHandler(ctx, logger)
	if err != nil {
		t.Fatalf("NewMediaHandler returned an error: %v", err)
	}

	err = handler.Stop()
	if err != nil {
		t.Errorf("Stop returned an error: %v", err)
	}
}