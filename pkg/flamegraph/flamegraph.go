package flamegraph

import (
	"fmt"
	"html/template"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"trace-cli/pkg/analyzer"
	"trace-cli/pkg/models"
)

type FlameNode struct {
	Name      string
	Service   string
	Operation string
	Value     float64
	StartX    float64
	Width     float64
	Depth     int
	Color     string
	Children  []*FlameNode
	Span      *analyzer.SpanInfo
}

type FlameGraph struct {
	Root      *FlameNode
	MaxDepth  int
	TotalTime time.Duration
	TraceID   string
}

func GenerateFromTrace(trace *models.Trace) *FlameGraph {
	rootSpan := trace.RootSpan()
	if rootSpan == nil || len(trace.Spans) == 0 {
		return &FlameGraph{
			Root:      &FlameNode{Name: "empty"},
			TotalTime: trace.Duration(),
			TraceID:   trace.TraceID,
		}
	}

	totalDuration := trace.Duration().Seconds() * 1000
	if totalDuration == 0 {
		totalDuration = 1
	}

	fg := &FlameGraph{
		TotalTime: trace.Duration(),
		TraceID:   trace.TraceID,
	}

	spanChain := buildSpanInfoChain(trace, rootSpan, "", 0)
	fg.Root = buildFlameTree(spanChain, totalDuration, fg)
	fg.calculateLayout()

	return fg
}

func buildSpanInfoChain(trace *models.Trace, span *models.Span, parentID string, depth int) []*analyzer.SpanInfo {
	var chain []*analyzer.SpanInfo

	info := &analyzer.SpanInfo{
		Service:   span.Service,
		Operation: span.Operation,
		Duration:  span.Duration,
		StartTime: span.StartTime,
		EndTime:   span.EndTime,
		SpanID:    span.SpanID,
		ParentID:  parentID,
	}
	chain = append(chain, info)

	children := trace.Children(span.SpanID)
	sort.Slice(children, func(i, j int) bool {
		return children[i].StartTime.Before(children[j].StartTime)
	})

	for _, child := range children {
		childChain := buildSpanInfoChain(trace, child, span.SpanID, depth+1)
		chain = append(chain, childChain...)
	}

	return chain
}

func buildFlameTree(spans []*analyzer.SpanInfo, _ float64, fg *FlameGraph) *FlameNode {
	if len(spans) == 0 {
		return nil
	}

	rootSpan := spans[0]
	root := &FlameNode{
		Name:      fmt.Sprintf("%s::%s", rootSpan.Service, rootSpan.Operation),
		Service:   rootSpan.Service,
		Operation: rootSpan.Operation,
		Value:     rootSpan.Duration.Seconds() * 1000,
		Depth:     0,
		Span:      rootSpan,
	}

	fg.MaxDepth = 0

	spanMap := make(map[string]*FlameNode)
	spanMap[rootSpan.SpanID] = root

	for i := 1; i < len(spans); i++ {
		span := spans[i]
		node := &FlameNode{
			Name:      fmt.Sprintf("%s::%s", span.Service, span.Operation),
			Service:   span.Service,
			Operation: span.Operation,
			Value:     span.Duration.Seconds() * 1000,
			Depth:     calculateDepth(spans, span, i),
			Span:      span,
		}

		if node.Depth > fg.MaxDepth {
			fg.MaxDepth = node.Depth
		}

		parent := spanMap[span.ParentID]
		if parent != nil {
			parent.Children = append(parent.Children, node)
		}

		spanMap[span.SpanID] = node
	}

	return root
}

func calculateDepth(spans []*analyzer.SpanInfo, current *analyzer.SpanInfo, currentIndex int) int {
	depth := 0
	parentID := current.ParentID

	for parentID != "" {
		for i := currentIndex - 1; i >= 0; i-- {
			if spans[i].SpanID == parentID {
				depth++
				parentID = spans[i].ParentID
				break
			}
		}
		if parentID == current.ParentID {
			break
		}
	}

	return depth
}

func (fg *FlameGraph) calculateLayout() {
	if fg.Root == nil {
		return
	}

	totalWidth := 1000.0
	rowHeight := 20.0

	var calculate func(node *FlameNode, startX float64, depth int)
	calculate = func(node *FlameNode, startX float64, depth int) {
		node.StartX = startX
		node.Depth = depth
		node.Width = (node.Value / (fg.TotalTime.Seconds() * 1000)) * totalWidth
		node.Color = getColor(node.Service, depth)

		childStartX := startX
		for _, child := range node.Children {
			calculate(child, childStartX, depth+1)
			childStartX += child.Width
		}
	}

	calculate(fg.Root, 0, 0)
}

func getColor(service string, depth int) string {
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

func (fg *FlameGraph) GenerateSVG(outputPath string) error {
	width := 1200
	height := (fg.MaxDepth + 2) * 25
	rowHeight := 20

	var svgBuilder strings.Builder
	svgBuilder.WriteString(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`,
		width, height, width, height))
	svgBuilder.WriteString(`<style>
		.flame-bar { cursor: pointer; }
		.flame-bar:hover { stroke: #000; stroke-width: 1; }
		.flame-text { font-family: Arial, sans-serif; font-size: 11px; fill: #000; pointer-events: none; }
		.tooltip { font-family: Arial, sans-serif; font-size: 12px; }
	</style>`)

	svgBuilder.WriteString(fmt.Sprintf(`<text x="10" y="20" class="tooltip" font-weight="bold">Trace ID: %s</text>`, fg.TraceID))
	svgBuilder.WriteString(fmt.Sprintf(`<text x="%d" y="20" class="tooltip" text-anchor="end">Total: %s</text>`,
		width-10, fg.TotalTime))

	var drawNode func(node *FlameNode)
	drawNode = func(node *FlameNode) {
		if node.Width < 1 {
			return
		}

		x := node.StartX + 100
		y := float64(height-30) - float64(node.Depth)*float64(rowHeight)
		w := node.Width
		h := float64(rowHeight) - 2

		svgBuilder.WriteString(fmt.Sprintf(`<g class="flame-bar">
			<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" fill="%s" rx="2" ry="2">
				<title>%s&#10;Duration: %s&#10;Service: %s</title>
			</rect>`, x, y, w, h, node.Color,
			template.HTMLEscapeString(node.Name),
			node.Span.Duration,
			node.Service))

		if w > 30 {
			text := node.Name
			if len(text) > int(w/6) {
				text = text[:int(w/6)-3] + "..."
			}
			svgBuilder.WriteString(fmt.Sprintf(`<text x="%.2f" y="%.2f" class="flame-text">%s</text>`,
				x+4, y+h-6, template.HTMLEscapeString(text)))
		}

		svgBuilder.WriteString(`</g>`)

		for _, child := range node.Children {
			drawNode(child)
		}
	}

	drawNode(fg.Root)
	svgBuilder.WriteString(`</svg>`)

	return os.WriteFile(outputPath, []byte(svgBuilder.String()), 0644)
}

const htmlTemplate = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <title>Flame Graph - {{.TraceID}}</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { font-family: 'Segoe UI', Arial, sans-serif; background: #f5f5f5; padding: 20px; }
        .container { max-width: 1400px; margin: 0 auto; }
        h1 { color: #333; margin-bottom: 20px; font-size: 24px; }
        .header { background: white; padding: 20px; border-radius: 8px; margin-bottom: 20px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        .stats { display: flex; gap: 30px; margin-top: 15px; }
        .stat-item { background: #f8f9fa; padding: 10px 15px; border-radius: 6px; }
        .stat-label { font-size: 12px; color: #666; }
        .stat-value { font-size: 18px; font-weight: bold; color: #333; }
        .flame-container { background: white; padding: 20px; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); overflow-x: auto; }
        .flame-bar { cursor: pointer; transition: opacity 0.2s; }
        .flame-bar:hover { stroke: #000; stroke-width: 2; }
        .flame-text { font-family: Arial, sans-serif; font-size: 11px; fill: #000; pointer-events: none; }
        .tooltip {
            position: absolute;
            background: rgba(0,0,0,0.85);
            color: white;
            padding: 10px;
            border-radius: 6px;
            font-size: 12px;
            pointer-events: none;
            opacity: 0;
            transition: opacity 0.2s;
            z-index: 1000;
        }
        .tooltip.show { opacity: 1; }
        .tooltip-label { color: #aaa; font-size: 11px; }
        .tooltip-value { font-size: 13px; font-weight: bold; }
        .controls { margin-top: 20px; display: flex; gap: 10px; }
        button {
            padding: 8px 16px;
            border: none;
            border-radius: 4px;
            background: #3498db;
            color: white;
            cursor: pointer;
            font-size: 14px;
        }
        button:hover { background: #2980b9; }
        .breadcrumb { background: #f8f9fa; padding: 10px; border-radius: 6px; margin-top: 15px; font-size: 13px; }
        .breadcrumb span { color: #3498db; cursor: pointer; }
        .breadcrumb span:hover { text-decoration: underline; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🔥 分布式追踪火焰图</h1>
            <div><strong>Trace ID:</strong> {{.TraceID}}</div>
            <div class="stats">
                <div class="stat-item">
                    <div class="stat-label">总耗时</div>
                    <div class="stat-value">{{.TotalTime}}</div>
                </div>
                <div class="stat-item">
                    <div class="stat-label">调用深度</div>
                    <div class="stat-value">{{.MaxDepth}} 层</div>
                </div>
                <div class="stat-item">
                    <div class="stat-label">Span 数量</div>
                    <div class="stat-value">{{.SpanCount}}</div>
                </div>
                <div class="stat-item">
                    <div class="stat-label">服务数量</div>
                    <div class="stat-value">{{.ServiceCount}}</div>
                </div>
            </div>
            <div class="breadcrumb" id="breadcrumb">
                <span onclick="resetZoom()">🔍 全部</span>
            </div>
        </div>

        <div class="flame-container">
            <div class="tooltip" id="tooltip"></div>
            <svg id="flamegraph" xmlns="http://www.w3.org/2000/svg" width="1200" viewBox="0 0 1200 {{.SVGHeight}}">
                {{.SVGContent}}
            </svg>
        </div>

        <div class="controls">
            <button onclick="resetZoom()">🔄 重置视图</button>
            <button onclick="downloadSVG()">⬇️ 下载 SVG</button>
        </div>
    </div>

    <script>
        let currentRoot = null;
        const tooltip = document.getElementById('tooltip');
        const svg = document.getElementById('flamegraph');

        function showTooltip(e, data) {
            tooltip.innerHTML = '<div class="tooltip-label">调用</div>' +
                '<div class="tooltip-value">' + data.name + '</div>' +
                '<div style="margin-top: 8px;" class="tooltip-label">耗时</div>' +
                '<div class="tooltip-value">' + data.duration + '</div>' +
                '<div style="margin-top: 8px;" class="tooltip-label">服务</div>' +
                '<div class="tooltip-value">' + data.service + '</div>' +
                '<div style="margin-top: 8px;" class="tooltip-label">占比</div>' +
                '<div class="tooltip-value">' + data.percentage + '%</div>';
            tooltip.classList.add('show');
            moveTooltip(e);
        }

        function moveTooltip(e) {
            tooltip.style.left = (e.pageX + 15) + 'px';
            tooltip.style.top = (e.pageY + 15) + 'px';
        }

        function hideTooltip() {
            tooltip.classList.remove('show');
        }

        function zoomTo(nodeId, name) {
            const node = document.getElementById(nodeId);
            if (!node) return;

            const bars = document.querySelectorAll('.flame-bar');
            bars.forEach(bar => {
                if (bar.id !== nodeId && !isDescendant(nodeId, bar.id)) {
                    bar.style.opacity = '0.2';
                } else {
                    bar.style.opacity = '1';
                }
            });

            updateBreadcrumb(name);
        }

        function isDescendant(parentId, childId) {
            return childId.startsWith(parentId + '_');
        }

        function resetZoom() {
            const bars = document.querySelectorAll('.flame-bar');
            bars.forEach(bar => bar.style.opacity = '1');
            document.getElementById('breadcrumb').innerHTML = '<span onclick="resetZoom()">🔍 全部</span>';
        }

        function updateBreadcrumb(name) {
            const bc = document.getElementById('breadcrumb');
            bc.innerHTML += ' → <span onclick="resetZoom()">' + escapeHtml(name) + '</span>';
        }

        function escapeHtml(text) {
            const div = document.createElement('div');
            div.textContent = text;
            return div.innerHTML;
        }

        function downloadSVG() {
            const svgData = new XMLSerializer().serializeToString(svg);
            const blob = new Blob([svgData], {type: 'image/svg+xml'});
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = 'flamegraph_{{.TraceID}}.svg';
            a.click();
            URL.revokeObjectURL(url);
        }

        svg.addEventListener('mousemove', moveTooltip);
        svg.addEventListener('mouseleave', hideTooltip);
    </script>
</body>
</html>
`

type HTMLData struct {
	TraceID      string
	TotalTime    string
	MaxDepth     int
	SpanCount    int
	ServiceCount int
	SVGHeight    int
	SVGContent   template.HTML
}

func (fg *FlameGraph) GenerateHTML(outputPath string) error {
	spanCount := 0
	services := make(map[string]bool)
	var countSpans func(node *FlameNode)
	countSpans = func(node *FlameNode) {
		spanCount++
		services[node.Service] = true
		for _, child := range node.Children {
			countSpans(child)
		}
	}
	if fg.Root != nil {
		countSpans(fg.Root)
	}

	svgHeight := (fg.MaxDepth + 2) * 25
	svgContent := fg.generateSVGContent()

	data := HTMLData{
		TraceID:      fg.TraceID,
		TotalTime:    fg.TotalTime.String(),
		MaxDepth:     fg.MaxDepth,
		SpanCount:    spanCount,
		ServiceCount: len(services),
		SVGHeight:    svgHeight,
		SVGContent:   template.HTML(svgContent),
	}

	tmpl, err := template.New("flamegraph").Parse(htmlTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer file.Close()

	return tmpl.Execute(file, data)
}

func (fg *FlameGraph) generateSVGContent() string {
	if fg.Root == nil {
		return ""
	}

	height := (fg.MaxDepth + 2) * 25
	rowHeight := 20
	var svgBuilder strings.Builder

	svgBuilder.WriteString(fmt.Sprintf(`<text x="10" y="20" font-family="Arial" font-size="14" font-weight="bold">Trace ID: %s</text>`, fg.TraceID))
	svgBuilder.WriteString(fmt.Sprintf(`<text x="1190" y="20" font-family="Arial" font-size="14" text-anchor="end">Total: %s</text>`, fg.TotalTime))

	nodeIdCounter := 0
	var drawNode func(node *FlameNode, parentId string) string
	drawNode = func(node *FlameNode, parentId string) string {
		if node.Width < 1 {
			return ""
		}

		nodeIdCounter++
		nodeId := fmt.Sprintf("node_%d", nodeIdCounter)

		x := node.StartX + 100
		y := float64(height-30) - float64(node.Depth)*float64(rowHeight)
		w := node.Width
		h := float64(rowHeight) - 2

		percentage := (node.Value / (fg.TotalTime.Seconds() * 1000)) * 100

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf(`<g class="flame-bar" id="%s" onclick="zoomTo('%s', '%s')"
			onmouseover="showTooltip(event, {name: '%s', duration: '%s', service: '%s', percentage: '%.2f'})"
			onmouseout="hideTooltip()">
			<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" fill="%s" rx="2" ry="2"/>`,
			nodeId, nodeId, template.HTMLEscapeString(node.Name),
			template.HTMLEscapeString(node.Name), node.Span.Duration, node.Service, percentage,
			x, y, w, h, node.Color))

		if w > 30 {
			text := node.Name
			if len(text) > int(w/6) {
				text = text[:int(w/6)-3] + "..."
			}
			sb.WriteString(fmt.Sprintf(`<text x="%.2f" y="%.2f" class="flame-text">%s</text>`,
				x+4, y+h-6, template.HTMLEscapeString(text)))
		}

		sb.WriteString(`</g>`)

		for _, child := range node.Children {
			childSVG := drawNode(child, nodeId)
			sb.WriteString(childSVG)
		}

		return sb.String()
	}

	svgBuilder.WriteString(drawNode(fg.Root, ""))

	return svgBuilder.String()
}
