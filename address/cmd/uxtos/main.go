// 用于列出wallets，balance，uxtos数量
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

// Config 存储配置信息
type Config struct {
	URL      string `yaml:"url"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Minconf  int    `yaml:"minconf"`
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

// AddressInfo 代表 JSON 文件中的每个地址条目
type AddressInfo struct {
	Address       string   `json:"address"`
	Amount        float64  `json:"amount"`
	Confirmations int      `json:"confirmations"`
	Label         string   `json:"label"`
	Txids         []string `json:"txids"`
}

// ReadAddresses 从 JSON 文件中读取地址
func ReadAddresses(filename string) ([]AddressInfo, error) {
	bytes, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var addresses []AddressInfo
	err = json.Unmarshal(bytes, &addresses)
	if err != nil {
		return nil, err
	}

	return addresses, nil
}

func main() {
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
	logFilePath := "uxtos.log"

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
	sugar.Infof(format, "Starting uxtos, RPC server: %s", config.URL)

	// 调用 listwallets RPC
	walletList, err := sendRpcRequest(config.URL, config.Username, config.Password, "listwallets", []interface{}{})
	if err != nil {
		sugar.Fatalf("Error listing wallets: %v", err)
	}
	sugar.Infof("Node load wallet(s):%s", walletList)

	wallets, ok := walletList.([]interface{})
	if !ok {
		sugar.Fatalf("Error asserting wallet list type: %v", walletList)
	}

	totalbalance := 0.0
	for _, wallet := range wallets {
		walletName, ok := wallet.(string)
		if !ok {
			continue
		}
		sugar.Infof("Processing wallet: %s", walletName)
		walletUrl := fmt.Sprintf("%s/wallet/%s", config.URL, walletName)
		// 检查 listunspent
		balanceResult, err := sendRpcRequest(walletUrl, config.Username, config.Password, "getbalances", []interface{}{})
		if err != nil {
			sugar.Fatalf("Error getting balance: for wallet %s: %v", walletName, err)
			continue
		} else {
			sugar.Infof("Balances: %v", balanceResult)
			if brMap, ok := balanceResult.(map[string]interface{}); ok {
				// 然后，我们尝试从"mine"键访问对应的值，并将其断言为map[string]interface{}
				if mine, ok := brMap["mine"].(map[string]interface{}); ok {
					// 最后，我们尝试从"mine" map中提取"trusted"的值，并将其断言为float64类型
					if trusted, ok := mine["trusted"].(float64); ok {
						totalbalance += trusted
						//sugar.Infof("The trusted balance is: %f\n", trusted)
					} else {
						sugar.Fatal("The trusted value is not a float64 type")
					}
				} else {
					sugar.Fatal("'mine' key is not the expected type")
				}
			} else {
				sugar.Fatal("balanceResult is not the expected type")
			}
		}

		sugar.Infof("minconf: %v", config.Minconf)
		// 调用 listunspent RPC
		listUnspentResult, err := sendRpcRequest(walletUrl, config.Username, config.Password, "listunspent", []interface{}{config.Minconf})
		if err != nil {
			sugar.Fatalf("Error listing unspent outputs: %v", err)
		} else {
			unspentOutputs, ok := listUnspentResult.([]interface{})
			if !ok {
				sugar.Fatalf("Invalid response type for unspent outputs")
			}
			sugar.Infof("Number of Unspent Outputs: %v", len(unspentOutputs))
		}
	}
	sugar.Infof("The total balance is: %f", totalbalance)

}
