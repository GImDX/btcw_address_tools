package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v2"
)

// Config 存储配置信息
type Config struct {
	URL                  string  `yaml:"url"`
	Username             string  `yaml:"username"`
	Password             string  `yaml:"password"`
	IsBump               bool    `yaml:"isBump"`
	BlockCheckInterval   int     `yaml:"blockCheckInterval"`
	BumpfeeBlockInterval int     `yaml:"bumpfeeBlockInterval"`
	FeeBumpAmount        float64 `yaml:"feeBumpAmount"`
	FeeCap               float64 `yaml:"feeCap"`
}

// TxInfo 用于跟踪交易信息
type TxInfo struct {
	WalletName       string
	FirstBlockHeight int
	CurrentFeerate   float64
}

// JsonRpcRequest 和 JsonRpcResponse 分别定义了RPC请求和响应的结构
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
	logFilePath := "bumpfee.log"

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
	sugar.Infof("Starting bumpfee, RPC server: %s", config.URL)

	// 这里是主循环的开始
	txInfos := make(map[string]*TxInfo)
	var lastBlockHeight int64 = -1 // 初始设置为 -1 以确保第一次检测到区块高度变化

	// 获取钱包列表
	walletListResp, err := sendRpcRequest(config.URL, config.Username, config.Password, "listwallets", []interface{}{})
	if err != nil {
		sugar.Errorf("Error listing wallets", zap.Error(err))
	}
	wallets, ok := walletListResp.([]interface{})
	if !ok {
		sugar.Errorf("Invalid wallet list response")
	}

	// 指定minimumAmount
	queryOptions := map[string]interface{}{
		"minimumAmount": 0.00002, // 指定最小UTXO，排除0.00001的
	}

	for {
		// 获取当前区块高度
		blockCountResp, err := sendRpcRequest(config.URL, config.Username, config.Password, "getblockcount", []interface{}{})
		if err != nil {
			sugar.Error("Error getting current block count", zap.Error(err))
			continue
		}
		// } else{
		// 	sugar.Infof("getblockcount SUCCESS")
		// }

		currentBlockCount, ok := blockCountResp.(float64)
		if !ok {
			sugar.Error("Invalid block count response")
			continue
		}
		if lastBlockHeight == -1 {
			sugar.Infof("New block detected: %d", int64(currentBlockCount))
		} else if int64(currentBlockCount) != lastBlockHeight {
			sugar.Infof("New block detected: %d", int64(currentBlockCount))
		}

		for _, wallet := range wallets {
			walletName, ok := wallet.(string)
			if !ok {
				sugar.Error("Invalid wallet name in wallet list")
				continue
			}
			walletUrl := fmt.Sprintf("%s/wallet/%s", config.URL, walletName)

			// 获取未确认的交易 minconf=0, maxconf=0
			unspentResp, err := sendRpcRequest(walletUrl, config.Username, config.Password, "listunspent", []interface{}{0, 0, []string{}, true, queryOptions})
			if err != nil {
				sugar.Error("Error getting unconfirmed txids for wallet", zap.String("wallet", walletName), zap.Error(err))
				continue
			}
			unspent, ok := unspentResp.([]interface{})
			if !ok {
				sugar.Error("Invalid unspent response for wallet", zap.String("wallet", walletName))
				continue
			}

			for _, u := range unspent {
				unspentInfo, ok := u.(map[string]interface{})
				if !ok {
					sugar.Error("Invalid unspent info", zap.String("wallet", walletName))
					continue
				}
				txid, ok := unspentInfo["txid"].(string)
				if !ok {
					sugar.Error("Invalid txid", zap.String("wallet", walletName))
					continue
				}

				// 检查和更新费率
				info, exists := txInfos[txid]
				if !exists {
					// 使用 gettransaction RPC命令获取交易详情
					getTxResp, err := sendRpcRequest(walletUrl, config.Username, config.Password, "gettransaction", []interface{}{txid})
					if err != nil {
						sugar.Error("Error getting transaction info", zap.String("wallet", walletName), zap.String("txid", txid), zap.Error(err))
						continue
					}
					getTxInfo, ok := getTxResp.(map[string]interface{})
					if !ok {
						sugar.Error("Invalid gettransaction response", zap.String("wallet", walletName), zap.String("txid", txid))
						continue
					}
					fee, ok := getTxInfo["fee"].(float64)
					if !ok {
						sugar.Error("Invalid fee in gettransaction response", zap.String("wallet", walletName), zap.String("txid", txid))
						continue
					}
					hex, ok := getTxInfo["hex"].(string)
					if !ok {
						sugar.Error("Invalid hex in gettransaction response", zap.String("wallet", walletName), zap.String("txid", txid))
						continue
					}
					// 计算手续费率sat/vB
					feerate := math.Abs(fee) * 1e8 / float64(len(hex)) * 2

					info = &TxInfo{
						WalletName:       walletName,
						FirstBlockHeight: int(currentBlockCount),
						CurrentFeerate:   feerate,
					}
					txInfos[txid] = info
					sugar.Infof("Found a new unconfirmed transaction, wallet: %s, txid: %s, feerate: %.1f", info.WalletName, txid, feerate)
				}

				// 检测到区块高度变化
				if int64(currentBlockCount) != lastBlockHeight && lastBlockHeight != -1 {
					// 打印每个未确认交易的区块高度差
					for txid, info := range txInfos {
						// 仅当交易所属的钱包与当前处理的钱包相同时，才打印日志
						if info.WalletName == walletName {
							blockHeightDiff := int64(currentBlockCount) - int64(info.FirstBlockHeight)
							sugar.Infof("wallet: %s, transaction txid: %s, unconfirmed for block interval: %d", info.WalletName, txid, blockHeightDiff)
						}
					}
				}

				if int(currentBlockCount)-info.FirstBlockHeight >= config.BumpfeeBlockInterval {
					newFeerate := info.CurrentFeerate + config.FeeBumpAmount
					if newFeerate > config.FeeCap {
						newFeerate = config.FeeCap
					}
					newFeerateRounded := int(math.Round(newFeerate))
					// bumpfee incrementalFee at least 1 sat/vB
					if newFeerate-info.CurrentFeerate >= 1 {
						sugar.Infof("Bumpfee for txid: %s, newFeerate: %d", txid, newFeerateRounded)
						if config.IsBump {
							bumpResp, err := sendRpcRequest(walletUrl, config.Username, config.Password, "bumpfee", []interface{}{txid, map[string]interface{}{"fee_rate": newFeerateRounded}})
							if err != nil {
								sugar.Error("Error bumping fee", zap.String("txid", txid), zap.Error(err))
								continue
							}
							bumpInfo, ok := bumpResp.(map[string]interface{})
							if !ok {
								sugar.Error("Invalid bumpfee response", zap.String("txid", txid))
								continue
							}
							newTxid, ok := bumpInfo["txid"].(string)
							if !ok {
								sugar.Error("Error retrieving new txid after bumpfee", zap.String("txid", txid))
								continue
							}
							// 移除旧的txid
							delete(txInfos, txid)
							sugar.Infof("New txid: %s, newFeerate: %d", newTxid, newFeerateRounded)
						} else {
							// 移除旧的txid
							delete(txInfos, txid)
							sugar.Infof("IsBump is false, No bumped, Old txid: %s", txid)
							sugar.Infof("New txid: %s, newFeerate: %d", txid, newFeerate)
						}
					} else {
						sugar.Infof("No bumped, Bumpfee incrementalFee at least 1 sat/vB")
					}
					// 移除旧的txid
					// delete(txInfos, txid)
				}
			}
		}
		lastBlockHeight = int64(currentBlockCount)

		// 每隔一定时间间隔运行
		time.Sleep(time.Duration(config.BlockCheckInterval) * time.Second)
	}
}
