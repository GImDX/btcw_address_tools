package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"gopkg.in/yaml.v2"
)

// Config 结构体用于映射YAML配置文件中的键值对
type Config struct {
    InputFilePath  string `yaml:"inputFilePath"`
    OutputFilePath string `yaml:"outputFilePath"`
}

func main() {
    // 读取并解析配置文件
    config := Config{}
    configFile, err := ioutil.ReadFile("config.yaml")
    if err != nil {
        fmt.Println("Error reading config file:", err)
        return
    }

    err = yaml.Unmarshal(configFile, &config)
    if err != nil {
        fmt.Println("Error parsing config file:", err)
        return
    }

    inputFile, err := os.Open(config.InputFilePath)
    if err != nil {
        fmt.Println("Error opening input file:", err)
        return
    }
    defer inputFile.Close()

    outputFile, err := os.Create(config.OutputFilePath)
    if err != nil {
        fmt.Println("Error creating output file:", err)
        return
    }
    defer outputFile.Close()

    scanner := bufio.NewScanner(inputFile)
    writer := bufio.NewWriter(outputFile)
    defer writer.Flush()

    for scanner.Scan() {
        line := scanner.Text()
        if strings.Contains(line, "label") {
            _, err := writer.WriteString(line + "\n")
            if err != nil {
                fmt.Println("Error writing to output file:", err)
                return
            }
        }
    }

    if err := scanner.Err(); err != nil {
        fmt.Println("Error reading input file:", err)
    }
}
