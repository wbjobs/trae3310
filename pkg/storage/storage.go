package storage

import (
	"sync"
	"time"

	"trace-cli/pkg/models"
)

type TraceStore struct {
	mu     sync.RWMutex
	traces map[string]*models.Trace
}

func NewTraceStore() *TraceStore {
	return &TraceStore{
		traces: make(map[string]*models.Trace),
	}
}

func (s *TraceStore) AddSpan(span *models.Span) {
	s.mu.Lock()
	defer s.mu.Unlock()

	trace, exists := s.traces[span.TraceID]
	if !exists {
		trace = &models.Trace{
			TraceID: span.TraceID,
			Spans:   make([]*models.Span, 0),
		}
		s.traces[span.TraceID] = trace
	}
	trace.Spans = append(trace.Spans, span)
}

func (s *TraceStore) AddSpans(spans []*models.Span) {
	for _, span := range spans {
		s.AddSpan(span)
	}
}

func (s *TraceStore) GetTrace(traceID string) *models.Trace {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if trace, exists := s.traces[traceID]; exists {
		return trace
	}
	return nil
}

func (s *TraceStore) GetAllTraces() []*models.Trace {
	s.mu.RLock()
	defer s.mu.RUnlock()

	traces := make([]*models.Trace, 0, len(s.traces))
	for _, trace := range s.traces {
		traces = append(traces, trace)
	}
	return traces
}

func (s *TraceStore) GetAllSpans() []*models.Span {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var spans []*models.Span
	for _, trace := range s.traces {
		spans = append(spans, trace.Spans...)
	}
	return spans
}

type FilterOptions struct {
	Service    string
	Operation  string
	TraceID    string
	StartTime  time.Time
	EndTime    time.Time
	MinDuration time.Duration
	MaxDuration time.Duration
}

func (s *TraceStore) QueryTraces(opts FilterOptions) []*models.Trace {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*models.Trace

	for _, trace := range s.traces {
		if opts.TraceID != "" && trace.TraceID != opts.TraceID {
			continue
		}

		matchedSpans := s.filterSpans(trace.Spans, opts)
		if len(matchedSpans) == 0 {
			continue
		}

		filteredTrace := &models.Trace{
			TraceID: trace.TraceID,
			Spans:   matchedSpans,
		}
		result = append(result, filteredTrace)
	}

	return result
}

func (s *TraceStore) QuerySpans(opts FilterOptions) []*models.Span {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var allSpans []*models.Span
	for _, trace := range s.traces {
		allSpans = append(allSpans, trace.Spans...)
	}

	return s.filterSpans(allSpans, opts)
}

func (s *TraceStore) filterSpans(spans []*models.Span, opts FilterOptions) []*models.Span {
	var result []*models.Span

	for _, span := range spans {
		if opts.Service != "" && span.Service != opts.Service {
			continue
		}
		if opts.Operation != "" && span.Operation != opts.Operation {
			continue
		}
		if !opts.StartTime.IsZero() && span.StartTime.Before(opts.StartTime) {
			continue
		}
		if !opts.EndTime.IsZero() && span.EndTime.After(opts.EndTime) {
			continue
		}
		if opts.MinDuration > 0 && span.Duration < opts.MinDuration {
			continue
		}
		if opts.MaxDuration > 0 && span.Duration > opts.MaxDuration {
			continue
		}
		result = append(result, span)
	}

	return result
}

func (s *TraceStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.traces = make(map[string]*models.Trace)
}

func (s *TraceStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.traces)
}
