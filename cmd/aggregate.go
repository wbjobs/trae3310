package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"trace-cli/pkg/aggregator"
	"trace-cli/pkg/analyzer"
	"trace-cli/pkg/output"
)

var aggregateCmd = &cobra.Command{
	Use:   "aggregate",
	Short: "时间段聚合分析",
	Long:  `分析指定时间段内所有trace的聚合统计，包括每个operation的P50/P95/P99延迟、错误率、调用次数。`,
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

		fmt.Printf("🔍 正在聚合分析...\n")
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

		switch outputFormat {
		case "table":
			output.PrintAggregateResult(result, limit)
			output.PrintAggregateSummary(result)
		case "json":
			return fmt.Errorf("JSON输出格式暂未实现")
		default:
			return fmt.Errorf("不支持的输出格式: %s (支持: table|json)", outputFormat)
		}

		if useStreaming {
			memoryCount := streamIndex.MemoryUsage()
			spilled := int64(0)
			if memoryCount > int64(spillThreshold) {
				spilled = memoryCount - int64(spillThreshold)
			}
			fmt.Printf("\n💾 流式模式: 总span数 %d, 内存中 %d 个, 已溢出到磁盘 %d 个\n",
				memoryCount,
				memoryCount-spilled,
				spilled)
		}

		return nil
	},
}

func init() {
	aggregateCmd.Flags().StringVarP(&timeWindowStr, "time-window", "w", "1h", "时间窗口，如 1h、30m、24h (默认1小时)")
	aggregateCmd.Flags().StringVarP(&serviceFilter, "service", "s", "", "按服务名称过滤")
	aggregateCmd.Flags().StringVarP(&operationFilter, "operation", "o", "", "按操作名称过滤")
	aggregateCmd.Flags().IntVarP(&limit, "limit", "l", 0, "结果数量限制 (0表示无限制)")
	rootCmd.AddCommand(aggregateCmd)
}
