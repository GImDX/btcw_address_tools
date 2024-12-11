package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	RPCURL      string `yaml:"url"`
	RPCUser     string `yaml:"username"`
	RPCPassword string `yaml:"password"`
	NBlocks     int    `yaml:"nblocks"`
}

type RPCResponse struct {
	Result interface{} `json:"result"`
	Error  interface{} `json:"error"`
	ID     string      `json:"id"`
}

func readConfig(filename string) (*Config, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	// Apply defaults if needed
	if config.RPCURL == "" {
		config.RPCURL = "http://192.168.8.115:9330"
	}
	if config.RPCUser == "" {
		config.RPCUser = "USER"
	}
	if config.RPCPassword == "" {
		config.RPCPassword = "PASS"
	}
	if config.NBlocks == 0 {
		config.NBlocks = 120
	}
	return &config, nil
}

func rpcCall(rpcURL, user, password, method string, params []interface{}) (interface{}, error) {
	client := &http.Client{}
	payloadMap := map[string]interface{}{
		"jsonrpc": "1.0",
		"id":      "curltest",
		"method":  method,
		"params":  params,
	}
	payloadBytes, err := json.Marshal(payloadMap)
	if err != nil {
		return nil, fmt.Errorf("Failed to marshal RPC payload: %v", err)
	}

	req, err := http.NewRequest("POST", rpcURL, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(user, password)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result RPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.Error != nil {
		return nil, fmt.Errorf("RPC error: %v", result.Error)
	}
	return result.Result, nil
}

func parseBits(bits string) *big.Int {
	bitsInt, _ := strconv.ParseUint(bits, 16, 32)
	coefficient := bitsInt & 0x00ffffff
	exponent := bitsInt >> 24
	target := new(big.Int).SetUint64(coefficient)
	target.Lsh(target, uint(8*(exponent-3)))
	return target
}

func calculateDifficulty(target *big.Int) *big.Float {
	maxTarget := new(big.Int).SetUint64(0xFFFF)
	maxTarget.Lsh(maxTarget, 8*(0x1D-3))
	targetFloat := new(big.Float).SetInt(target)
	maxTargetFloat := new(big.Float).SetInt(maxTarget)
	difficulty := new(big.Float).Quo(maxTargetFloat, targetFloat)
	return difficulty
}

func main() {
	// Read configuration
	config, err := readConfig("config.yaml")
	if err != nil {
		log.Fatalf("Failed to read config: %v", err)
	}

	// Get current block count
	currentBlockCount, err := rpcCall(config.RPCURL, config.RPCUser, config.RPCPassword, "getblockcount", nil)
	if err != nil {
		log.Fatalf("Failed to get block count: %v", err)
	}
	totalBlocks := int(currentBlockCount.(float64))

	// Prepare CSV file
	timestamp := time.Now().Format("20060102_150405")
	csvFilename := fmt.Sprintf("networkchart_%s_nblocks_%d.csv", timestamp, config.NBlocks)
	csvFile, err := os.Create(csvFilename)
	if err != nil {
		log.Fatalf("Failed to create CSV file: %v", err)
	}
	defer csvFile.Close()
	csvWriter := csv.NewWriter(csvFile)
	defer csvWriter.Flush()

	// Write CSV header
	csvWriter.Write([]string{"Time", "Height", "Hashrate", "CalculatedDifficulty"})

	// Fetch data for every nblocks interval
	for height := 0; height <= totalBlocks; height += config.NBlocks {
		log.Printf("height: %v", height)
		// Get block hash
		blockHash, err := rpcCall(config.RPCURL, config.RPCUser, config.RPCPassword, "getblockhash", []interface{}{height})
		if err != nil {
			log.Printf("Failed to get block hash for height %d: %v", height, err)
			continue
		}
		// log.Printf("blockHash: %v", blockHash)

		// Get block header
		blockHeader, err := rpcCall(config.RPCURL, config.RPCUser, config.RPCPassword, "getblockheader", []interface{}{blockHash, true})
		if err != nil {
			log.Printf("Failed to get block header for height %d: %v", height, err)
			continue
		}
		// log.Printf("blockHeader: %v", blockHeader)

		header := blockHeader.(map[string]interface{})
		timeUnix := int64(header["time"].(float64))
		utcTime := time.Unix(timeUnix, 0).UTC().Format("2006/01/02 15:04:05")

		bits := header["bits"].(string)
		// log.Printf("bits: %v", bits)
		// bits = "1c2a1115"
		target := parseBits(bits)
		calculatedDifficulty := calculateDifficulty(target)
		difficulty, _ := calculatedDifficulty.Float64()

		// Get network hashrate
		hashrate, err := rpcCall(config.RPCURL, config.RPCUser, config.RPCPassword, "getnetworkhashps", []interface{}{config.NBlocks, height})
		if err != nil {
			log.Printf("Failed to get network hashrate for height %d: %v", height, err)
			continue
		}

		// Write to CSV
		csvWriter.Write([]string{
			utcTime,
			strconv.Itoa(height),
			fmt.Sprintf("%.3f", hashrate.(float64)),
			fmt.Sprintf("%.3f", difficulty),
		})
	}

	log.Printf("Data saved to %s", csvFilename)
}
