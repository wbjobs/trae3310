package stream

import (
	"fmt"
	"sort"
	"time"

	"trace-cli/pkg/analyzer"
	"trace-cli/pkg/models"
)

type StreamAnalyzer struct {
	index *StreamIndex
}

func NewStreamAnalyzer(index *StreamIndex) *StreamAnalyzer {
	return &StreamAnalyzer{index: index}
}

type StreamFilterOptions struct {
	Service   string
	Operation string
	TraceID   string
	StartTime time.Time
	EndTime   time.Time
}

func (sa *StreamAnalyzer) FilterTraceIDs(opts StreamFilterOptions) []string {
	if opts.TraceID != "" {
		if _, ok := sa.index.GetTraceInfo(opts.TraceID); ok {
			return []string{opts.TraceID}
		}
		return nil
	}

	return sa.index.FilterTraceIDs(opts.Service, opts.Operation, opts.StartTime, opts.EndTime)
}

func (sa *StreamAnalyzer) FindSlowQueries(opts StreamFilterOptions, thresholdMs int64) ([]*analyzer.SlowQuery, error) {
	traceIDs := sa.FilterTraceIDs(opts)
	threshold := time.Duration(thresholdMs) * time.Millisecond

	var results []*analyzer.SlowQuery

	for _, traceID := range traceIDs {
		info, ok := sa.index.GetTraceInfo(traceID)
		if !ok {
			continue
		}

		duration := time.Duration(info.MaxTime - info.MinTime)
		if duration < threshold {
			continue
		}

		rootSpan, err := sa.index.LoadSpan(info.RootSpanID)
		if err != nil {
			return nil, fmt.Errorf("failed to load root span: %w", err)
		}

		spanChain, err := sa.GetSpanChain(traceID)
		if err != nil {
			return nil, err
		}

		slowCount := 0
		for _, s := range spanChain {
			if s.Duration > 100*time.Millisecond {
				slowCount++
			}
		}

		results = append(results, &analyzer.SlowQuery{
			TraceID:       traceID,
			RootService:   rootSpan.Service,
			RootOperation: rootSpan.Operation,
			Duration:      duration,
			NumSpans:      info.SpanCount,
			NumServices:   len(info.Services),
			SlowSpans:     slowCount,
			SpanChain:     spanChain,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Duration > results[j].Duration
	})

	return results, nil
}

func (sa *StreamAnalyzer) FindBottlenecks(opts StreamFilterOptions) ([]*analyzer.BottleneckPath, error) {
	traceIDs := sa.FilterTraceIDs(opts)

	var results []*analyzer.BottleneckPath

	for _, traceID := range traceIDs {
		bn, err := sa.analyzeSingleBottleneck(traceID)
		if err != nil {
			return nil, err
		}
		if bn != nil {
			results = append(results, bn)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].CriticalDuration > results[j].CriticalDuration
	})

	return results, nil
}

func (sa *StreamAnalyzer) analyzeSingleBottleneck(traceID string) (*analyzer.BottleneckPath, error) {
	info, ok := sa.index.GetTraceInfo(traceID)
	if !ok || info.SpanCount == 0 {
		return nil, nil
	}

	totalDuration := time.Duration(info.MaxTime - info.MinTime)

	criticalPath, err := sa.findCriticalPath(traceID)
	if err != nil {
		return nil, err
	}

	fullPath, err := sa.GetSpanChain(traceID)
	if err != nil {
		return nil, err
	}

	var criticalDuration time.Duration
	var slowest *analyzer.SpanInfo
	for _, s := range criticalPath {
		criticalDuration += s.Duration
		if slowest == nil || s.Duration > slowest.Duration {
			slowest = s
		}
	}

	crossServiceCalls, err := sa.findCrossServiceCalls(traceID, totalDuration)
	if err != nil {
		return nil, err
	}

	return &analyzer.BottleneckPath{
		TraceID:           traceID,
		TotalDuration:     totalDuration,
		CriticalPath:      criticalPath,
		CriticalDuration:  criticalDuration,
		SlowestSpan:       slowest,
		CrossServiceCalls: crossServiceCalls,
		Path:              fullPath,
	}, nil
}

func (sa *StreamAnalyzer) findCriticalPath(traceID string) ([]*analyzer.SpanInfo, error) {
	info, ok := sa.index.GetTraceInfo(traceID)
	if !ok || info.RootSpanID == "" {
		return nil, fmt.Errorf("invalid trace: %s", traceID)
	}

	var longestPath []*analyzer.SpanInfo
	var longestDuration time.Duration

	var dfs func(spanID string, currentPath []*analyzer.SpanInfo, currentDuration time.Duration)
	dfs = func(spanID string, currentPath []*analyzer.SpanInfo, currentDuration time.Duration) {
		span, err := sa.index.LoadSpan(spanID)
		if err != nil {
			return
		}

		spanInfo := &analyzer.SpanInfo{
			Service:   span.Service,
			Operation: span.Operation,
			Duration:  span.Duration,
			StartTime: span.StartTime,
			EndTime:   span.EndTime,
			SpanID:    span.SpanID,
			ParentID:  span.ParentID,
		}

		newPath := append(currentPath, spanInfo)
		newDuration := currentDuration + span.Duration

		children := sa.index.GetChildren(spanID)
		if len(children) == 0 {
			if newDuration > longestDuration {
				longestDuration = newDuration
				longestPath = make([]*analyzer.SpanInfo, len(newPath))
				copy(longestPath, newPath)
			}
			return
		}

		for _, childID := range children {
			dfs(childID, newPath, newDuration)
		}
	}

	dfs(info.RootSpanID, nil, 0)

	return longestPath, nil
}

func (sa *StreamAnalyzer) findCrossServiceCalls(traceID string, totalDuration time.Duration) ([]*analyzer.CrossServiceCall, error) {
	var calls []*analyzer.CrossServiceCall
	seen := make(map[string]bool)

	spanIDs, ok := sa.index.GetTraceSpanIDs(traceID)
	if !ok {
		return nil, nil
	}

	for _, spanID := range spanIDs {
		ref, ok := sa.index.GetSpanRef(spanID)
		if !ok || ref.ParentID == "" {
			continue
		}

		parentRef, ok := sa.index.GetSpanRef(ref.ParentID)
		if !ok {
			continue
		}

		if parentRef.Service != ref.Service {
			key := fmt.Sprintf("%s->%s:%s", parentRef.Service, ref.Service, ref.Operation)
			if seen[key] {
				continue
			}
			seen[key] = true

			percentage := 0.0
			if totalDuration > 0 {
				percentage = float64(time.Duration(ref.DurationMs)*time.Millisecond) / float64(totalDuration) * 100
			}

			calls = append(calls, &analyzer.CrossServiceCall{
				FromService: parentRef.Service,
				ToService:   ref.Service,
				Operation:   ref.Operation,
				Duration:    time.Duration(ref.DurationMs) * time.Millisecond,
				Percentage:  percentage,
			})
		}
	}

	sort.Slice(calls, func(i, j int) bool {
		return calls[i].Percentage > calls[j].Percentage
	})

	return calls, nil
}

func (sa *StreamAnalyzer) GetTraceSummaries(opts StreamFilterOptions) ([]*analyzer.TraceSummary, error) {
	traceIDs := sa.FilterTraceIDs(opts)

	var summaries []*analyzer.TraceSummary

	for _, traceID := range traceIDs {
		info, ok := sa.index.GetTraceInfo(traceID)
		if !ok {
			continue
		}

		rootSpan, err := sa.index.LoadSpan(info.RootSpanID)
		if err != nil {
			return nil, fmt.Errorf("failed to load root span for trace %s: %w", traceID, err)
		}

		slowSpans := 0
		err = sa.index.WalkTrace(traceID, func(span *models.Span, depth int) error {
			if span.Duration > 100*time.Millisecond {
				slowSpans++
			}
			return nil
		})
		if err != nil {
			return nil, err
		}

		summaries = append(summaries, &analyzer.TraceSummary{
			TraceID:       traceID,
			RootService:   rootSpan.Service,
			RootOperation: rootSpan.Operation,
			Duration:      time.Duration(info.MaxTime - info.MinTime),
			NumSpans:      info.SpanCount,
			NumServices:   len(info.Services),
			HasErrors:     info.HasError,
			SlowSpans:     slowSpans,
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Duration > summaries[j].Duration
	})

	return summaries, nil
}

func (sa *StreamAnalyzer) GetSpanChain(traceID string) ([]*analyzer.SpanInfo, error) {
	var chain []*analyzer.SpanInfo

	err := sa.index.WalkTrace(traceID, func(span *models.Span, depth int) error {
		chain = append(chain, &analyzer.SpanInfo{
			Service:   span.Service,
			Operation: span.Operation,
			Duration:  span.Duration,
			StartTime: span.StartTime,
			EndTime:   span.EndTime,
			SpanID:    span.SpanID,
			ParentID:  span.ParentID,
		})
		return nil
	})

	if err != nil {
		return nil, err
	}

	return chain, nil
}
