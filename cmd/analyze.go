package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"trace-cli/pkg/output"
)

var analyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "分析追踪数据",
	Long:  `显示追踪数据的概览信息，包括耗时、Span数量、服务数量等关键指标。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := getFilterOptions()
		if err != nil {
			return err
		}

		summaries := analyzerInstance.GetTraceSummaries(opts)

		switch outputFormat {
		case "table":
			output.PrintTraceSummaries(summaries, limit)
		default:
			return fmt.Errorf("不支持的输出格式: %s (支持: table)", outputFormat)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(analyzeCmd)
}
