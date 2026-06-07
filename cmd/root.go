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
	"trace-cli/pkg/stream"
)

var (
	cfgFile    string
	configData *config.Config
	store      *storage.TraceStore
	analyzerInstance *analyzer.Analyzer
	grpcClient *grpcclient.Client

	streamIndex     *stream.StreamIndex
	streamAnalyzer  *stream.StreamAnalyzer
	streamFlameGen  *stream.StreamFlameGenerator
	useStreaming    bool
	spillThreshold  int
	tempDir         string

	serviceFilter   string
	operationFilter string
	traceIDFilter   string
	startTimeStr    string
	endTimeStr      string
	limit           int
	outputFormat    string
	timeWindowStr   string
	metricsNamespace string
	metricsOutputFile string
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

		if useStreaming {
			streamIndex, err = stream.NewStreamIndex(tempDir, spillThreshold)
			if err != nil {
				return fmt.Errorf("创建流式索引器失败: %w", err)
			}
			streamAnalyzer = stream.NewStreamAnalyzer(streamIndex)
			streamFlameGen = stream.NewStreamFlameGenerator(streamIndex)
		} else {
			store = storage.NewTraceStore()
			analyzerInstance = analyzer.NewAnalyzer(store)
		}

		return nil
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if grpcClient != nil {
			grpcClient.Close()
		}
		if streamIndex != nil {
			streamIndex.Close()
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
	rootCmd.PersistentFlags().BoolVar(&useStreaming, "stream", false, "启用流式处理模式（处理大型trace时使用）")
	rootCmd.PersistentFlags().IntVar(&spillThreshold, "spill-threshold", 10000, "内存span数量阈值，超过后写入磁盘")
	rootCmd.PersistentFlags().StringVar(&tempDir, "temp-dir", "", "临时文件目录（默认系统临时目录）")
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

func getStreamFilterOptions() stream.StreamFilterOptions {
	opts := stream.StreamFilterOptions{
		Service:   serviceFilter,
		Operation: operationFilter,
		TraceID:   traceIDFilter,
	}

	if startTimeStr != "" {
		opts.StartTime, _ = time.Parse(time.RFC3339, startTimeStr)
	}
	if endTimeStr != "" {
		opts.EndTime, _ = time.Parse(time.RFC3339, endTimeStr)
	}

	return opts
}

func initGRPCClient() error {
	var err error
	if useStreaming && streamIndex != nil {
		grpcClient, err = grpcclient.NewStreamingClient(configData, streamIndex)
	} else {
		grpcClient, err = grpcclient.NewClient(configData, store)
	}
	return err
}

func startReceiver(ctx context.Context, addr string) error {
	if err := initGRPCClient(); err != nil {
		return err
	}
	return grpcClient.StartReceiver(ctx, addr)
}
