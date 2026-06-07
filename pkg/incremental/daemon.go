package incremental

import (
	"context"
	"fmt"
	"time"

	"trace-cli/pkg/aggregator"
	"trace-cli/pkg/analyzer"
	"trace-cli/pkg/metrics"
	"trace-cli/pkg/stream"
)

type Daemon struct {
	aggregator     *aggregator.Aggregator
	metricsExporter *metrics.MetricsExporter
	metricsAddr    string
	interval       time.Duration
	timeWindow     time.Duration
	opts           analyzer.FilterOptions
	streamOpts     stream.StreamFilterOptions
	useStreaming   bool
	running        bool
	lastUpdate     time.Time
	updateCount    int64
}

type DaemonConfig struct {
	Interval       time.Duration
	TimeWindow     time.Duration
	Service        string
	Operation      string
	MetricsAddr    string
	MetricsNamespace string
	UseStreaming   bool
}

func NewDaemon(cfg DaemonConfig, agg *aggregator.Aggregator, useStreaming bool) (*Daemon, error) {
	if cfg.Interval == 0 {
		cfg.Interval = 10 * time.Second
	}
	if cfg.TimeWindow == 0 {
		cfg.TimeWindow = time.Hour
	}

	d := &Daemon{
		aggregator:   agg,
		interval:     cfg.Interval,
		timeWindow:   cfg.TimeWindow,
		metricsAddr:  cfg.MetricsAddr,
		useStreaming: useStreaming,
	}

	if useStreaming {
		d.streamOpts = stream.StreamFilterOptions{
			Service:   cfg.Service,
			Operation: cfg.Operation,
		}
	} else {
		d.opts = analyzer.FilterOptions{
			Service:   cfg.Service,
			Operation: cfg.Operation,
		}
	}

	if cfg.MetricsAddr != "" {
		d.metricsExporter = metrics.NewMetricsExporter(agg, cfg.MetricsNamespace)
	}

	return d, nil
}

func (d *Daemon) Start(ctx context.Context) error {
	if d.running {
		return fmt.Errorf("daemon is already running")
	}

	d.running = true
	fmt.Printf("🚀 启动增量分析守护进程\n")
	fmt.Printf("   时间窗口: %v\n", d.timeWindow)
	fmt.Printf("   更新间隔: %v\n", d.interval)
	fmt.Printf("   流式模式: %v\n", d.useStreaming)
	if d.metricsExporter != nil {
		fmt.Printf("   Metrics端点: http://%s/metrics\n", d.metricsAddr)
		server, err := d.metricsExporter.StartHTTPServer(d.metricsAddr)
		if err != nil {
			return fmt.Errorf("failed to start metrics server: %w", err)
		}
		go func() {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			server.Shutdown(shutdownCtx)
		}()
	}

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	if err := d.update(); err != nil {
		fmt.Printf("⚠️  首次更新失败: %v\n", err)
	}

	for {
		select {
		case <-ctx.Done():
			d.running = false
			fmt.Printf("\n🛑 守护进程已停止，共执行 %d 次更新\n", d.updateCount)
			return nil
		case <-ticker.C:
			if err := d.update(); err != nil {
				fmt.Printf("⚠️  更新失败 [%s]: %v\n", time.Now().Format(time.RFC3339), err)
			}
		}
	}
}

func (d *Daemon) update() error {
	start := time.Now()
	d.updateCount++

	windowStart, windowEnd := aggregator.GetTimeRangeFromDuration(d.timeWindow)

	var result *aggregator.AggregateResult
	var err error

	if d.useStreaming {
		d.streamOpts.StartTime = windowStart
		d.streamOpts.EndTime = windowEnd
		result, err = d.aggregator.AggregateStream(d.streamOpts)
	} else {
		d.opts.StartTime = windowStart
		d.opts.EndTime = windowEnd
		result, err = d.aggregator.Aggregate(d.opts)
	}

	if err != nil {
		return err
	}

	d.lastUpdate = time.Now()
	duration := d.lastUpdate.Sub(start)

	fmt.Printf("[%s] ✅ 更新完成: %d traces, %d spans, %d operations (耗时 %v)\n",
		d.lastUpdate.Format(time.RFC3339),
		result.TotalTraces,
		result.TotalSpans,
		len(result.Operations),
		duration)

	if d.metricsExporter != nil {
		d.metricsExporter.GenerateMetrics()
	}

	return nil
}

func (d *Daemon) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"running":      d.running,
		"interval":     d.interval,
		"timeWindow":   d.timeWindow,
		"lastUpdate":   d.lastUpdate,
		"updateCount":  d.updateCount,
		"useStreaming": d.useStreaming,
	}
}

func (d *Daemon) GetAggregator() *aggregator.Aggregator {
	return d.aggregator
}

func (d *Daemon) GetMetricsExporter() *metrics.MetricsExporter {
	return d.metricsExporter
}
