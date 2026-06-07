package metrics

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"trace-cli/pkg/aggregator"
)

type MetricsExporter struct {
	aggregator *aggregator.Aggregator
	namespace  string
	mu         struct {
		sync.RWMutex
		metrics string
	}
}

func NewMetricsExporter(agg *aggregator.Aggregator, namespace string) *MetricsExporter {
	if namespace == "" {
		namespace = "trace_cli"
	}
	return &MetricsExporter{
		aggregator: agg,
		namespace:  namespace,
	}
}

func (e *MetricsExporter) GenerateMetrics() string {
	result := e.aggregator.GetResult()
	if result == nil {
		return ""
	}

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# HELP %s_operation_calls_total Total number of operation calls\n", e.namespace))
	sb.WriteString(fmt.Sprintf("# TYPE %s_operation_calls_total counter\n", e.namespace))
	for _, op := range e.aggregator.GetSortedOperations() {
		sb.WriteString(fmt.Sprintf("%s_operation_calls_total{service=\"%s\",operation=\"%s\"} %d\n",
			e.namespace, op.Service, op.Operation, op.CallCount))
	}

	sb.WriteString(fmt.Sprintf("# HELP %s_operation_errors_total Total number of operation errors\n", e.namespace))
	sb.WriteString(fmt.Sprintf("# TYPE %s_operation_errors_total counter\n", e.namespace))
	for _, op := range e.aggregator.GetSortedOperations() {
		sb.WriteString(fmt.Sprintf("%s_operation_errors_total{service=\"%s\",operation=\"%s\"} %d\n",
			e.namespace, op.Service, op.Operation, op.ErrorCount))
	}

	sb.WriteString(fmt.Sprintf("# HELP %s_operation_error_rate Operation error rate (0-1)\n", e.namespace))
	sb.WriteString(fmt.Sprintf("# TYPE %s_operation_error_rate gauge\n", e.namespace))
	for _, op := range e.aggregator.GetSortedOperations() {
		sb.WriteString(fmt.Sprintf("%s_operation_error_rate{service=\"%s\",operation=\"%s\"} %.4f\n",
			e.namespace, op.Service, op.Operation, op.ErrorRate))
	}

	sb.WriteString(fmt.Sprintf("# HELP %s_operation_duration_seconds Operation duration in seconds\n", e.namespace))
	sb.WriteString(fmt.Sprintf("# TYPE %s_operation_duration_seconds summary\n", e.namespace))
	for _, op := range e.aggregator.GetSortedOperations() {
		sb.WriteString(fmt.Sprintf("%s_operation_duration_seconds{service=\"%s\",operation=\"%s\",quantile=\"0.5\"} %.6f\n",
			e.namespace, op.Service, op.Operation, op.P50.Seconds()))
		sb.WriteString(fmt.Sprintf("%s_operation_duration_seconds{service=\"%s\",operation=\"%s\",quantile=\"0.95\"} %.6f\n",
			e.namespace, op.Service, op.Operation, op.P95.Seconds()))
		sb.WriteString(fmt.Sprintf("%s_operation_duration_seconds{service=\"%s\",operation=\"%s\",quantile=\"0.99\"} %.6f\n",
			e.namespace, op.Service, op.Operation, op.P99.Seconds()))
		sb.WriteString(fmt.Sprintf("%s_operation_duration_seconds_sum{service=\"%s\",operation=\"%s\"} %.6f\n",
			e.namespace, op.Service, op.Operation, op.TotalDuration.Seconds()))
		sb.WriteString(fmt.Sprintf("%s_operation_duration_seconds_count{service=\"%s\",operation=\"%s\"} %d\n",
			e.namespace, op.Service, op.Operation, op.CallCount))
	}

	sb.WriteString(fmt.Sprintf("# HELP %s_operation_duration_min_seconds Minimum operation duration in seconds\n", e.namespace))
	sb.WriteString(fmt.Sprintf("# TYPE %s_operation_duration_min_seconds gauge\n", e.namespace))
	for _, op := range e.aggregator.GetSortedOperations() {
		sb.WriteString(fmt.Sprintf("%s_operation_duration_min_seconds{service=\"%s\",operation=\"%s\"} %.6f\n",
			e.namespace, op.Service, op.Operation, op.MinDuration.Seconds()))
	}

	sb.WriteString(fmt.Sprintf("# HELP %s_operation_duration_max_seconds Maximum operation duration in seconds\n", e.namespace))
	sb.WriteString(fmt.Sprintf("# TYPE %s_operation_duration_max_seconds gauge\n", e.namespace))
	for _, op := range e.aggregator.GetSortedOperations() {
		sb.WriteString(fmt.Sprintf("%s_operation_duration_max_seconds{service=\"%s\",operation=\"%s\"} %.6f\n",
			e.namespace, op.Service, op.Operation, op.MaxDuration.Seconds()))
	}

	sb.WriteString(fmt.Sprintf("# HELP %s_total_spans Total number of spans processed\n", e.namespace))
	sb.WriteString(fmt.Sprintf("# TYPE %s_total_spans gauge\n", e.namespace))
	sb.WriteString(fmt.Sprintf("%s_total_spans %d\n", e.namespace, result.TotalSpans))

	sb.WriteString(fmt.Sprintf("# HELP %s_total_traces Total number of traces processed\n", e.namespace))
	sb.WriteString(fmt.Sprintf("# TYPE %s_total_traces gauge\n", e.namespace))
	sb.WriteString(fmt.Sprintf("%s_total_traces %d\n", e.namespace, result.TotalTraces))

	sb.WriteString(fmt.Sprintf("# HELP %s_last_update_timestamp Unix timestamp of last metrics update\n", e.namespace))
	sb.WriteString(fmt.Sprintf("# TYPE %s_last_update_timestamp gauge\n", e.namespace))
	sb.WriteString(fmt.Sprintf("%s_last_update_timestamp %d\n", e.namespace, result.UpdatedAt.Unix()))

	e.mu.Lock()
	e.mu.metrics = sb.String()
	e.mu.Unlock()

	return sb.String()
}

func (e *MetricsExporter) GetMetrics() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.mu.metrics
}

func (e *MetricsExporter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.GenerateMetrics()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(e.GetMetrics()))
}

func (e *MetricsExporter) StartHTTPServer(addr string) (*http.Server, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", e.ServeHTTP)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("Metrics server error: %v\n", err)
		}
	}()

	return server, nil
}

func (e *MetricsExporter) SaveToFile(filepath string) error {
	metrics := e.GenerateMetrics()
	return os.WriteFile(filepath, []byte(metrics), 0644)
}

func FormatPrometheusDuration(d time.Duration) string {
	return fmt.Sprintf("%.6f", d.Seconds())
}

func EscapeLabelValue(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}
