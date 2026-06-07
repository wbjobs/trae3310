package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

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
		mock.GenerateSampleData(store, numTraces)
		fmt.Printf("✅ 成功生成 %d 条追踪数据，共包含 %d 个Span\n",
			store.Len(), len(store.GetAllSpans()))

		if limit > 0 {
			opts, err := getFilterOptions()
			if err != nil {
				return err
			}
			summaries := analyzerInstance.GetTraceSummaries(opts)
			output.PrintTraceSummaries(summaries, limit)
		}

		return nil
	},
}

func init() {
	generateCmd.Flags().IntVarP(&numTraces, "count", "n", 10, "生成的追踪数据数量")
	rootCmd.AddCommand(generateCmd)
}
