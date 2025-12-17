package main

import (
	"encoding/json"
	"fmt"
	"log"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
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


	// ✅ 异步处理 TTS，避免阻塞 MQTT 回调
    go func(t string) {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()
        
        done := make(chan error, 1)
        go func() {
            done <- speakText(t)
        }()

        select {
        case err := <-done:
            if err != nil {
                log.Printf("❌ TTS 错误: %v", err)
            } else {
                log.Printf("✅ 已完成朗读: %q", t)
            }
        case <-ctx.Done():
            log.Printf("⏰ TTS 超时（30秒），放弃朗读: %.50q", t)
            // 注意：无法强制 kill powershell 进程，但至少不卡主线
        }
    }(text)

	
}

func speakText(text string) error {
	 log.Printf("🔊 尝试朗读文本 (长度=%d): %.50q", len(text), text) // 最多显示前50字符

    // 转义 PowerShell 特殊字符
	safeText := strings.ReplaceAll(text, "\"", "`\"")
	safeText = strings.ReplaceAll(safeText, "$", "`$")

	start := time.Now()

	// 构建 PowerShell 命令（增加错误捕获和静默模式）
	psCmd := `
			try {
			    Add-Type -AssemblyName System.Speech
			    $synth = New-Object System.Speech.Synthesis.SpeechSynthesizer
			    $synth.Speak("` + safeText + `")
			    Write-Host "✅ TTS 成功: 长度=$(("` + safeText + `").Length)"
			} catch {
			    Write-Error "❌ TTS 失败: $($_.Exception.Message)"
			    exit 1
			}
			`

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psCmd)

	// 捕获 stdout + stderr 合并输出
	output, err := cmd.CombinedOutput()

	// 记录完整输出（包含 Write-Host 和 Write-Error）
	logMsg := strings.TrimSpace(string(output))
	if logMsg != "" {
		log.Printf("🔊 PowerShell TTS 输出: %s", logMsg)
	}

	if err != nil {
		log.Printf("❌ PowerShell TTS 执行失败: %v", err)
		return err
	}

	log.Printf("🔊 朗读结束，耗时: %v", time.Since(start))

	return nil
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

    log.SetOutput(logFile)

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
	opts.SetClientID("go-tts-client")
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)

	opts.SetOnConnectHandler(func(client mqtt.Client) {
	    log.Println("🔌 MQTT 连接成功，正在重新订阅主题...")
	    token := client.Subscribe(cfg.Topic, 1, f)
	    if !token.WaitTimeout(5 * time.Second) || token.Error() != nil {
	        log.Fatalf("❌ 重订阅失败: %v", token.Error())
	    }
	    log.Printf("✅ 重订阅成功: %s", cfg.Topic)
	})
	
	// 可选：添加连接丢失回调用于调试
	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
	    log.Printf("⚠️ MQTT 连接已断开: %v", err)
	})

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
		
	token = client.Subscribe(cfg.Topic, 1, f)
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
