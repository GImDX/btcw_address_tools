// 用于调用sendmany发送最大容量（2919 addresses，99405vB的交易）,不要用正在挖矿的节点执行，会卡住
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

// Config 存储配置信息
type Config struct {
	URL             	string 	`yaml:"url"`
	Username        	string 	`yaml:"username"`
	Password        	string 	`yaml:"password"`
	AddressFile     	string 	`yaml:"addressFile"`
	AddressLimit    	int 	`yaml:"addressLimit"`
	Amounts      		float64 `yaml:"amounts"`
	Feerate      		int 	`yaml:"feerate"`
	IsSend      		bool 	`yaml:"isSend"`
	MaxSendCount    	int 	`yaml:"maxSendCount"`
	MaxUnconfSize	    int 	`yaml:"maxUnconfSize"`
	Minconf  			int 	`yaml:"minconf"`
	Maxconf   			int 	`yaml:"maxconf"`
	SleepSec   			int 	`yaml:"sleepSec"`
	
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
	// format := "%-40s %v"

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
	logFilePath := "sendmany.log"

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
	sugar.Infof("Starting sendmany, RPC server: %s", config.URL)
	sugar.Infof("Sending to wallet: %s", config.AddressFile)

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

    sendCount := 0 // 记录 sendmany 调用次数

    // 从文件中读取地址
    addressInfos, err := ReadAddresses(config.AddressFile)
    if err != nil {
        sugar.Fatalf("Error reading addresses: %v", err)
    }

    // 构建 sendmany 的参数
    amounts := make(map[string]float64)
    for i, info := range addressInfos {
        if i >= config.AddressLimit {
            break
        }
        amounts[info.Address] = config.Amounts // 假设每个地址分配的数量是 0.00001 BTC
    }

	for sendCount < config.MaxSendCount {
		if sendCount >= config.MaxSendCount {
			break
		}
		for _, wallet := range wallets {
            walletName, ok := wallet.(string)
            if !ok {
                continue
            }
			sugar.Infof("Processing wallet: %s", walletName)
            walletUrl := fmt.Sprintf("%s/wallet/%s", config.URL, walletName)
            // 检查 listunspent
            unspentResp, err := sendRpcRequest(walletUrl, config.Username, config.Password, "listunspent", []interface{}{config.Minconf, config.Maxconf})
            if err != nil {
                sugar.Fatalf("Error listing unspent for wallet %s: %v", walletName, err)
                continue
            }

			unspent, ok := unspentResp.([]interface{})
			if !ok {
				sugar.Errorf("Error asserting unspent type for wallet %s", walletName)
				continue
			}

			// 计算当前钱包中未确认交易的总大小
			var totalUnconfirmedSize int
			for _, u := range unspent {
				unspentTx, ok := u.(map[string]interface{})
				if !ok {
					continue
				}
				txid, okTxid := unspentTx["txid"].(string)
				if okTxid {
					// 调用 gettransaction
					txResp, err := sendRpcRequest(walletUrl, config.Username, config.Password, "gettransaction", []interface{}{txid})
					if err != nil {
						sugar.Errorf("Error getting transaction %s for wallet %s: %v", txid, walletName, err)
						continue
					}
					tx, ok := txResp.(map[string]interface{})
					if !ok {
						continue
					}
					hex, okHex := tx["hex"].(string)
					if okHex {
						// 转换为字节长度
						totalUnconfirmedSize += len(hex) / 2
					}
				}
			}

			// 每个钱包允许存在的未确认交易数量，需要满足btc limitdescendantsize limitdescendantcount limitancestorsize limitancestorcount
            if totalUnconfirmedSize < config.MaxUnconfSize  {
                // listunspent 为空，执行 sendmany
                if config.IsSend {
                    sendManyResp, err := sendRpcRequest(walletUrl, config.Username, config.Password, "sendmany", []interface{}{"", amounts, 1, "", []string{}, nil, nil, nil, config.Feerate, true})
                    if err != nil {
                        sugar.Warnf("Error sending BTC from wallet %s: %v, sendManyResp: %v", walletName, err, sendManyResp)
						continue
                    }
					sendManyInfo, ok := sendManyResp.(map[string]interface{})
					if !ok {
						sugar.Fatalf("Invalid sendmany response", zap.String("walletName", walletName))
						continue
					}
					txid, ok := sendManyInfo["txid"].(string)
					if !ok {
						sugar.Fatalf("Error retrieving txid after sendmany", zap.String("walletName", walletName))
						continue
					}
                    sugar.Infof("Send BTC result from wallet %s: txis: %s", walletName, txid)
                    sendCount++
                }else{
					sugar.Infof("isSend is false, no send")
					sendCount++
				}
				sugar.Infof("Made transaction: %d / %d",sendCount , config.MaxSendCount)
				if sendCount >= config.MaxSendCount {
					sugar.Infof("Created enough transaction, exiting...")
					os.Exit(0)
				}
            } else{
				// 遍历未花费的交易
				for _, u := range unspent {
					sugar.Infof("Total unconfirmed transaction size for wallet %s is %d, skipping sendmany", walletName, totalUnconfirmedSize)
					unspentTx, ok := u.(map[string]interface{})
					if !ok {
						continue
					}
					txid, okTxid := unspentTx["txid"].(string)
					confirmations, okConf := unspentTx["confirmations"].(float64)
					if okTxid && okConf {
						// 记录不满足条件的交易
						sugar.Infof("Skip, Unspent transaction not meeting criteria in wallet %s: txid: %s, confirmations=%d", walletName, txid, int(confirmations))
					}
				}
				continue
			}
        }
        // 等待10秒
        time.Sleep(time.Duration(config.SleepSec) * time.Second)
    }

}
