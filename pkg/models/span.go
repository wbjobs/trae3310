package models

import (
	"fmt"
	"time"
)

type Span struct {
	TraceID    string
	SpanID     string
	ParentID   string
	Service    string
	Operation  string
	StartTime  time.Time
	EndTime    time.Time
	Duration   time.Duration
	Attributes map[string]string
	Status     SpanStatus
	Kind       SpanKind
	Events     []SpanEvent
	Links      []SpanLink
}

type SpanStatus struct {
	Code        StatusCode
	Description string
}

type StatusCode int

const (
	StatusUnset StatusCode = iota
	StatusOk
	StatusError
)

type SpanKind int

const (
	SpanKindUnspecified SpanKind = iota
	SpanKindInternal
	SpanKindServer
	SpanKindClient
	SpanKindProducer
	SpanKindConsumer
)

type SpanEvent struct {
	Time       time.Time
	Name       string
	Attributes map[string]string
}

type SpanLink struct {
	TraceID string
	SpanID  string
}

type Trace struct {
	TraceID string
	Spans   []*Span
}

func (t *Trace) Duration() time.Duration {
	if len(t.Spans) == 0 {
		return 0
	}
	var minStart, maxEnd time.Time
	for i, s := range t.Spans {
		if i == 0 {
			minStart = s.StartTime
			maxEnd = s.EndTime
			continue
		}
		if s.StartTime.Before(minStart) {
			minStart = s.StartTime
		}
		if s.EndTime.After(maxEnd) {
			maxEnd = s.EndTime
		}
	}
	return maxEnd.Sub(minStart)
}

func (t *Trace) RootSpan() *Span {
	for _, s := range t.Spans {
		if s.ParentID == "" {
			return s
		}
	}
	return nil
}

func (t *Trace) GetSpan(spanID string) *Span {
	for _, s := range t.Spans {
		if s.SpanID == spanID {
			return s
		}
	}
	return nil
}

func (t *Trace) Children(parentID string) []*Span {
	var children []*Span
	for _, s := range t.Spans {
		if s.ParentID == parentID {
			children = append(children, s)
		}
	}
	return children
}

func (s *Span) String() string {
	return fmt.Sprintf("[%s] %s::%s (%s)", s.TraceID, s.Service, s.Operation, s.Duration)
}
