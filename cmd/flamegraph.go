package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"trace-cli/pkg/flamegraph"
	"trace-cli/pkg/models"
)

var (
	outputFile string
)

var flamegraphCmd = &cobra.Command{
	Use:   "flamegraph",
	Short: "生成火焰图",
	Long:  `为指定的Trace生成火焰图，支持SVG和HTML两种格式。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if traceIDFilter == "" {
			return fmt.Errorf("必须指定 --trace-id 参数")
		}

		var fg *flamegraph.FlameGraph
		var err error

		if useStreaming {
			fg, err = streamFlameGen.GenerateFromTrace(traceIDFilter)
			if err != nil {
				return fmt.Errorf("生成火焰图失败: %w", err)
			}
		} else {
			opts, errOpts := getFilterOptions()
			if errOpts != nil {
				return errOpts
			}

			traces := analyzerInstance.FilterTraces(opts)
			if len(traces) == 0 {
				return fmt.Errorf("未找到Trace ID为 %s 的追踪数据", traceIDFilter)
			}

			var trace *models.Trace
			for _, t := range traces {
				if t.TraceID == traceIDFilter {
					trace = t
					break
				}
			}

			if trace == nil {
				return fmt.Errorf("未找到Trace ID为 %s 的追踪数据", traceIDFilter)
			}

			fg = flamegraph.GenerateFromTrace(trace)
		}

		var outputPath string
		if outputFile == "" {
			outputPath = fmt.Sprintf("flamegraph_%s", traceIDFilter[:8])
		} else {
			outputPath = outputFile
		}

		switch outputFormat {
		case "svg":
			if outputFile == "" {
				outputPath += ".svg"
			}
			if err := fg.GenerateSVG(outputPath); err != nil {
				return fmt.Errorf("生成SVG火焰图失败: %w", err)
			}
			fmt.Printf("✅ SVG火焰图已生成: %s\n", outputPath)

		case "html":
			if outputFile == "" {
				outputPath += ".html"
			}
			if err := fg.GenerateHTML(outputPath); err != nil {
				return fmt.Errorf("生成HTML火焰图失败: %w", err)
			}
			fmt.Printf("✅ HTML火焰图已生成: %s\n", outputPath)

		default:
			return fmt.Errorf("不支持的输出格式: %s (支持: svg|html)", outputFormat)
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
	flamegraphCmd.Flags().StringVarP(&outputFile, "output-file", "O", "", "输出文件路径")
	flamegraphCmd.MarkFlagRequired("trace-id")
	rootCmd.AddCommand(flamegraphCmd)
}
