package main

import (
	"encoding/json"
	"fmt"
	"log"
	"io"
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
	 log.Printf("🔊 尝试朗读文本 (长度=%d): %.50q", len(text), text) // 最多显示前50字符

    err := ole.CoInitialize(0)
    if err != nil {
        log.Printf("❌ COM 初始化失败: %v", err)
        return fmt.Errorf("COM 初始化失败: %v", err)
    }
    defer ole.CoUninitialize()

    unknown, err := oleutil.CreateObject("SAPI.SpVoice")
    if err != nil {
        log.Printf("❌ 创建 SpVoice 对象失败: %v", err)
        return fmt.Errorf("创建 SpVoice 失败: %v", err)
    }
    voice, err := unknown.QueryInterface(ole.IID_IDispatch)
    if err != nil {
        log.Printf("❌ QueryInterface 失败: %v", err)
        unknown.Release()
        return fmt.Errorf("QueryInterface 失败: %v", err)
    }
    defer voice.Release()

    result, err := oleutil.CallMethod(voice, "Speak", text)
    if err != nil {
        log.Printf("❌ Speak 调用失败: %v", err)
    } else {
        log.Printf("ℹ️ Speak 返回值: %v", result.Val)
    }
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
        broker   string
        topic    string
        username string
        password string
        showHelp bool
    )


	logFile, err := os.OpenFile("tts-mqtt.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
    if err != nil {
        fmt.Fprintf(os.Stderr, "无法创建日志文件: %v\n", err)
        os.Exit(1)
    }
    defer logFile.Close()

    // 可选：同时输出到控制台和文件
    multiWriter := io.MultiWriter(os.Stdout, logFile)
    log.SetOutput(multiWriter)

    // 设置日志前缀（含时间戳）
    log.SetFlags(log.LstdFlags | log.Lshortfile) // Lshortfile 显示文件:行号，便于调试
    // =============================

	

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

	if showHelp {
        pflag.Usage()
        os.Exit(0)
    }

    // 默认配置
    cfg := &Config{
        Broker: "tcp://localhost:1883",
        Topic:  "home/tts/say",
    }

    const defaultConfigFile = "config.json"
    var loadedFromConfig = false

    // ✅ 自动检测 config.json 是否存在
    if _, err := os.Stat(defaultConfigFile); err == nil {
        // 文件存在，尝试加载
        fileCfg, err := loadConfigFromFile(defaultConfigFile)
        if err != nil {
            log.Fatalf("❌ 配置文件 %q 存在但加载失败: %v", defaultConfigFile, err)
        }
        // 合并：配置文件字段优先，非空才覆盖
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
        loadedFromConfig = true
        log.Printf("✅ 使用配置文件: %s", defaultConfigFile)
    }

    // ✅ 仅当未从配置文件加载时，才应用命令行参数
    if !loadedFromConfig {
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
        log.Println("ℹ️ 未找到 config.json，使用命令行参数或默认值")
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
	
	token := client.Connect()
	// 设置 10 秒超时
	if !token.WaitTimeout(10 * time.Second) {
	    log.Fatal("❌ 连接 MQTT Broker 超时（10秒）")
	}
	if err := token.Error(); err != nil {
	    log.Fatalf("❌ 无法连接到 MQTT Broker: %v", err)
	}
		
	token := client.Subscribe(cfg.Topic, 1, f)
	if !token.WaitTimeout(10 * time.Second) {
		log.Fatalf("订阅主题超时 %s: %v", cfg.Topic, token.Error())
	}
	if err := token.Error(); err != nil {
	    log.Fatalf("❌ 无法订阅主题: %v", err)
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
