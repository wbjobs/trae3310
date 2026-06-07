package grpcclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"trace-cli/pkg/config"
	"trace-cli/pkg/models"
	"trace-cli/pkg/storage"

	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

type Client struct {
	conn     *grpc.ClientConn
	server   *grpc.Server
	cfg      *config.Config
	traceCl  collectortrace.TraceServiceClient
	store    *storage.TraceStore
	listener net.Listener
}

func NewClient(cfg *config.Config, store *storage.TraceStore) (*Client, error) {
	opts, err := buildDialOptions(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build dial options: %w", err)
	}

	conn, err := grpc.NewClient(cfg.Collector.Address, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to collector: %w", err)
	}

	return &Client{
		conn:    conn,
		cfg:     cfg,
		traceCl: collectortrace.NewTraceServiceClient(conn),
		store:   store,
	}, nil
}

func (c *Client) StartReceiver(ctx context.Context, listenAddr string) error {
	var err error
	c.listener, err = net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", listenAddr, err)
	}

	serverOpts, err := buildServerOptions(c.cfg)
	if err != nil {
		return fmt.Errorf("failed to build server options: %w", err)
	}

	c.server = grpc.NewServer(serverOpts...)
	collectortrace.RegisterTraceServiceServer(c.server, &traceReceiverServer{store: c.store})

	go func() {
		<-ctx.Done()
		c.server.GracefulStop()
	}()

	if err := c.server.Serve(c.listener); err != nil && err != grpc.ErrServerStopped {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

type traceReceiverServer struct {
	collectortrace.UnimplementedTraceServiceServer
	store *storage.TraceStore
}

func (s *traceReceiverServer) Export(ctx context.Context, req *collectortrace.ExportTraceServiceRequest) (*collectortrace.ExportTraceServiceResponse, error) {
	spans := protoToSpans(req)
	s.store.AddSpans(spans)
	return &collectortrace.ExportTraceServiceResponse{}, nil
}

func protoToSpans(req *collectortrace.ExportTraceServiceRequest) []*models.Span {
	var spans []*models.Span

	for _, rs := range req.ResourceSpans {
		serviceName := getServiceName(rs.Resource)

		for _, ss := range rs.ScopeSpans {
			for _, span := range ss.Spans {
				modelSpan := protoSpanToModel(span, serviceName)
				spans = append(spans, modelSpan)
			}
		}
	}

	return spans
}

func getServiceName(resource *resourcepb.Resource) string {
	if resource == nil {
		return "unknown"
	}
	for _, attr := range resource.Attributes {
		if attr.Key == "service.name" {
			if strVal := attr.Value.GetStringValue(); strVal != "" {
				return strVal
			}
		}
	}
	return "unknown"
}

func protoSpanToModel(span *tracepb.Span, serviceName string) *models.Span {
	startTime := time.Unix(0, int64(span.StartTimeUnixNano))
	endTime := time.Unix(0, int64(span.EndTimeUnixNano))

	return &models.Span{
		TraceID:    string(span.TraceId),
		SpanID:     string(span.SpanId),
		ParentID:   string(span.ParentSpanId),
		Service:    serviceName,
		Operation:  span.Name,
		StartTime:  startTime,
		EndTime:    endTime,
		Duration:   endTime.Sub(startTime),
		Attributes: protoAttributesToMap(span.Attributes),
		Status: models.SpanStatus{
			Code:        models.StatusCode(span.Status.Code),
			Description: span.Status.Message,
		},
		Kind:   models.SpanKind(span.Kind),
		Events: protoEventsToModel(span.Events),
		Links:  protoLinksToModel(span.Links),
	}
}

func protoAttributesToMap(attrs []*common.KeyValue) map[string]string {
	result := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		result[attr.Key] = attrValueToString(attr.Value)
	}
	return result
}

func attrValueToString(v *common.AnyValue) string {
	if v == nil {
		return ""
	}
	switch val := v.Value.(type) {
	case *common.AnyValue_StringValue:
		return val.StringValue
	case *common.AnyValue_BoolValue:
		return fmt.Sprintf("%t", val.BoolValue)
	case *common.AnyValue_IntValue:
		return fmt.Sprintf("%d", val.IntValue)
	case *common.AnyValue_DoubleValue:
		return fmt.Sprintf("%f", val.DoubleValue)
	default:
		return ""
	}
}

func protoEventsToModel(events []*tracepb.Span_Event) []models.SpanEvent {
	result := make([]models.SpanEvent, len(events))
	for i, event := range events {
		result[i] = models.SpanEvent{
			Time:       time.Unix(0, int64(event.TimeUnixNano)),
			Name:       event.Name,
			Attributes: protoAttributesToMap(event.Attributes),
		}
	}
	return result
}

func protoLinksToModel(links []*tracepb.Span_Link) []models.SpanLink {
	result := make([]models.SpanLink, len(links))
	for i, link := range links {
		result[i] = models.SpanLink{
			TraceID: string(link.TraceId),
			SpanID:  string(link.SpanId),
		}
	}
	return result
}

func buildDialOptions(cfg *config.Config) ([]grpc.DialOption, error) {
	var opts []grpc.DialOption

	if cfg.Collector.Insecure {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		tlsConfig, err := buildTLSConfig(cfg.Auth)
		if err != nil {
			return nil, err
		}
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	}

	if cfg.Auth.Type != "" {
		opts = append(opts, grpc.WithUnaryInterceptor(authInterceptor(cfg.Auth)))
	}

	return opts, nil
}

func buildServerOptions(cfg *config.Config) ([]grpc.ServerOption, error) {
	var opts []grpc.ServerOption

	if !cfg.Collector.Insecure {
		tlsConfig, err := buildTLSConfig(cfg.Auth)
		if err != nil {
			return nil, err
		}
		opts = append(opts, grpc.Creds(credentials.NewTLS(tlsConfig)))
	}

	return opts, nil
}

func buildTLSConfig(auth config.AuthConfig) (*tls.Config, error) {
	tlsConfig := &tls.Config{}

	if auth.TLSCert != "" && auth.TLSKey != "" {
		cert, err := tls.LoadX509KeyPair(auth.TLSCert, auth.TLSKey)
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS cert/key: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	if auth.TLSCA != "" {
		caCert, err := os.ReadFile(auth.TLSCA)
		if err != nil {
			return nil, fmt.Errorf("failed to read TLS CA: %w", err)
		}
		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsConfig.RootCAs = caPool
	}

	return tlsConfig, nil
}

func authInterceptor(auth config.AuthConfig) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {

		ctx = addAuthMetadata(ctx, auth)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func addAuthMetadata(ctx context.Context, auth config.AuthConfig) context.Context {
	md := metadata.MD{}

	switch auth.Type {
	case "apikey":
		md.Set("x-api-key", auth.APIKey)
	case "bearer":
		md.Set("authorization", "Bearer "+auth.Token)
	case "basic":
		md.Set("authorization", "Basic "+basicAuth(auth.Username, auth.Password))
	}

	return metadata.NewOutgoingContext(ctx, md)
}

func basicAuth(username, password string) string {
	return fmt.Sprintf("%s:%s", username, password)
}

func (c *Client) Close() error {
	if c.server != nil {
		c.server.Stop()
	}
	if c.listener != nil {
		c.listener.Close()
	}
	return c.conn.Close()
}

func (c *Client) Export(ctx context.Context, traces []*models.Trace) error {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.Collector.Timeout)*time.Second)
	defer cancel()

	request := tracesToRequest(traces)
	_, err := c.traceCl.Export(ctx, request)
	if err != nil {
		return fmt.Errorf("failed to export traces: %w", err)
	}
	return nil
}

func tracesToRequest(traces []*models.Trace) *collectortrace.ExportTraceServiceRequest {
	resourceSpans := make([]*tracepb.ResourceSpans, 0, len(traces))

	for _, trace := range traces {
		rs := &tracepb.ResourceSpans{
			ScopeSpans: []*tracepb.ScopeSpans{{
				Spans: make([]*tracepb.Span, 0, len(trace.Spans)),
			}},
		}

		for _, span := range trace.Spans {
			rs.ScopeSpans[0].Spans = append(rs.ScopeSpans[0].Spans, spanToProto(span))
		}

		resourceSpans = append(resourceSpans, rs)
	}

	return &collectortrace.ExportTraceServiceRequest{
		ResourceSpans: resourceSpans,
	}
}

func spanToProto(span *models.Span) *tracepb.Span {
	return &tracepb.Span{
		TraceId:           []byte(span.TraceID),
		SpanId:            []byte(span.SpanID),
		ParentSpanId:      []byte(span.ParentID),
		Name:              span.Operation,
		Kind:              tracepb.Span_SpanKind(span.Kind),
		StartTimeUnixNano: uint64(span.StartTime.UnixNano()),
		EndTimeUnixNano:   uint64(span.EndTime.UnixNano()),
		Attributes:        attributesToProto(span.Attributes),
		Status: &tracepb.Status{
			Code:    tracepb.Status_StatusCode(span.Status.Code),
			Message: span.Status.Description,
		},
	}
}

func attributesToProto(attrs map[string]string) []*common.KeyValue {
	result := make([]*common.KeyValue, 0, len(attrs))
	for k, v := range attrs {
		result = append(result, &common.KeyValue{
			Key:   k,
			Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: v}},
		})
	}
	return result
}
