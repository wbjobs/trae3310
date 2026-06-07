package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"trace-cli/pkg/analyzer"
	"trace-cli/pkg/mock"
	"trace-cli/pkg/output"
)

var (
	numTraces int
)

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "生成模拟追踪数据",
	Long:  `生成模拟的分布式追踪数据，用于测试和演示功能。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("🎲 正在生成 %d 条模拟追踪数据...\n", numTraces)

		var spanCount int
		if useStreaming {
			mock.GenerateSampleDataForStream(streamIndex, numTraces)
			spanCount = int(streamIndex.MemoryUsage())
			traceCount := int(streamIndex.TraceCount())
			fmt.Printf("✅ 流式模式: 成功生成 %d 条追踪数据，共包含 %d 个Span\n",
				traceCount, spanCount)

			memorySpans := spillThreshold
			if spanCount < spillThreshold {
				memorySpans = spanCount
			}
			spilled := 0
			if spanCount > spillThreshold {
				spilled = spanCount - spillThreshold
			}
			fmt.Printf("💾 内存中 %d 个Span，已溢出到磁盘 %d 个Span\n",
				memorySpans, spilled)
		} else {
			mock.GenerateSampleData(store, numTraces)
			spanCount = len(store.GetAllSpans())
			fmt.Printf("✅ 成功生成 %d 条追踪数据，共包含 %d 个Span\n",
				store.Len(), spanCount)
		}

		if limit > 0 {
			if useStreaming {
				streamOpts := getStreamFilterOptions()
				summaries, err := streamAnalyzer.GetTraceSummaries(streamOpts)
				if err != nil {
					return err
				}
				output.PrintTraceSummaries(summaries, limit)
			} else {
				opts, err := getFilterOptions()
				if err != nil {
					return err
				}
				summaries := analyzerInstance.GetTraceSummaries(opts)
				output.PrintTraceSummaries(summaries, limit)
			}
		}

		return nil
	},
}

func init() {
	generateCmd.Flags().IntVarP(&numTraces, "count", "n", 10, "生成的追踪数据数量")
	rootCmd.AddCommand(generateCmd)
}
