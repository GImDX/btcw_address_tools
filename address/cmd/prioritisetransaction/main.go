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
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v2"
)

type Config struct {
	URL                       string  `yaml:"url"`
	Username                  string  `yaml:"username"`
	Password                  string  `yaml:"password"`
	CheckInterval        	  int     `yaml:"checkInterval"`
	FeeDelta                  float64 `yaml:"feeDelta"`
	PrioritiseTransactionURLs []struct {
		URL      string `yaml:"url"`
		Username string `yaml:"username"`
		Password string `yaml:"password"`
	} `yaml:"prioritiseTransactionURLs"`
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

	sugar.Infof("Starting prioritisetransaction, transaction RPC server: %s", config.URL)
	sugar.Infof("Starting prioritisetransaction, mining RPC server: %s", config.PrioritiseTransactionURLs)

	// 获取钱包列表
	walletListResp, err := sendRpcRequest(config.URL, config.Username, config.Password, "listwallets", []interface{}{})
	if err != nil {
		sugar.Errorf("Error listing wallets", zap.Error(err))
	}
	wallets, ok := walletListResp.([]interface{})
	if !ok {
		sugar.Errorf("Invalid wallet list response")
	}

	prioritisetransactionCircle := 0
	for {
		sugar.Infof("prioritisetransactionCircle: %d", prioritisetransactionCircle)
		for _, wallet := range wallets {
			walletName, ok := wallet.(string)
			if !ok {
				sugar.Error("Invalid wallet name in wallet list")
				continue
			}
			walletUrl := fmt.Sprintf("%s/wallet/%s", config.URL, walletName)

			sugar.Infof("Checking unconfirmed transactions for wallet: %s", walletUrl)

			// Fetch unconfirmed transactions from the main node
			unconfirmedTx, err := sendRpcRequest(walletUrl, config.Username, config.Password, "listunspent", []interface{}{0, 0, []string{}, true, map[string]interface{}{"minimumAmount": 0.00002}})
			if err != nil {
				sugar.Errorf("Error fetching unconfirmed transactions: %v", err)
				continue
			}

			txids, ok := unconfirmedTx.([]interface{})
			if !ok || len(txids) == 0 {
				sugar.Info("No unconfirmed transactions found or invalid listunspent response")
				continue
			}

			for _, tx := range txids {
				txInfo, ok := tx.(map[string]interface{})
				if !ok {
					sugar.Error("Invalid transaction format")
					continue
				}

				txid, ok := txInfo["txid"].(string)
				if !ok {
					sugar.Error("Transaction ID not found")
					continue
				}

				for _, node := range config.PrioritiseTransactionURLs {
					sugar.Infof("Processing mining node: %s", node.URL)
					_, err := sendRpcRequest(node.URL, node.Username, node.Password, "prioritisetransaction", []interface{}{txid, 0, config.FeeDelta})
					if err != nil {
						sugar.Errorf("Error prioritising transaction %s on node %s: %v", txid, node.URL, err)
						continue
					}
					sugar.Infof("Successfully prioritised transaction %s on node %s, fee_delta %f", txid, node.URL, config.FeeDelta)
					// sugar.Infof("prioritisetransaction response: %v", prioritiseTXResp)
				}
			}
		}
		prioritisetransactionCircle += 1
		time.Sleep(time.Duration(config.CheckInterval) * time.Second)
	}
}
