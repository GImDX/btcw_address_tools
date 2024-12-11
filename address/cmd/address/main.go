// 已弃用，直接在RPC窗口中调用tx命令，地址数量超过100个就没有反应了，改为RPC调用sendmany方法
package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strings"
)

func main() {
	// 从文件中读取JSON数据
	filePath := "../btcw2.json"
	jsonData, err := ioutil.ReadFile(filePath)
	if err != nil {
		fmt.Println("Error reading JSON file:", err)
		return
	}

	// 解析JSON数据
	var addresses []map[string]interface{}
	err = json.Unmarshal(jsonData, &addresses)
	if err != nil {
		fmt.Println("Error parsing JSON:", err)
		return
	}

	// 设置最大拼接的地址数量
	maxAddresses := 3000 // 你可以根据需要更改这个值

	// 提取并拼接地址
	var formattedAddresses strings.Builder
	for i, addr := range addresses {
		if i >= maxAddresses {
			break
		}
		formattedAddresses.WriteString(fmt.Sprintf("%s", addr["address"]))
		if i < maxAddresses-1 {
			formattedAddresses.WriteString(" ")
		}
	}

	// 打印结果
	fmt.Printf("Total addresses: %d\n", len(addresses))
	// tx "address" amount ( fee_rate num_sends subtractfeefromamount conf_target "estimate_mode" avoid_reuse verbose )
	// fmt.Println("tx "+`"`+formattedAddresses.String()+`"`+" 0.00001"+" 3400 1 false 1 unset false true")
	fmt.Println("tx "+`"`+formattedAddresses.String()+`"`+" 0.00001"+" 4000")
}
