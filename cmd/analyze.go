package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"trace-cli/pkg/analyzer"
	"trace-cli/pkg/output"
)

var analyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "分析追踪数据",
	Long:  `显示追踪数据的概览信息，包括耗时、Span数量、服务数量等关键指标。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var summaries []*analyzer.TraceSummary
		var err error

		if useStreaming {
			streamOpts := getStreamFilterOptions()
			summaries, err = streamAnalyzer.GetTraceSummaries(streamOpts)
			if err != nil {
				return err
			}
		} else {
			var opts analyzer.FilterOptions
			opts, err = getFilterOptions()
			if err != nil {
				return err
			}
			summaries = analyzerInstance.GetTraceSummaries(opts)
		}

		switch outputFormat {
		case "table":
			output.PrintTraceSummaries(summaries, limit)
		default:
			return fmt.Errorf("不支持的输出格式: %s (支持: table)", outputFormat)
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
	rootCmd.AddCommand(analyzeCmd)
}
