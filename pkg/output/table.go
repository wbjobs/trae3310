package output

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/olekukonko/tablewriter"

	"trace-cli/pkg/analyzer"
)

func PrintTraceSummaries(summaries []*analyzer.TraceSummary, limit int) {
	if len(summaries) == 0 {
		fmt.Println("⚠️  没有找到匹配的追踪数据")
		return
	}

	if limit > 0 && limit < len(summaries) {
		summaries = summaries[:limit]
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"#", "Trace ID", "服务", "操作", "耗时", "Span数", "服务数", "错误", "慢Span"})
	table.SetAutoWrapText(false)
	table.SetAutoFormatHeaders(true)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetBorder(true)
	table.SetRowLine(false)

	for i, sum := range summaries {
		hasError := "❌"
		if !sum.HasErrors {
			hasError = "✅"
		}

		slowStr := fmt.Sprintf("%d", sum.SlowSpans)
		if sum.SlowSpans > 0 {
			slowStr = fmt.Sprintf("🔴 %d", sum.SlowSpans)
		}

		table.Append([]string{
			fmt.Sprintf("%d", i+1),
			sum.TraceID,
			sum.RootService,
			sum.RootOperation,
			formatDuration(sum.Duration),
			fmt.Sprintf("%d", sum.NumSpans),
			fmt.Sprintf("%d", sum.NumServices),
			hasError,
			slowStr,
		})
	}

	table.Render()
	fmt.Printf("\n📊 共显示 %d 条追踪记录\n", len(summaries))
}

func PrintSlowQueries(slowQueries []*analyzer.SlowQuery, limit int) {
	if len(slowQueries) == 0 {
		fmt.Println("✅ 没有发现慢查询（耗时超过阈值的追踪）")
		return
	}

	if limit > 0 && limit < len(slowQueries) {
		slowQueries = slowQueries[:limit]
	}

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("🔍 慢查询分析结果")
	fmt.Println(strings.Repeat("=", 80))

	for i, sq := range slowQueries {
		fmt.Printf("\n%d. Trace ID: %s\n", i+1, sq.TraceID)
		fmt.Printf("   入口服务: %s::%s\n", sq.RootService, sq.RootOperation)
		fmt.Printf("   总耗时: %s\n", formatDuration(sq.Duration))
		fmt.Printf("   Span数量: %d, 涉及服务: %d\n", sq.NumSpans, sq.NumServices)
		fmt.Println("   调用链:")

		printSpanChain(sq.SpanChain, 0, "")

		fmt.Println(strings.Repeat("-", 80))
	}

	fmt.Printf("\n📊 共发现 %d 条慢查询\n", len(slowQueries))
}

func printSpanChain(spans []*analyzer.SpanInfo, startIdx int, indent string) int {
	if startIdx >= len(spans) {
		return startIdx
	}

	current := spans[startIdx]
	durationStr := formatDuration(current.Duration)
	if current.Duration > 100*time.Millisecond {
		durationStr = "🔴 " + durationStr
	}

	fmt.Printf("%s├─ %s::%s %s\n", indent, current.Service, current.Operation, durationStr)

	childIndent := indent + "│  "
	idx := startIdx + 1

	for idx < len(spans) {
		next := spans[idx]
		if next.ParentID == current.SpanID {
			idx = printSpanChain(spans, idx, childIndent)
		} else {
			break
		}
	}

	return idx
}

func PrintBottlenecks(bottlenecks []*analyzer.BottleneckPath, limit int) {
	if len(bottlenecks) == 0 {
		fmt.Println("⚠️  没有找到瓶颈分析数据")
		return
	}

	if limit > 0 && limit < len(bottlenecks) {
		bottlenecks = bottlenecks[:limit]
	}

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("⚡ 跨服务调用瓶颈分析")
	fmt.Println(strings.Repeat("=", 80))

	for i, bn := range bottlenecks {
		fmt.Printf("\n%d. Trace ID: %s\n", i+1, bn.TraceID)
		fmt.Printf("   总耗时: %s, 关键路径耗时: %s (%.1f%%)\n",
			formatDuration(bn.TotalDuration),
			formatDuration(bn.CriticalDuration),
			float64(bn.CriticalDuration)/float64(bn.TotalDuration)*100)

		if bn.SlowestSpan != nil {
			fmt.Printf("   最慢Span: %s::%s (%s)\n",
				bn.SlowestSpan.Service, bn.SlowestSpan.Operation, formatDuration(bn.SlowestSpan.Duration))
		}

		if len(bn.CrossServiceCalls) > 0 {
			fmt.Println("\n   跨服务调用:")
			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"调用方向", "操作", "耗时", "占比"})
			table.SetAutoWrapText(false)
			table.SetAlignment(tablewriter.ALIGN_LEFT)

			for _, call := range bn.CrossServiceCalls {
				table.Append([]string{
					fmt.Sprintf("%s → %s", call.FromService, call.ToService),
					call.Operation,
					formatDuration(call.Duration),
					fmt.Sprintf("%.2f%%", call.Percentage),
				})
			}
			table.Render()
		}

		fmt.Println("\n   关键路径:")
		for j, span := range bn.CriticalPath {
			marker := "├─"
			if j == len(bn.CriticalPath)-1 {
				marker = "└─"
			}
			durationStr := formatDuration(span.Duration)
			if span.Duration > 100*time.Millisecond {
				durationStr = "🔴 " + durationStr
			}
			fmt.Printf("   %s %s::%s %s\n", marker, span.Service, span.Operation, durationStr)
		}

		fmt.Println(strings.Repeat("-", 80))
	}

	fmt.Printf("\n📊 共分析 %d 条追踪的瓶颈\n", len(bottlenecks))
}

func PrintSpanDetails(spans []*analyzer.SpanInfo) {
	if len(spans) == 0 {
		fmt.Println("⚠️  没有Span数据")
		return
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"#", "服务", "操作", "开始时间", "结束时间", "耗时", "Span ID"})
	table.SetAutoWrapText(false)
	table.SetAlignment(tablewriter.ALIGN_LEFT)

	for i, span := range spans {
		durationStr := formatDuration(span.Duration)
		if span.Duration > 100*time.Millisecond {
			durationStr = "🔴 " + durationStr
		}

		table.Append([]string{
			fmt.Sprintf("%d", i+1),
			span.Service,
			span.Operation,
			span.StartTime.Format("15:04:05.000"),
			span.EndTime.Format("15:04:05.000"),
			durationStr,
			span.SpanID,
		})
	}

	table.Render()
}

func formatDuration(d time.Duration) string {
	if d >= time.Second {
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
	if d >= time.Millisecond {
		return fmt.Sprintf("%.2fms", float64(d.Nanoseconds())/1e6)
	}
	return fmt.Sprintf("%dμs", d.Microseconds())
}
