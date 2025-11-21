package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"github.com/spf13/pflag"
)

type Config struct {
	Broker   string
	Topic    string
	Username string
	Password string
}

var f mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
	payload := string(msg.Payload())
	log.Printf("收到 MQTT 消息 [主题: %s]: %s", msg.Topic(), payload)

	var text string
	var j struct{ Text string `json:"text"` }
	if err := json.Unmarshal([]byte(payload), &j); err == nil && j.Text != "" {
		text = j.Text
	} else {
		text = payload
	}

	text = strings.TrimSpace(text)
	if text == "" || len(text) > 500 {
		log.Println("⚠️ 文本为空或过长，跳过朗读")
		return
	}

	if err := speakText(text); err != nil {
		log.Printf("❌ TTS 错误: %v", err)
	} else {
		log.Printf("✅ 已完成朗读: %q", text)
	}
}

func speakText(text string) error {
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

	_, err = oleutil.CallMethod(voice, "Speak", text)
	return err
}

func loadConfigFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("无法读取配置文件 %q: %w", path, err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("配置文件 %q 不是有效的 JSON: %w", path, err)
	}

	// 手动提取字段（避免结构体零值覆盖）
	cfg := &Config{}
	if v, ok := raw["broker"]; ok {
		if s, ok := v.(string); ok {
			cfg.Broker = s
		}
	}
	if v, ok := raw["topic"]; ok {
		if s, ok := v.(string); ok {
			cfg.Topic = s
		}
	}
	if v, ok := raw["username"]; ok {
		if s, ok := v.(string); ok {
			cfg.Username = s
		}
	}
	if v, ok := raw["password"]; ok {
		if s, ok := v.(string); ok {
			cfg.Password = s
		}
	}
	return cfg, nil
}

func main() {
	var (
		configFile string
		broker     string
		topic      string
		username   string
		password   string
		showHelp   bool
	)

	pflag.StringVarP(&configFile, "config", "c", "", "可选：JSON 配置文件路径（不指定则不加载）")
	pflag.StringVarP(&broker, "broker", "b", "", "MQTT Broker 地址 (e.g. tcp://localhost:1883)")
	pflag.StringVarP(&topic, "topic", "t", "", "订阅的主题")
	pflag.StringVarP(&username, "username", "u", "", "MQTT 用户名")
	pflag.StringVarP(&password, "password", "p", "", "MQTT 密码")
	pflag.BoolVarP(&showHelp, "help", "h", false, "显示帮助")
	pflag.Parse()

	if showHelp {
		pflag.Usage()
		os.Exit(0)
	}

	// 1. 从默认值开始
	cfg := &Config{
		Broker: "tcp://localhost:1883",
		Topic:  "home/tts/say",
	}

	// 2. 如果指定了 -c，则加载配置文件
	if configFile != "" {
		fileCfg, err := loadConfigFromFile(configFile)
		if err != nil {
			log.Fatalf("❌ %v", err)
		}
		// 合并：配置文件覆盖默认值
		if fileCfg.Broker != "" {
			cfg.Broker = fileCfg.Broker
		}
		if fileCfg.Topic != "" {
			cfg.Topic = fileCfg.Topic
		}
		if fileCfg.Username != "" {
			cfg.Username = fileCfg.Username
		}
		if fileCfg.Password != "" {
			cfg.Password = fileCfg.Password
		}
	}

	// 3. 命令行参数优先级最高
	if broker != "" {
		cfg.Broker = broker
	}
	if topic != "" {
		cfg.Topic = topic
	}
	if username != "" {
		cfg.Username = username
	}
	if password != "" {
		cfg.Password = password
	}

	// 启动 MQTT 客户端
	opts := mqtt.NewClientOptions()
	opts.AddBroker(cfg.Broker)
	opts.SetClientID("go-tts-client-" + fmt.Sprintf("%d", time.Now().Unix()))
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)

	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
	}
	if cfg.Password != "" {
		opts.SetPassword(cfg.Password)
	}

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("无法连接 MQTT Broker %s: %v", cfg.Broker, token.Error())
	}

	if token := client.Subscribe(cfg.Topic, 1, f); token.Wait() && token.Error() != nil {
		log.Fatalf("无法订阅主题 %s: %v", cfg.Topic, token.Error())
	}

	log.Printf("✅ 已连接 MQTT Broker: %s", cfg.Broker)
	if cfg.Username != "" {
		log.Printf("👤 使用用户名: %s", cfg.Username)
	}
	log.Printf("🎧 正在监听主题: %s", cfg.Topic)
	log.Println("💡 示例:")
	log.Println(`   tts-mqtt.exe -b tcp://192.168.1.100:1883 -t my/tts -u user -p pass`)
	log.Println(`   tts-mqtt.exe -c config.json`)

	select {}
}
