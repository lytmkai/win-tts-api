// main.go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

// 请求结构体（支持 JSON）
type TTSRequest struct {
	Text string `json:"text"`
}

func speakText(text string) error {
	// 每次调用独立初始化 COM（线程安全需注意，此处简单处理）
	err := ole.CoInitialize(0)
	if err != nil {
		return fmt.Errorf("COM 初始化失败: %v", err)
	}
	defer ole.CoUninitialize()

	unknown, err := oleutil.CreateObject("SAPI.SpVoice")
	if err != nil {
		return fmt.Errorf("创建 SpVoice 失败: %v", err)
	}
	voice, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return fmt.Errorf("QueryInterface 失败: %v", err)
	}
	defer voice.Release()

	// 阻塞直到语音播放完成（SAPI 默认同步）
	_, err = oleutil.CallMethod(voice, "Speak", text)
	if err != nil {
		return fmt.Errorf("TTS Speak 失败: %v", err)
	}

	return nil
}

func ttsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "仅支持 POST 方法", http.StatusMethodNotAllowed)
		return
	}

	var text string

	// 支持两种格式：application/json 和 application/x-www-form-urlencoded
	contentType := r.Header.Get("Content-Type")

	switch {
	case strings.Contains(contentType, "application/json"):
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "读取请求体失败", http.StatusBadRequest)
			return
		}
		var req TTSRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "无效的 JSON 格式", http.StatusBadRequest)
			return
		}
		text = req.Text

	case strings.Contains(contentType, "application/x-www-form-urlencoded"):
		if err := r.ParseForm(); err != nil {
			http.Error(w, "解析表单失败", http.StatusBadRequest)
			return
		}
		text = r.FormValue("text")

	default:
		http.Error(w, "不支持的内容类型，请使用 JSON 或表单", http.StatusUnsupportedMediaType)
		return
	}

	// 校验文本
	text = strings.TrimSpace(text)
	if text == "" {
		http.Error(w, "text 字段不能为空", http.StatusBadRequest)
		return
	}
	if len(text) > 500 {
		http.Error(w, "文本长度不能超过 500 字符", http.StatusBadRequest)
		return
	}

	// 调用 TTS
	log.Printf("正在朗读: %q", text)
	err := speakText(text)
	if err != nil {
		log.Printf("TTS 错误: %v", err)
		http.Error(w, "TTS 执行失败，请检查系统语音设置", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "success",
		"msg":    "已开始朗读",
	})
}

func main() {
	http.HandleFunc("/tts", ttsHandler)

	fmt.Println("🚀 Windows 离线 TTS 服务已启动")
	fmt.Println("📌 监听地址: http://localhost:5555/tts")
	fmt.Println("📝 支持 POST，内容类型：application/json 或 application/x-www-form-urlencoded")
	fmt.Println("💡 示例（JSON）:")
	fmt.Println(`   curl -X POST http://localhost:5555/tts -H "Content-Type: application/json" -d '{"text":"你好，世界！"}'`)
	fmt.Println("💡 示例（表单）:")
	fmt.Println(`   curl -X POST http://localhost:5555/tts -d "text=欢迎使用 Go TTS"`)

	log.Fatal(http.ListenAndServe(":5555", nil))
}
