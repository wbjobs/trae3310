package output

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"trace-cli/pkg/aggregator"
)

func PrintAggregateResult(result *aggregator.AggregateResult, limit int) {
	fmt.Println()
	fmt.Println("══════════════════════════════════════════════════════════════════════════════")
	fmt.Println("                        📊 聚合统计结果")
	fmt.Println("══════════════════════════════════════════════════════════════════════════════")
	fmt.Println()

	fmt.Printf("⏰ 时间范围: %s  →  %s\n",
		result.StartTime.Format("2006-01-02 15:04:05"),
		result.EndTime.Format("2006-01-02 15:04:05"))
	fmt.Printf("📈 总Trace数: %d\n", result.TotalTraces)
	fmt.Printf("📊 总Span数: %d\n", result.TotalSpans)
	fmt.Printf("🔧 操作数: %d\n", len(result.Operations))
	fmt.Printf("🕐 更新时间: %s\n", result.UpdatedAt.Format("2006-01-02 15:04:05"))
	fmt.Println()

	if len(result.Operations) == 0 {
		fmt.Println("⚠️  没有找到符合条件的操作数据")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	defer w.Flush()

	fmt.Fprintln(w, "服务\t操作\t调用次数\t错误数\t错误率\tP50\tP95\tP99\t最小\t最大")
	fmt.Fprintln(w, "----\t----\t--------\t------\t------\t---\t---\t---\t----\t----")

	ops := result.Operations
	keys := make([]string, 0, len(ops))
	for k := range ops {
		keys = append(keys, k)
	}

	sort.Slice(keys, func(i, j int) bool {
		return ops[keys[i]].CallCount > ops[keys[j]].CallCount
	})

	displayCount := len(keys)
	if limit > 0 && limit < displayCount {
		displayCount = limit
	}

	for i := 0; i < displayCount; i++ {
		op := ops[keys[i]]

		status := "✅"
		if op.ErrorRate > 0.05 {
			status = "⚠️"
		}
		if op.ErrorRate > 0.1 {
			status = "❌"
		}

		fmt.Fprintf(w, "%s\t%s\t%s%d\t%s%d\t%s%.2f%%\t%s\t%s\t%s\t%s\t%s\n",
			op.Service,
			op.Operation,
			status, op.CallCount,
			status, op.ErrorCount,
			status, op.ErrorRate*100,
			formatDuration(op.P50),
			formatDuration(op.P95),
			formatDuration(op.P99),
			formatDuration(op.MinDuration),
			formatDuration(op.MaxDuration),
		)
	}

	if limit > 0 && len(keys) > limit {
		fmt.Fprintf(w, "...\t...\t(还有 %d 个操作)\t\t\t\t\t\t\t\n", len(keys)-limit)
	}

	fmt.Fprintln(w)
}

func PrintAggregateSummary(result *aggregator.AggregateResult) {
	fmt.Println()
	fmt.Println("📋 聚合摘要")
	fmt.Println("─────────────────────────────────────────")

	ops := result.Operations
	totalCalls := int64(0)
	totalErrors := int64(0)
	avgP50 := time.Duration(0)
	avgP95 := time.Duration(0)
	avgP99 := time.Duration(0)
	count := 0

	for _, op := range ops {
		totalCalls += op.CallCount
		totalErrors += op.ErrorCount
		avgP50 += op.P50
		avgP95 += op.P95
		avgP99 += op.P99
		count++
	}

	if count > 0 {
		avgP50 /= time.Duration(count)
		avgP95 /= time.Duration(count)
		avgP99 /= time.Duration(count)
	}

	errorRate := float64(0)
	if totalCalls > 0 {
		errorRate = float64(totalErrors) / float64(totalCalls) * 100
	}

	fmt.Printf("  总调用次数: %d\n", totalCalls)
	fmt.Printf("  总错误次数: %d\n", totalErrors)
	fmt.Printf("  平均错误率: %.2f%%\n", errorRate)
	fmt.Printf("  平均 P50: %s\n", formatDuration(avgP50))
	fmt.Printf("  平均 P95: %s\n", formatDuration(avgP95))
	fmt.Printf("  平均 P99: %s\n", formatDuration(avgP99))
	fmt.Println("─────────────────────────────────────────")
	fmt.Println()
}

func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000.0)
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}
