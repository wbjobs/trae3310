package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"trace-cli/pkg/output"
)

var (
	slowThreshold int64
)

var slowCmd = &cobra.Command{
	Use:   "slow",
	Short: "识别慢查询",
	Long:  `自动识别耗时超过指定阈值的追踪，显示完整的调用链。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := getFilterOptions()
		if err != nil {
			return err
		}

		threshold := slowThreshold
		if threshold == 0 {
			threshold = configData.Analysis.SlowQueryThresholdMs
		}

		slowQueries := analyzerInstance.FindSlowQueries(opts, threshold)

		switch outputFormat {
		case "table":
			fmt.Printf("⏱️  慢查询阈值: %dms\n\n", threshold)
			output.PrintSlowQueries(slowQueries, limit)
		default:
			return fmt.Errorf("不支持的输出格式: %s (支持: table)", outputFormat)
		}

		return nil
	},
}

func init() {
	slowCmd.Flags().Int64VarP(&slowThreshold, "threshold", "", 0, "慢查询阈值(毫秒)，默认使用配置文件中的值")
	rootCmd.AddCommand(slowCmd)
}
