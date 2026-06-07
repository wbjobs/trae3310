package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

var (
	listenAddr string
)

var receiveCmd = &cobra.Command{
	Use:   "receive",
	Short: "启动gRPC接收器",
	Long:  `启动OTLP gRPC接收器，接收从OpenTelemetry Collector推送的追踪数据。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		errCh := make(chan error, 1)

		go func() {
			fmt.Printf("📡 启动OTLP接收器，监听地址: %s\n", listenAddr)
			fmt.Println("按 Ctrl+C 停止接收...")
			errCh <- startReceiver(ctx, listenAddr)
		}()

		select {
		case <-sigCh:
			fmt.Println("\n🛑 收到停止信号，正在关闭...")
			cancel()
			fmt.Printf("📊 共接收 %d 条追踪数据\n", store.Len())
		case err := <-errCh:
			if err != nil {
				return fmt.Errorf("接收器错误: %w", err)
			}
		}

		return nil
	},
}

func init() {
	receiveCmd.Flags().StringVarP(&listenAddr, "listen", "l", ":4317", "OTLP gRPC监听地址")
	rootCmd.AddCommand(receiveCmd)
}
