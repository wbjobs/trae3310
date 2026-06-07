package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"trace-cli/pkg/aggregator"
	"trace-cli/pkg/incremental"
)

var (
	daemonIntervalStr string
	daemonWindowStr   string
	daemonMetricsAddr string
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "增量分析守护进程",
	Long:  `持续运行，每10秒拉取新数据并更新聚合结果，支持Prometheus metrics HTTP端点。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		interval, err := time.ParseDuration(daemonIntervalStr)
		if err != nil {
			return fmt.Errorf("解析更新间隔失败: %w", err)
		}

		timeWindow, err := time.ParseDuration(daemonWindowStr)
		if err != nil {
			return fmt.Errorf("解析时间窗口失败: %w", err)
		}

		var agg *aggregator.Aggregator
		if useStreaming {
			agg = aggregator.NewStreamingAggregator(streamIndex)
		} else {
			agg = aggregator.NewAggregator(store)
		}

		cfg := incremental.DaemonConfig{
			Interval:         interval,
			TimeWindow:       timeWindow,
			Service:          serviceFilter,
			Operation:        operationFilter,
			MetricsAddr:      daemonMetricsAddr,
			MetricsNamespace: metricsNamespace,
			UseStreaming:     useStreaming,
		}

		daemon, err := incremental.NewDaemon(cfg, agg, useStreaming)
		if err != nil {
			return fmt.Errorf("创建守护进程失败: %w", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			<-sigCh
			fmt.Println("\n🛑 收到停止信号，正在关闭...")
			cancel()
		}()

		return daemon.Start(ctx)
	},
}

func init() {
	daemonCmd.Flags().StringVarP(&daemonIntervalStr, "interval", "i", "10s", "更新间隔，如 10s、30s、1m (默认10秒)")
	daemonCmd.Flags().StringVarP(&daemonWindowStr, "time-window", "w", "1h", "聚合时间窗口，如 1h、6h、24h (默认1小时)")
	daemonCmd.Flags().StringVar(&daemonMetricsAddr, "metrics-addr", ":9090", "Prometheus metrics HTTP端点地址，空则不启动")
	daemonCmd.Flags().StringVar(&metricsNamespace, "namespace", "trace_cli", "Metrics命名空间前缀")
	daemonCmd.Flags().StringVarP(&serviceFilter, "service", "s", "", "按服务名称过滤")
	daemonCmd.Flags().StringVarP(&operationFilter, "operation", "o", "", "按操作名称过滤")
	rootCmd.AddCommand(daemonCmd)
}
