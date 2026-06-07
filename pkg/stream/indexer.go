package stream

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"trace-cli/pkg/models"
)

const (
	spanHeaderSize = 8 + 8 + 8 + 8
	maxMemorySpans = 10000
)

type SpanRef struct {
	TraceID    string
	SpanID     string
	ParentID   string
	Offset     int64
	Size       int32
	StartTime  int64
	EndTime    int64
	DurationMs int64
	Service    string
	Operation  string
}

type StreamIndex struct {
	mu sync.RWMutex

	traceIndex    map[string]*TraceInfo
	spanIndex     map[string]*SpanRef
	parentIndex   map[string][]string
	traceSpans    map[string][]string
	memoryCache   map[string]*models.Span

	spillThreshold int
	memorySpans    atomic.Int64
	tempFile       *os.File
	tempFilePath   string
	encoder        *binaryEncoder

	closed bool
}

type TraceInfo struct {
	TraceID    string
	RootSpanID string
	MinTime    int64
	MaxTime    int64
	SpanCount  int
	Services   map[string]bool
	HasError   bool
}

func NewStreamIndex(tempDir string, spillThreshold int) (*StreamIndex, error) {
	if spillThreshold <= 0 {
		spillThreshold = maxMemorySpans
	}

	tmpFile, err := os.CreateTemp(tempDir, "trace-spans-*.bin")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}

	return &StreamIndex{
		traceIndex:     make(map[string]*TraceInfo),
		spanIndex:      make(map[string]*SpanRef),
		parentIndex:    make(map[string][]string),
		traceSpans:     make(map[string][]string),
		memoryCache:    make(map[string]*models.Span),
		spillThreshold: spillThreshold,
		tempFile:       tmpFile,
		tempFilePath:   tmpFile.Name(),
		encoder:        newBinaryEncoder(tmpFile),
	}, nil
}

func (si *StreamIndex) AddSpan(span *models.Span) error {
	if si.closed {
		return errors.New("index is closed")
	}

	spanRef := &SpanRef{
		TraceID:    span.TraceID,
		SpanID:     span.SpanID,
		ParentID:   span.ParentID,
		StartTime:  span.StartTime.UnixNano(),
		EndTime:    span.EndTime.UnixNano(),
		DurationMs: span.Duration.Milliseconds(),
		Service:    span.Service,
		Operation:  span.Operation,
	}

	si.mu.Lock()
	defer si.mu.Unlock()

	if _, exists := si.spanIndex[span.SpanID]; exists {
		return nil
	}

	if span.ParentID == "" {
		if traceInfo, ok := si.traceIndex[span.TraceID]; ok {
			traceInfo.RootSpanID = span.SpanID
		}
	}

	if _, ok := si.traceIndex[span.TraceID]; !ok {
		si.traceIndex[span.TraceID] = &TraceInfo{
			TraceID:  span.TraceID,
			Services: make(map[string]bool),
		}
	}

	traceInfo := si.traceIndex[span.TraceID]
	traceInfo.Services[span.Service] = true
	if span.StartTime.UnixNano() < traceInfo.MinTime || traceInfo.MinTime == 0 {
		traceInfo.MinTime = span.StartTime.UnixNano()
	}
	if span.EndTime.UnixNano() > traceInfo.MaxTime {
		traceInfo.MaxTime = span.EndTime.UnixNano()
	}
	traceInfo.SpanCount++
	if span.Status.Code == models.StatusError {
		traceInfo.HasError = true
	}

	if span.ParentID != "" {
		si.parentIndex[span.ParentID] = append(si.parentIndex[span.ParentID], span.SpanID)
	}

	si.traceSpans[span.TraceID] = append(si.traceSpans[span.TraceID], span.SpanID)

	memoryCount := si.memorySpans.Add(1)
	if memoryCount > int64(si.spillThreshold) {
		offset, err := si.encoder.encodeSpan(span)
		if err != nil {
			return fmt.Errorf("failed to spill span to disk: %w", err)
		}
		spanRef.Offset = offset
		spanRef.Size = int32(binary.Size(span))
	} else {
		si.memoryCache[span.SpanID] = span
	}

	si.spanIndex[span.SpanID] = spanRef

	return nil
}

func (si *StreamIndex) GetSpanRef(spanID string) (*SpanRef, bool) {
	si.mu.RLock()
	defer si.mu.RUnlock()
	ref, ok := si.spanIndex[spanID]
	return ref, ok
}

func (si *StreamIndex) GetTraceInfo(traceID string) (*TraceInfo, bool) {
	si.mu.RLock()
	defer si.mu.RUnlock()
	info, ok := si.traceIndex[traceID]
	return info, ok
}

func (si *StreamIndex) GetTraceSpanIDs(traceID string) ([]string, bool) {
	si.mu.RLock()
	defer si.mu.RUnlock()
	spanIDs, ok := si.traceSpans[traceID]
	return spanIDs, ok
}

func (si *StreamIndex) GetChildren(spanID string) []string {
	si.mu.RLock()
	defer si.mu.RUnlock()
	return si.parentIndex[spanID]
}

func (si *StreamIndex) GetAllTraceIDs() []string {
	si.mu.RLock()
	defer si.mu.RUnlock()
	ids := make([]string, 0, len(si.traceIndex))
	for id := range si.traceIndex {
		ids = append(ids, id)
	}
	return ids
}

func (si *StreamIndex) LoadSpan(spanID string) (*models.Span, error) {
	si.mu.RLock()
	ref, ok := si.spanIndex[spanID]
	if !ok {
		si.mu.RUnlock()
		return nil, fmt.Errorf("span not found: %s", spanID)
	}

	if span, cached := si.memoryCache[spanID]; cached {
		si.mu.RUnlock()
		return span, nil
	}
	si.mu.RUnlock()

	if ref.Offset == 0 {
		return nil, fmt.Errorf("span data not available: %s", spanID)
	}

	si.encoder.mu.Lock()
	defer si.encoder.mu.Unlock()

	span, err := si.encoder.decodeSpan(ref.Offset)
	if err != nil {
		return nil, fmt.Errorf("failed to load span from disk: %w", err)
	}

	return span, nil
}

func (si *StreamIndex) WalkTrace(traceID string, fn func(span *models.Span, depth int) error) error {
	traceInfo, ok := si.GetTraceInfo(traceID)
	if !ok {
		return fmt.Errorf("trace not found: %s", traceID)
	}

	if traceInfo.RootSpanID == "" {
		return errors.New("trace has no root span")
	}

	var walk func(spanID string, depth int) error
	walk = func(spanID string, depth int) error {
		span, err := si.LoadSpan(spanID)
		if err != nil {
			return err
		}

		if err := fn(span, depth); err != nil {
			return err
		}

		children := si.GetChildren(spanID)
		for _, childID := range children {
			if err := walk(childID, depth+1); err != nil {
				return err
			}
		}
		return nil
	}

	return walk(traceInfo.RootSpanID, 0)
}

func (si *StreamIndex) FilterTraceIDs(service, operation string, startTime, endTime time.Time) []string {
	si.mu.RLock()
	defer si.mu.RUnlock()

	var result []string
	startNano := startTime.UnixNano()
	endNano := endTime.UnixNano()

	for traceID, info := range si.traceIndex {
		if service != "" && !info.Services[service] {
			continue
		}
		if !startTime.IsZero() && info.MaxTime < startNano {
			continue
		}
		if !endTime.IsZero() && info.MinTime > endNano {
			continue
		}
		result = append(result, traceID)
	}

	return result
}

func (si *StreamIndex) MemoryUsage() int64 {
	return si.memorySpans.Load()
}

func (si *StreamIndex) TraceCount() int64 {
	si.mu.RLock()
	defer si.mu.RUnlock()
	return int64(len(si.traceIndex))
}

func (si *StreamIndex) Close() error {
	si.mu.Lock()
	defer si.mu.Unlock()

	if si.closed {
		return nil
	}

	si.closed = true

	for k := range si.memoryCache {
		delete(si.memoryCache, k)
	}
	si.memoryCache = nil

	for k := range si.traceIndex {
		delete(si.traceIndex, k)
	}
	si.traceIndex = nil

	for k := range si.spanIndex {
		delete(si.spanIndex, k)
	}
	si.spanIndex = nil

	for k := range si.parentIndex {
		delete(si.parentIndex, k)
	}
	si.parentIndex = nil

	for k := range si.traceSpans {
		delete(si.traceSpans, k)
	}
	si.traceSpans = nil

	if si.tempFile != nil {
		si.tempFile.Close()
		os.Remove(si.tempFilePath)
	}

	return nil
}

type binaryEncoder struct {
	mu       sync.Mutex
	file     *os.File
	scratch  []byte
}

func newBinaryEncoder(file *os.File) *binaryEncoder {
	return &binaryEncoder{
		file:    file,
		scratch: make([]byte, 1024*1024),
	}
}

func (be *binaryEncoder) encodeSpan(span *models.Span) (int64, error) {
	offset, err := be.file.Seek(0, 2)
	if err != nil {
		return 0, err
	}

	buf := be.scratch[:0]

	buf = appendString(buf, span.TraceID)
	buf = appendString(buf, span.SpanID)
	buf = appendString(buf, span.ParentID)
	buf = appendString(buf, span.Service)
	buf = appendString(buf, span.Operation)

	timeBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(timeBuf, uint64(span.StartTime.UnixNano()))
	buf = append(buf, timeBuf...)
	binary.LittleEndian.PutUint64(timeBuf, uint64(span.EndTime.UnixNano()))
	buf = append(buf, timeBuf...)

	statusBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(statusBuf, uint32(span.Status.Code))
	buf = append(buf, statusBuf...)
	buf = appendString(buf, span.Status.Description)

	kindBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(kindBuf, uint32(span.Kind))
	buf = append(buf, kindBuf...)

	attrsLen := make([]byte, 4)
	binary.LittleEndian.PutUint32(attrsLen, uint32(len(span.Attributes)))
	buf = append(buf, attrsLen...)

	for k, v := range span.Attributes {
		buf = appendString(buf, k)
		buf = appendString(buf, v)
	}

	_, err = be.file.Write(buf)
	if err != nil {
		return 0, err
	}

	return offset, nil
}

func (be *binaryEncoder) decodeSpan(offset int64) (*models.Span, error) {
	_, err := be.file.Seek(offset, 0)
	if err != nil {
		return nil, err
	}

	span := &models.Span{
		Attributes: make(map[string]string),
	}

	readBuf := make([]byte, 4)

	var readErr error
	readFull := func(n int) []byte {
		if readErr != nil {
			return nil
		}
		buf := make([]byte, n)
		_, readErr = be.file.Read(buf)
		return buf
	}

	span.TraceID = readString(readBuf, be.file, &readErr)
	span.SpanID = readString(readBuf, be.file, &readErr)
	span.ParentID = readString(readBuf, be.file, &readErr)
	span.Service = readString(readBuf, be.file, &readErr)
	span.Operation = readString(readBuf, be.file, &readErr)

	timeBytes := readFull(16)
	if readErr != nil {
		return nil, readErr
	}
	span.StartTime = time.Unix(0, int64(binary.LittleEndian.Uint64(timeBytes[:8])))
	span.EndTime = time.Unix(0, int64(binary.LittleEndian.Uint64(timeBytes[8:16])))
	span.Duration = span.EndTime.Sub(span.StartTime)

	statusBytes := readFull(4)
	if readErr != nil {
		return nil, readErr
	}
	span.Status.Code = models.StatusCode(binary.LittleEndian.Uint32(statusBytes))
	span.Status.Description = readString(readBuf, be.file, &readErr)

	kindBytes := readFull(4)
	if readErr != nil {
		return nil, readErr
	}
	span.Kind = models.SpanKind(binary.LittleEndian.Uint32(kindBytes))

	attrsLenBytes := readFull(4)
	if readErr != nil {
		return nil, readErr
	}
	attrsLen := binary.LittleEndian.Uint32(attrsLenBytes)

	for i := uint32(0); i < attrsLen; i++ {
		k := readString(readBuf, be.file, &readErr)
		v := readString(readBuf, be.file, &readErr)
		if readErr != nil {
			return nil, readErr
		}
		span.Attributes[k] = v
	}

	if readErr != nil {
		return nil, readErr
	}

	return span, nil
}

func appendString(buf []byte, s string) []byte {
	lenBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(lenBuf, uint32(len(s)))
	buf = append(buf, lenBuf...)
	buf = append(buf, []byte(s)...)
	return buf
}

func readString(preBuf []byte, f *os.File, readErr *error) string {
	if *readErr != nil {
		return ""
	}
	_, *readErr = f.Read(preBuf)
	if *readErr != nil {
		return ""
	}
	strLen := binary.LittleEndian.Uint32(preBuf)
	if strLen == 0 {
		return ""
	}
	strBuf := make([]byte, strLen)
	_, *readErr = f.Read(strBuf)
	if *readErr != nil {
		return ""
	}
	return string(strBuf)
}

func init() {
	_ = filepath.Join
}
