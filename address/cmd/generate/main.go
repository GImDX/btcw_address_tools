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

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v2"
)

type Config struct {
	URL                       string  `yaml:"url"`
	Username                  string  `yaml:"username"`
	Password                  string  `yaml:"password"`
}

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

// sendRpcRequest 发送RPC请求的函数
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
	configFile, err := ioutil.ReadFile("config.yaml")
	if err != nil {
		log.Fatalf("Error reading config file: %v", err)
	}

	var config Config
	if err := yaml.Unmarshal(configFile, &config); err != nil {
		log.Fatalf("Error parsing config file: %v", err)
	}

	logFilePath := "prioritisetransaction.log"
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

	sugar.Infof("Starting generate, mining RPC server: %s", config.URL)

	generateResp, err := sendRpcRequest(config.URL, config.Username, config.Password, "generate", []interface{}{})
	if err != nil {
		sugar.Errorf("Error generate", zap.Error(err))
	}
	sugar.Infof("%v", generateResp)
}
