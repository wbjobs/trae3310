package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"trace-cli/pkg/aggregator"
	"trace-cli/pkg/analyzer"
	"trace-cli/pkg/metrics"
)

var metricsCmd = &cobra.Command{
	Use:   "metrics",
	Short: "导出Prometheus metrics格式",
	Long:  `将聚合统计结果导出为Prometheus metrics格式，支持输出到文件或stdout。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var agg *aggregator.Aggregator
		if useStreaming {
			agg = aggregator.NewStreamingAggregator(streamIndex)
		} else {
			agg = aggregator.NewAggregator(store)
		}

		var opts analyzer.FilterOptions
		var err error

		if timeWindowStr != "" {
			d, err := aggregator.ParseDuration(timeWindowStr)
			if err != nil {
				return fmt.Errorf("解析时间窗口失败: %w", err)
			}
			start, end := aggregator.GetTimeRangeFromDuration(d)
			opts.StartTime = start
			opts.EndTime = end
		} else {
			opts, err = getFilterOptions()
			if err != nil {
				return err
			}
			if opts.StartTime.IsZero() || opts.EndTime.IsZero() {
				return fmt.Errorf("必须指定 --time-window 参数或 --start-time 和 --end-time 参数")
			}
		}

		if opts.Service == "" {
			opts.Service = serviceFilter
		}
		if opts.Operation == "" {
			opts.Operation = operationFilter
		}

		fmt.Printf("🔍 正在生成metrics...\n")
		fmt.Printf("   时间范围: %s → %s\n",
			opts.StartTime.Format("2006-01-02 15:04:05"),
			opts.EndTime.Format("2006-01-02 15:04:05"))

		var result *aggregator.AggregateResult
		if useStreaming {
			streamOpts := getStreamFilterOptions()
			streamOpts.StartTime = opts.StartTime
			streamOpts.EndTime = opts.EndTime
			result, err = agg.AggregateStream(streamOpts)
		} else {
			result, err = agg.Aggregate(opts)
		}

		if err != nil {
			return fmt.Errorf("聚合分析失败: %w", err)
		}

		exporter := metrics.NewMetricsExporter(agg, metricsNamespace)
		metricsText := exporter.GenerateMetrics()

		if metricsOutputFile != "" {
			if err := os.WriteFile(metricsOutputFile, []byte(metricsText), 0644); err != nil {
				return fmt.Errorf("写入metrics文件失败: %w", err)
			}
			fmt.Printf("✅ Metrics已保存到: %s\n", metricsOutputFile)
		} else {
			fmt.Println()
			fmt.Println("══════════════════════════════════════════════════════════════════════════════")
			fmt.Println("                        📊 Prometheus Metrics")
			fmt.Println("══════════════════════════════════════════════════════════════════════════════")
			fmt.Println()
			fmt.Print(metricsText)
		}

		fmt.Printf("\n📈 统计: %d traces, %d spans, %d operations\n",
			result.TotalTraces, result.TotalSpans, len(result.Operations))

		return nil
	},
}

func init() {
	metricsCmd.Flags().StringVarP(&timeWindowStr, "time-window", "w", "1h", "时间窗口，如 1h、30m、24h (默认1小时)")
	metricsCmd.Flags().StringVarP(&serviceFilter, "service", "s", "", "按服务名称过滤")
	metricsCmd.Flags().StringVarP(&operationFilter, "operation", "o", "", "按操作名称过滤")
	metricsCmd.Flags().StringVar(&metricsNamespace, "namespace", "trace_cli", "Metrics命名空间前缀")
	metricsCmd.Flags().StringVarP(&metricsOutputFile, "output-file", "O", "", "输出文件路径 (默认输出到stdout)")
	rootCmd.AddCommand(metricsCmd)
}
