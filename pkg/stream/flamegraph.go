package stream

import (
	"fmt"
	"html/template"
	"math"
	"os"
	"strings"
	"time"

	"trace-cli/pkg/analyzer"
	"trace-cli/pkg/flamegraph"
)

type StreamFlameGenerator struct {
	index *StreamIndex
}

func NewStreamFlameGenerator(index *StreamIndex) *StreamFlameGenerator {
	return &StreamFlameGenerator{index: index}
}

type LightFlameNode struct {
	Name       string
	Service    string
	Operation  string
	ValueMs    float64
	StartX     float64
	Width      float64
	Depth      int
	Color      string
	SpanID     string
	ParentID   string
	StartTime  int64
	EndTime    int64
	DurationMs int64
}

func (sfg *StreamFlameGenerator) GenerateFromTrace(traceID string) (*flamegraph.FlameGraph, error) {
	info, ok := sfg.index.GetTraceInfo(traceID)
	if !ok {
		return nil, fmt.Errorf("trace not found: %s", traceID)
	}

	totalDuration := time.Duration(info.MaxTime - info.MinTime)
	if totalDuration == 0 {
		totalDuration = 1
	}

	totalWidth := 1000.0
	maxDepth := 0

	nodes := make(map[string]*LightFlameNode)
	orderedNodes := make([]*LightFlameNode, 0, info.SpanCount)

	err := sfg.index.WalkTrace(traceID, func(span *analyzer.Span, depth int) error {
		if depth > maxDepth {
			maxDepth = depth
		}

		node := &LightFlameNode{
			Name:       fmt.Sprintf("%s::%s", span.Service, span.Operation),
			Service:    span.Service,
			Operation:  span.Operation,
			ValueMs:    span.Duration.Seconds() * 1000,
			Depth:      depth,
			SpanID:     span.SpanID,
			ParentID:   span.ParentID,
			StartTime:  span.StartTime.UnixNano(),
			EndTime:    span.EndTime.UnixNano(),
			DurationMs: span.Duration.Milliseconds(),
		}
		node.Color = getNodeColor(node.Service, node.Depth)

		nodes[span.SpanID] = node
		orderedNodes = append(orderedNodes, node)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk trace: %w", err)
	}

	sfg.calculateLayout(orderedNodes, totalDuration, totalWidth)

	root := sfg.buildFlameTree(orderedNodes, nodes, maxDepth)

	return &flamegraph.FlameGraph{
		Root:      root,
		MaxDepth:  maxDepth,
		TotalTime: totalDuration,
		TraceID:   traceID,
	}, nil
}

func (sfg *StreamFlameGenerator) calculateLayout(nodes []*LightFlameNode, totalDuration time.Duration, totalWidth float64) {
	if len(nodes) == 0 {
		return
	}

	rootStartTime := nodes[0].StartTime

	positionStack := make(map[int]float64)
	positionStack[0] = 0

	for _, node := range nodes {
		depth := node.Depth

		relativeStart := float64(node.StartTime-rootStartTime) / float64(totalDuration)
		node.StartX = relativeStart * totalWidth

		relativeWidth := float64(node.EndTime-node.StartTime) / float64(totalDuration)
		node.Width = relativeWidth * totalWidth

		positionStack[depth] = node.StartX + node.Width
	}
}

func (sfg *StreamFlameGenerator) buildFlameTree(ordered []*LightFlameNode, nodeMap map[string]*LightFlameNode, maxDepth int) *flamegraph.FlameNode {
	if len(ordered) == 0 {
		return nil
	}

	root := sfg.toFlameNode(ordered[0])
	parentMap := make(map[string]*flamegraph.FlameNode)
	parentMap[ordered[0].SpanID] = root

	for i := 1; i < len(ordered); i++ {
		lightNode := ordered[i]
		fn := sfg.toFlameNode(lightNode)

		parent := parentMap[lightNode.ParentID]
		if parent != nil {
			parent.Children = append(parent.Children, fn)
		}

		parentMap[lightNode.SpanID] = fn
	}

	return root
}

func (sfg *StreamFlameGenerator) toFlameNode(ln *LightFlameNode) *flamegraph.FlameNode {
	return &flamegraph.FlameNode{
		Name:      ln.Name,
		Service:   ln.Service,
		Operation: ln.Operation,
		Value:     ln.ValueMs,
		StartX:    ln.StartX,
		Width:     ln.Width,
		Depth:     ln.Depth,
		Color:     ln.Color,
		Span: &analyzer.SpanInfo{
			Service:   ln.Service,
			Operation: ln.Operation,
			Duration:  time.Duration(ln.DurationMs) * time.Millisecond,
			StartTime: time.Unix(0, ln.StartTime),
			EndTime:   time.Unix(0, ln.EndTime),
			SpanID:    ln.SpanID,
			ParentID:  ln.ParentID,
		},
	}
}

func getNodeColor(service string, depth int) string {
	colors := []string{
		"#e74c3c", "#3498db", "#2ecc71", "#f39c12", "#9b59b6",
		"#1abc9c", "#e67e22", "#34495e", "#16a085", "#d35400",
		"#c0392b", "#2980b9", "#27ae60", "#8e44ad", "#f1c40f",
	}

	hash := 0
	for _, c := range service {
		hash = int(c) + ((hash << 5) - hash)
	}

	baseColor := colors[int(math.Abs(float64(hash)))%len(colors)]
	return adjustBrightness(baseColor, depth*10)
}

func adjustBrightness(hex string, adjustment int) string {
	hex = strings.TrimPrefix(hex, "#")
	r := int(parseHex(hex[0:2]))
	g := int(parseHex(hex[2:4]))
	b := int(parseHex(hex[4:6]))

	r = clamp(r+adjustment, 0, 255)
	g = clamp(g+adjustment, 0, 255)
	b = clamp(b+adjustment, 0, 255)

	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

func parseHex(s string) uint8 {
	var val uint8
	fmt.Sscanf(s, "%x", &val)
	return val
}

func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func (sfg *StreamFlameGenerator) GenerateSVG(traceID, outputPath string) error {
	fg, err := sfg.GenerateFromTrace(traceID)
	if err != nil {
		return err
	}
	return fg.GenerateSVG(outputPath)
}

func (sfg *StreamFlameGenerator) GenerateHTML(traceID, outputPath string) error {
	fg, err := sfg.GenerateFromTrace(traceID)
	if err != nil {
		return err
	}
	return fg.GenerateHTML(outputPath)
}

func init() {
	_ = template.HTMLEscapeString
	_ = os.WriteFile
}
