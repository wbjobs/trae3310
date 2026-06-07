package mock

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"trace-cli/pkg/models"
	"trace-cli/pkg/storage"
)

type TraceGenerator struct {
	services []string
}

func NewGenerator() *TraceGenerator {
	return &TraceGenerator{
		services: []string{
			"api-gateway", "user-service", "order-service",
			"payment-service", "inventory-service",
			"notification-service", "auth-service",
		},
	}
}

func (g *TraceGenerator) GenerateTrace(numTraces int, store *storage.TraceStore) {
	for i := 0; i < numTraces; i++ {
		trace := g.generateSingleTrace(i)
		for _, span := range trace.Spans {
			store.AddSpan(span)
		}
	}
}

func (g *TraceGenerator) generateSingleTrace(idx int) *models.Trace {
	traceID := generateID()
	baseTime := time.Now().Add(-time.Duration(idx*5) * time.Second)

	trace := &models.Trace{
		TraceID: traceID,
		Spans:   make([]*models.Span, 0),
	}

	rootSpanID := generateID()
	rootDuration := time.Duration(50+idx*30) * time.Millisecond
	if idx%3 == 0 {
		rootDuration = time.Duration(150+idx*50) * time.Millisecond
	}

	rootSpan := &models.Span{
		TraceID:   traceID,
		SpanID:    rootSpanID,
		ParentID:  "",
		Service:   g.services[0],
		Operation: "handle_request",
		StartTime: baseTime,
		EndTime:   baseTime.Add(rootDuration),
		Duration:  rootDuration,
		Attributes: map[string]string{
			"http.method": "GET",
			"http.url":   fmt.Sprintf("/api/v1/users/%d", idx),
		},
		Status: models.SpanStatus{Code: models.StatusOk},
		Kind:   models.SpanKindServer,
	}
	trace.Spans = append(trace.Spans, rootSpan)

	g.generateChildSpans(trace, rootSpan, 1, baseTime, 3)

	return trace
}

func (g *TraceGenerator) generateChildSpans(trace *models.Trace, parent *models.Span, depth int, startTime time.Time, maxDepth int) {
	if depth > maxDepth {
		return
	}

	numChildren := 1 + depth
	if depth == maxDepth {
		numChildren = 2
	}

	for i := 0; i < numChildren; i++ {
		serviceIdx := (depth + i) % len(g.services)
		if serviceIdx == 0 {
			serviceIdx = 1
		}

		childSpanID := generateID()
		childDuration := time.Duration(10+depth*15+i*8) * time.Millisecond
		if depth == maxDepth && i == 0 {
			childDuration = time.Duration(80+depth*30) * time.Millisecond
		}

		childStartTime := startTime.Add(time.Duration(i*5) * time.Millisecond)

		operations := []string{
			"validate_token", "get_user", "create_order",
			"process_payment", "check_inventory",
			"send_notification", "query_database",
			"call_external_api",
		}
		opIdx := (depth*numChildren + i) % len(operations)

		childSpan := &models.Span{
			TraceID:   trace.TraceID,
			SpanID:    childSpanID,
			ParentID:  parent.SpanID,
			Service:   g.services[serviceIdx],
			Operation: operations[opIdx],
			StartTime: childStartTime,
			EndTime:   childStartTime.Add(childDuration),
			Duration:  childDuration,
			Attributes: map[string]string{
				"db.statement": "SELECT * FROM users WHERE id = ?",
				"peer.service": g.services[serviceIdx],
			},
			Status: models.SpanStatus{Code: models.StatusOk},
			Kind:   models.SpanKindClient,
		}
		trace.Spans = append(trace.Spans, childSpan)

		g.generateChildSpans(trace, childSpan, depth+1, childStartTime, maxDepth)
	}
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func GenerateSampleData(store *storage.TraceStore, count int) {
	gen := NewGenerator()
	gen.GenerateTrace(count, store)
}
