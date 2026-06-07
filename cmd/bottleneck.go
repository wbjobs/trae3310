package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"trace-cli/pkg/analyzer"
	"trace-cli/pkg/output"
)

var bottleneckCmd = &cobra.Command{
	Use:   "bottleneck",
	Short: "分析跨服务调用瓶颈",
	Long:  `分析追踪数据中的性能瓶颈，找出耗时占比最高的路径和跨服务调用。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var bottlenecks []*analyzer.BottleneckPath
		var err error

		if useStreaming {
			streamOpts := getStreamFilterOptions()
			bottlenecks, err = streamAnalyzer.FindBottlenecks(streamOpts)
			if err != nil {
				return err
			}
		} else {
			var opts analyzer.FilterOptions
			opts, err = getFilterOptions()
			if err != nil {
				return err
			}
			bottlenecks = analyzerInstance.FindBottlenecks(opts)
		}

		switch outputFormat {
		case "table":
			output.PrintBottlenecks(bottlenecks, limit)
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
	rootCmd.AddCommand(bottleneckCmd)
}
