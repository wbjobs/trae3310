package analyzer

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"trace-cli/pkg/models"
	"trace-cli/pkg/storage"
)

type Analyzer struct {
	store *storage.TraceStore
}

func NewAnalyzer(store *storage.TraceStore) *Analyzer {
	return &Analyzer{store: store}
}

type FilterOptions struct {
	Service    string
	Operation  string
	TraceID    string
	StartTime  time.Time
	EndTime    time.Time
}

func (a *Analyzer) FilterTraces(opts FilterOptions) []*models.Trace {
	storeOpts := storage.FilterOptions{
		Service:   opts.Service,
		Operation: opts.Operation,
		TraceID:   opts.TraceID,
		StartTime: opts.StartTime,
		EndTime:   opts.EndTime,
	}
	return a.store.QueryTraces(storeOpts)
}

type SlowQuery struct {
	TraceID     string
	RootService string
	RootOperation string
	Duration    time.Duration
	SpanChain   []*SpanInfo
	NumSpans    int
	NumServices int
}

type SpanInfo struct {
	Service   string
	Operation string
	Duration  time.Duration
	StartTime time.Time
	EndTime   time.Time
	SpanID    string
	ParentID  string
}

func (a *Analyzer) FindSlowQueries(opts FilterOptions, thresholdMs int64) []*SlowQuery {
	traces := a.FilterTraces(opts)
	threshold := time.Duration(thresholdMs) * time.Millisecond

	var slowQueries []*SlowQuery

	for _, trace := range traces {
		if trace.Duration() < threshold {
			continue
		}

		spanChain := a.buildSpanChain(trace)
		services := make(map[string]bool)
		for _, s := range spanChain {
			services[s.Service] = true
		}

		root := trace.RootSpan()
		rootService := ""
		rootOperation := ""
		if root != nil {
			rootService = root.Service
			rootOperation = root.Operation
		}

		slowQueries = append(slowQueries, &SlowQuery{
			TraceID:       trace.TraceID,
			RootService:   rootService,
			RootOperation: rootOperation,
			Duration:      trace.Duration(),
			SpanChain:     spanChain,
			NumSpans:      len(trace.Spans),
			NumServices:   len(services),
		})
	}

	sort.Slice(slowQueries, func(i, j int) bool {
		return slowQueries[i].Duration > slowQueries[j].Duration
	})

	return slowQueries
}

func (a *Analyzer) buildSpanChain(trace *models.Trace) []*SpanInfo {
	root := trace.RootSpan()
	if root == nil {
		return []*SpanInfo{}
	}

	return a.traverseSpan(trace, root, "")
}

func (a *Analyzer) traverseSpan(trace *models.Trace, span *models.Span, indent string) []*SpanInfo {
	var chain []*SpanInfo

	chain = append(chain, &SpanInfo{
		Service:   span.Service,
		Operation: span.Operation,
		Duration:  span.Duration,
		StartTime: span.StartTime,
		EndTime:   span.EndTime,
		SpanID:    span.SpanID,
		ParentID:  span.ParentID,
	})

	children := trace.Children(span.SpanID)
	sort.Slice(children, func(i, j int) bool {
		return children[i].StartTime.Before(children[j].StartTime)
	})

	for _, child := range children {
		childChain := a.traverseSpan(trace, child, indent+"  ")
		chain = append(chain, childChain...)
	}

	return chain
}

type BottleneckPath struct {
	TraceID           string
	Path              []*SpanInfo
	TotalDuration     time.Duration
	CriticalPath      []*SpanInfo
	CriticalDuration  time.Duration
	SlowestSpan       *SpanInfo
	CrossServiceCalls []*CrossServiceCall
}

type CrossServiceCall struct {
	FromService string
	ToService   string
	Operation   string
	Duration    time.Duration
	Percentage  float64
}

func (a *Analyzer) FindBottlenecks(opts FilterOptions) []*BottleneckPath {
	traces := a.FilterTraces(opts)

	var bottlenecks []*BottleneckPath

	for _, trace := range traces {
		bn := a.analyzeTraceBottleneck(trace)
		if bn != nil {
			bottlenecks = append(bottlenecks, bn)
		}
	}

	sort.Slice(bottlenecks, func(i, j int) bool {
		return bottlenecks[i].CriticalDuration > bottlenecks[j].CriticalDuration
	})

	return bottlenecks
}

func (a *Analyzer) analyzeTraceBottleneck(trace *models.Trace) *BottleneckPath {
	if len(trace.Spans) == 0 {
		return nil
	}

	totalDuration := trace.Duration()

	criticalPath := a.findCriticalPath(trace)
	criticalDuration := a.calculatePathDuration(criticalPath)

	spanChain := a.buildSpanChain(trace)

	var slowest *SpanInfo
	for _, span := range spanChain {
		if slowest == nil || span.Duration > slowest.Duration {
			slowest = span
		}
	}

	crossServiceCalls := a.findCrossServiceCalls(trace, totalDuration)

	return &BottleneckPath{
		TraceID:           trace.TraceID,
		Path:              spanChain,
		TotalDuration:     totalDuration,
		CriticalPath:      criticalPath,
		CriticalDuration:  criticalDuration,
		SlowestSpan:       slowest,
		CrossServiceCalls: crossServiceCalls,
	}
}

func (a *Analyzer) findCriticalPath(trace *models.Trace) []*SpanInfo {
	root := trace.RootSpan()
	if root == nil {
		return []*SpanInfo{}
	}

	return a.findLongestPath(trace, root)
}

func (a *Analyzer) findLongestPath(trace *models.Trace, span *models.Span) []*SpanInfo {
	children := trace.Children(span.SpanID)
	if len(children) == 0 {
		return []*SpanInfo{{
			Service:   span.Service,
			Operation: span.Operation,
			Duration:  span.Duration,
			StartTime: span.StartTime,
			EndTime:   span.EndTime,
			SpanID:    span.SpanID,
			ParentID:  span.ParentID,
		}}
	}

	var longestChildPath []*SpanInfo
	var longestDuration time.Duration

	for _, child := range children {
		childPath := a.findLongestPath(trace, child)
		childDuration := a.calculatePathDuration(childPath)
		if childDuration > longestDuration {
			longestDuration = childDuration
			longestChildPath = childPath
		}
	}

	current := &SpanInfo{
		Service:   span.Service,
		Operation: span.Operation,
		Duration:  span.Duration,
		StartTime: span.StartTime,
		EndTime:   span.EndTime,
		SpanID:    span.SpanID,
		ParentID:  span.ParentID,
	}

	return append([]*SpanInfo{current}, longestChildPath...)
}

func (a *Analyzer) calculatePathDuration(path []*SpanInfo) time.Duration {
	var total time.Duration
	for _, s := range path {
		total += s.Duration
	}
	return total
}

func (a *Analyzer) findCrossServiceCalls(trace *models.Trace, totalDuration time.Duration) []*CrossServiceCall {
	var calls []*CrossServiceCall
	seen := make(map[string]bool)

	for _, span := range trace.Spans {
		parent := trace.GetSpan(span.ParentID)
		if parent == nil {
			continue
		}

		if parent.Service != span.Service {
			key := fmt.Sprintf("%s->%s:%s", parent.Service, span.Service, span.Operation)
			if seen[key] {
				continue
			}
			seen[key] = true

			percentage := 0.0
			if totalDuration > 0 {
				percentage = float64(span.Duration) / float64(totalDuration) * 100
			}

			calls = append(calls, &CrossServiceCall{
				FromService: parent.Service,
				ToService:   span.Service,
				Operation:   span.Operation,
				Duration:    span.Duration,
				Percentage:  percentage,
			})
		}
	}

	sort.Slice(calls, func(i, j int) bool {
		return calls[i].Percentage > calls[j].Percentage
	})

	return calls
}

type TraceSummary struct {
	TraceID       string
	RootService   string
	RootOperation string
	Duration      time.Duration
	NumSpans      int
	NumServices   int
	HasErrors     bool
	SlowSpans     int
}

func (a *Analyzer) GetTraceSummaries(opts FilterOptions) []*TraceSummary {
	traces := a.FilterTraces(opts)

	var summaries []*TraceSummary

	for _, trace := range traces {
		services := make(map[string]bool)
		hasErrors := false
		slowSpans := 0

		for _, span := range trace.Spans {
			services[span.Service] = true
			if span.Status.Code == models.StatusError {
				hasErrors = true
			}
			if span.Duration > 100*time.Millisecond {
				slowSpans++
			}
		}

		root := trace.RootSpan()
		rootService := ""
		rootOperation := ""
		if root != nil {
			rootService = root.Service
			rootOperation = root.Operation
		}

		summaries = append(summaries, &TraceSummary{
			TraceID:       trace.TraceID,
			RootService:   rootService,
			RootOperation: rootOperation,
			Duration:      trace.Duration(),
			NumSpans:      len(trace.Spans),
			NumServices:   len(services),
			HasErrors:     hasErrors,
			SlowSpans:     slowSpans,
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Duration > summaries[j].Duration
	})

	return summaries
}

func (si *SpanInfo) String() string {
	return fmt.Sprintf("%s::%s (%s)", si.Service, si.Operation, si.Duration)
}

func (sq *SlowQuery) String() string {
	var chainStr []string
	for _, s := range sq.SpanChain {
		chainStr = append(chainStr, s.String())
	}
	return fmt.Sprintf("Trace %s: %sms\n  Chain: %s",
		sq.TraceID, sq.Duration.Milliseconds(), strings.Join(chainStr, " -> "))
}
