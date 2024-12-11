// 用于创建wallet，生成address，并输出addresses列表到json
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v2"
)

// Config 存储配置信息
type Config struct {
	URL             string `yaml:"url"`
	Username        string `yaml:"username"`
	Password        string `yaml:"password"`
	IsCreateWallet  bool   `yaml:"isCreateWallet"`
	NewWallet       string `yaml:"newWallet"`
	IsCreateAddress bool   `yaml:"isCreateAddress"`
	NewAddressCount int    `yaml:"newAddressCount"`
	Interval        int    `yaml:"interval"`
	OutputFile      string `yaml:"outputFile"`
}

// 定义请求和响应的结构体
type JsonRpcRequest struct {
	Jsonrpc string        `json:"jsonrpc"`
	ID      string        `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type JsonRpcResponse struct {
	Result interface{} `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	ID string `json:"id"`
}

// 发送RPC请求的函数
func sendRpcRequest(url, username, password, method string, params []interface{}) (interface{}, error) {
	reqBody := JsonRpcRequest{
		Jsonrpc: "1.0",
		ID:      method,
		Method:  method,
		Params:  params,
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	client := &http.Client{}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	req.Header.Add("Authorization", "Basic "+auth)
	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var response JsonRpcResponse
	err = json.Unmarshal(body, &response)
	if err != nil {
		return nil, err
	}

	if response.Error != nil {
		return nil, fmt.Errorf("RPC Error: %s", response.Error.Message)
	}

	return response.Result, nil
}

func main() {
	// url := "http://192.168.8.115:9334/"
	// username := "USER"
	// password := "PASS"
	// newWallet := "btcw4"
	// isCreatWallet := false
	// isCreatAddress := false
	// newAddressCount := 3000
	// interval := 10
	// outputFile := "../btcw4.json"
	format := "%-40s %v"

	// 读取配置文件
	configFile, err := ioutil.ReadFile("config.yaml")
	if err != nil {
		log.Fatalf("Error reading config file: %v", err)
	}

	var config Config
	if err := yaml.Unmarshal(configFile, &config); err != nil {
		log.Fatalf("Error parsing config file: %v", err)
	}

	// 日志文件路径
	logFilePath := "newaddress.log"

	// 创建并打开日志文件
	logFile, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatalf("Cannot open log file: %v", err)
	}
	defer logFile.Close()

	// 配置 zap
	zapconfig := zap.NewProductionEncoderConfig()
	zapconfig.EncodeTime = zapcore.ISO8601TimeEncoder
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zapconfig),
		zapcore.NewMultiWriteSyncer(zapcore.AddSync(logFile), zapcore.AddSync(os.Stdout)),
		zapcore.InfoLevel,
	)
	logger := zap.New(core)
	defer logger.Sync() // Flushes buffer, if any
	sugar := logger.Sugar()
	sugar.Infof("")
	sugar.Infof(format, "Starting newaddress, RPC server: %s", config.URL)

	// 调用 createwallet RPC
	if config.IsCreateWallet {
		createWalletResult, err := sendRpcRequest(config.URL, config.Username, config.Password, "createwallet", []interface{}{config.NewWallet, false, false, "", false, false, true})
		if err != nil {
			sugar.Fatalf("Error creating wallet: ", err)
		} else {
			sugar.Infof(format, "New BitcoinPow Wallets:", createWalletResult)
		}
	}
	sugar.Infof(format, "isCreatewallet:", config.IsCreateWallet)

	// 调用 listwallets RPC
	listWalletsResult, err := sendRpcRequest(config.URL, config.Username, config.Password, "listwallets", []interface{}{})
	if err != nil {
		sugar.Fatalf("Error listing wallet: ", err)
	} else {
		sugar.Infof(format, "Existing BitcoinPow Wallets:", listWalletsResult)
	}

	// 调用 getnewaddress RPC
	count := 0
	if config.IsCreateAddress {
		for i := 0; i < config.NewAddressCount; i++ {
			_, err := sendRpcRequest(config.URL, config.Username, config.Password, "getnewaddress", []interface{}{"", "legacy"})
			if err != nil {
				sugar.Infof("Error getting new address: %v\n", err)
			} else {
				count++
			}
			time.Sleep(time.Duration(config.Interval) * time.Millisecond)
		}
	}
	sugar.Infof(format, "isCreatAddress:", config.IsCreateAddress)
	sugar.Infof(format, "Create new BitcoinPow addresses:", count)

	// 调用 listreceivedbyaddress RPC
	listReceivedResult, err := sendRpcRequest(config.URL, config.Username, config.Password, "listreceivedbyaddress", []interface{}{1, true})
	if err != nil {
		sugar.Fatalf("Error listing received by address: ", err)
	}

	// 检查 OutputFile 文件是否已存在
	if _, err := os.Stat(config.OutputFile); err == nil {
		// 如果文件存在，报错并退出
		sugar.Fatalf("Error: Output file %s already exists. Exiting to prevent overwriting.", config.OutputFile)
	} else if !os.IsNotExist(err) {
		// 如果检查文件存在时遇到其他错误，也报错并退出
		sugar.Fatalf("Error checking if output file exists: %v", err)
	}

	// 保存结果到文件
	file, err := json.MarshalIndent(listReceivedResult, "", " ")
	if err != nil {
		sugar.Fatalf("Error marshalling JSON: ", err)
	}
	err = ioutil.WriteFile(config.OutputFile, file, 0666)
	if err != nil {
		sugar.Fatalf("Error writing file: ", err)
	} else {
		absolutePath, err := filepath.Abs(config.OutputFile)
		if err != nil {
			sugar.Fatalf("Error getting absolute path: ", err)
		}
		sugar.Infof(format, "Addresses list JSON file:", absolutePath)
	}
}
