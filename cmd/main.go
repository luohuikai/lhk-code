package main

import (
	"fmt"
	"os"
)

func main() {
	// 初始化设置
	config, err := LoadConfig()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer config.Cancel()

	SetupAndRunClient(*config, config.Ctx)
}
