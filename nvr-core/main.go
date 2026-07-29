package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("用法: nvr-core <record|push|stop>")
	}

	switch os.Args[1] {
	case "record":
		runCommand("record")
	case "push":
		runCommand("push")
	case "stop":
		stopAll()
	default:
		log.Fatalf("未知子命令: %s，可用: record, push, stop", os.Args[1])
	}
}

// runCommand 运行 record 或 push 子命令，处理信号优雅退出
func runCommand(cmd string) {
	cfg := LoadConfig()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 信号处理：收到 SIGTERM/SIGINT 时优雅退出
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigCh
		log.Printf("收到信号 %v，正在停止...", sig)
		cancel()
	}()

	switch cmd {
	case "record":
		runRecord(ctx, cfg)
	case "push":
		runPush(ctx, cfg)
	}
}

// stopAll 停止所有 nvr-core record/push 进程
func stopAll() {
	// 兼容：调用 init.d stop
	exec.Command("/bin/sh", "-c", "/etc/init.d/nvr stop 2>/dev/null || true").Run()

	// 查找并停止 nvr-core record/push 进程（排除自身 stop 进程）
	exec.Command("/bin/sh", "-c",
		"ps -w | grep 'nvr-core' | grep -E 'record|push' | grep -v grep | awk '{print $1}' | xargs -r kill -TERM 2>/dev/null || true").Run()

	log.Printf("已停止所有 nvr-core record/push 进程")
}
