package aggregator

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"trace-cli/pkg/analyzer"
	"trace-cli/pkg/models"
	"trace-cli/pkg/storage"
	"trace-cli/pkg/stream"
)

type OperationStats struct {
	Service      string
	Operation    string
	CallCount    int64
	ErrorCount   int64
	TotalDuration time.Duration
	MinDuration  time.Duration
	MaxDuration  time.Duration
	Durations    []time.Duration
	P50          time.Duration
	P95          time.Duration
	P99          time.Duration
	ErrorRate    float64
}

type AggregateResult struct {
	StartTime    time.Time
	EndTime      time.Time
	TotalSpans   int64
	TotalTraces  int64
	Operations   map[string]*OperationStats
	UpdatedAt    time.Time
}

type Aggregator struct {
	mu          sync.RWMutex
	store       *storage.TraceStore
	streamIndex *stream.StreamIndex
	result      *AggregateResult
	useStreaming bool
}

func NewAggregator(store *storage.TraceStore) *Aggregator {
	return &Aggregator{
		store: store,
		useStreaming: false,
	}
}

func NewStreamingAggregator(index *stream.StreamIndex) *Aggregator {
	return &Aggregator{
		streamIndex: index,
		useStreaming: true,
	}
}

func (a *Aggregator) Aggregate(opts analyzer.FilterOptions) (*AggregateResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	result := &AggregateResult{
		StartTime:  opts.StartTime,
		EndTime:    opts.EndTime,
		Operations: make(map[string]*OperationStats),
		UpdatedAt:  time.Now(),
	}

	var spans []*models.Span
	var err error

	if a.useStreaming {
		spans, err = a.getSpansFromStream(opts)
	} else {
		spans, err = a.getSpansFromStore(opts)
	}

	if err != nil {
		return nil, err
	}

	traceSet := make(map[string]bool)

	for _, span := range spans {
		traceSet[span.TraceID] = true
		result.TotalSpans++

		key := fmt.Sprintf("%s/%s", span.Service, span.Operation)
		stats, ok := result.Operations[key]
		if !ok {
			stats = &OperationStats{
				Service:     span.Service,
				Operation:   span.Operation,
				MinDuration: span.Duration,
				MaxDuration: span.Duration,
				Durations:   make([]time.Duration, 0),
			}
			result.Operations[key] = stats
		}

		stats.CallCount++
		stats.TotalDuration += span.Duration

		if span.Duration < stats.MinDuration {
			stats.MinDuration = span.Duration
		}
		if span.Duration > stats.MaxDuration {
			stats.MaxDuration = span.Duration
		}

		stats.Durations = append(stats.Durations, span.Duration)

		if span.Status.Code == models.StatusError {
			stats.ErrorCount++
		}
	}

	result.TotalTraces = int64(len(traceSet))

	for _, stats := range result.Operations {
		if stats.CallCount > 0 {
			stats.ErrorRate = float64(stats.ErrorCount) / float64(stats.CallCount)
		}
		if len(stats.Durations) > 0 {
			sort.Slice(stats.Durations, func(i, j int) bool {
				return stats.Durations[i] < stats.Durations[j]
			})
			stats.P50 = percentile(stats.Durations, 50)
			stats.P95 = percentile(stats.Durations, 95)
			stats.P99 = percentile(stats.Durations, 99)
		}
	}

	a.result = result
	return result, nil
}

func (a *Aggregator) AggregateStream(opts stream.StreamFilterOptions) (*AggregateResult, error) {
	analyzerOpts := analyzer.FilterOptions{
		Service:   opts.Service,
		Operation: opts.Operation,
		TraceID:   opts.TraceID,
		StartTime: opts.StartTime,
		EndTime:   opts.EndTime,
	}
	return a.Aggregate(analyzerOpts)
}

func (a *Aggregator) getSpansFromStore(opts analyzer.FilterOptions) ([]*models.Span, error) {
	storeOpts := storage.FilterOptions{
		Service:   opts.Service,
		Operation: opts.Operation,
		TraceID:   opts.TraceID,
		StartTime: opts.StartTime,
		EndTime:   opts.EndTime,
	}

	traces := a.store.QueryTraces(storeOpts)
	var spans []*models.Span

	for _, trace := range traces {
		for _, span := range trace.Spans {
			if opts.Operation != "" && span.Operation != opts.Operation {
				continue
			}
			if !opts.StartTime.IsZero() && span.StartTime.Before(opts.StartTime) {
				continue
			}
			if !opts.EndTime.IsZero() && span.StartTime.After(opts.EndTime) {
				continue
			}
			spans = append(spans, span)
		}
	}

	return spans, nil
}

func (a *Aggregator) getSpansFromStream(opts analyzer.FilterOptions) ([]*models.Span, error) {
	traceIDs := a.streamIndex.FilterTraceIDs(
		opts.Service,
		opts.Operation,
		opts.StartTime,
		opts.EndTime,
	)

	var spans []*models.Span

	for _, traceID := range traceIDs {
		err := a.streamIndex.WalkTrace(traceID, func(span *models.Span, depth int) error {
			if opts.Operation != "" && span.Operation != opts.Operation {
				return nil
			}
			if !opts.StartTime.IsZero() && span.StartTime.Before(opts.StartTime) {
				return nil
			}
			if !opts.EndTime.IsZero() && span.StartTime.After(opts.EndTime) {
				return nil
			}
			spans = append(spans, span)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	return spans, nil
}

func (a *Aggregator) GetResult() *AggregateResult {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.result
}

func (a *Aggregator) GetSortedOperations() []*OperationStats {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.result == nil {
		return nil
	}

	ops := make([]*OperationStats, 0, len(a.result.Operations))
	for _, op := range a.result.Operations {
		ops = append(ops, op)
	}

	sort.Slice(ops, func(i, j int) bool {
		return ops[i].CallCount > ops[j].CallCount
	})

	return ops
}

func percentile(durations []time.Duration, p int) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	if p <= 0 {
		return durations[0]
	}
	if p >= 100 {
		return durations[len(durations)-1]
	}

	index := int(float64(len(durations)-1) * float64(p) / 100.0)
	return durations[index]
}

func ParseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	return d, nil
}

func GetTimeRangeFromDuration(d time.Duration) (time.Time, time.Time) {
	end := time.Now()
	start := end.Add(-d)
	return start, end
}
