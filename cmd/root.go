package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"trace-cli/pkg/analyzer"
	"trace-cli/pkg/config"
	"trace-cli/pkg/grpcclient"
	"trace-cli/pkg/storage"
)

var (
	cfgFile    string
	configData *config.Config
	store      *storage.TraceStore
	analyzerInstance *analyzer.Analyzer
	grpcClient *grpcclient.Client

	serviceFilter   string
	operationFilter string
	traceIDFilter   string
	startTimeStr    string
	endTimeStr      string
	limit           int
	outputFormat    string
)

var rootCmd = &cobra.Command{
	Use:   "trace-cli",
	Short: "分布式追踪分析工具",
	Long:  `一个用于分析分布式系统追踪数据的CLI工具，支持慢查询识别、瓶颈分析和火焰图生成。`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		configData, err = config.LoadConfig(cfgFile)
		if err != nil {
			return fmt.Errorf("加载配置文件失败: %w", err)
		}

		store = storage.NewTraceStore()
		analyzerInstance = analyzer.NewAnalyzer(store)

		return nil
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if grpcClient != nil {
			grpcClient.Close()
		}
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "config.yaml", "配置文件路径")
	rootCmd.PersistentFlags().StringVarP(&serviceFilter, "service", "s", "", "按服务名称过滤")
	rootCmd.PersistentFlags().StringVarP(&operationFilter, "operation", "o", "", "按操作名称过滤")
	rootCmd.PersistentFlags().StringVarP(&traceIDFilter, "trace-id", "t", "", "按Trace ID过滤")
	rootCmd.PersistentFlags().StringVar(&startTimeStr, "start-time", "", "开始时间 (RFC3339格式，如: 2024-01-01T00:00:00Z)")
	rootCmd.PersistentFlags().StringVar(&endTimeStr, "end-time", "", "结束时间 (RFC3339格式)")
	rootCmd.PersistentFlags().IntVarP(&limit, "limit", "l", 0, "结果数量限制 (0表示无限制)")
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "output", "f", "table", "输出格式: table|json|html|svg")
}

func getFilterOptions() (analyzer.FilterOptions, error) {
	opts := analyzer.FilterOptions{
		Service:   serviceFilter,
		Operation: operationFilter,
		TraceID:   traceIDFilter,
	}

	var err error
	if startTimeStr != "" {
		opts.StartTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			return opts, fmt.Errorf("解析开始时间失败: %w", err)
		}
	}

	if endTimeStr != "" {
		opts.EndTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			return opts, fmt.Errorf("解析结束时间失败: %w", err)
		}
	}

	return opts, nil
}

func initGRPCClient() error {
	var err error
	grpcClient, err = grpcclient.NewClient(configData, store)
	return err
}

func startReceiver(ctx context.Context, addr string) error {
	if err := initGRPCClient(); err != nil {
		return err
	}
	return grpcClient.StartReceiver(ctx, addr)
}
