package observability

import (
	"context"
	"fmt"
	"sync"
	"time"

	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/utils"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Exporter sends telemetry to the observability service; its gRPC client is one
type Exporter interface {
	RecordSpan(ctx context.Context, in *gogiv1.RecordSpanRequest, opts ...grpc.CallOption) (*gogiv1.RecordSpanResponse, error)
	RecordGeneration(ctx context.Context, in *gogiv1.RecordGenerationRequest, opts ...grpc.CallOption) (*gogiv1.RecordGenerationResponse, error)
	IngestLogs(ctx context.Context, in *gogiv1.IngestLogsRequest, opts ...grpc.CallOption) (*gogiv1.IngestLogsResponse, error)
	RecordMetrics(ctx context.Context, in *gogiv1.RecordMetricsRequest, opts ...grpc.CallOption) (*gogiv1.RecordMetricsResponse, error)
}

// Config configures a client
type Config struct {
	// Service names the service that reports, e.g. llms
	Service string
	// BatchSize is the most items sent in one call; a buffer this full is sent at once
	BatchSize int
	// FlushInterval is how often the buffers are sent, full or not
	FlushInterval time.Duration
	// MaxBuffer is the most items a buffer holds while the observability service cannot
	// be reached; the oldest are dropped beyond it
	MaxBuffer int
	// Timeout bounds every call to the observability service
	Timeout time.Duration
}

func DefaultConfig(service string) Config {
	return Config{Service: service, BatchSize: 100, FlushInterval: 5 * time.Second, MaxBuffer: 10_000,
		Timeout: 5 * time.Second}
}

// buffer holds items of one kind until they are sent
type buffer[T any] struct {
	items   []T
	dropped int
}

// Client buffers telemetry and sends it to the observability service in batches, in the
// background. Its methods never block on the network and never fail; a nil *Client
// reports nothing
type Client struct {
	exporter Exporter
	config   Config
	now      func() time.Time

	mu          sync.Mutex
	spans       buffer[*gogiv1.Span]
	generations buffer[*gogiv1.Generation]
	logs        buffer[*gogiv1.LogEvent]
	metrics     buffer[*gogiv1.MetricPoint]

	kick    chan struct{}
	stop    chan struct{}
	stopped chan struct{}
	once    sync.Once
}

// NewClient returns a client that sends telemetry through the exporter, and starts sending
func NewClient(exporter Exporter, config Config) *Client {
	client := &Client{exporter: exporter, config: config, now: time.Now, kick: make(chan struct{}, 1),
		stop: make(chan struct{}), stopped: make(chan struct{})}
	go client.run()
	return client
}

// FromEnv returns a client for the observability service at GOGI_OBSERVABILITY_ADDR, and
// a function that sends what is buffered and closes the connection. Without the address,
// telemetry is off: the client is nil
func FromEnv(service string) (*Client, func(), error) {
	addr := utils.GetEnv("GOGI_OBSERVABILITY_ADDR", "")
	if addr == "" {
		return nil, func() {}, nil
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, func() {}, fmt.Errorf("observability service at %s: %w", addr, err)
	}
	client := NewClient(gogiv1.NewObservabilityServiceClient(conn), DefaultConfig(service))
	return client, func() {
		client.Close()
		conn.Close()
	}, nil
}

// Service returns the name of the service that reports
func (c *Client) Service() string {
	if c == nil {
		return ""
	}
	return c.config.Service
}

// add appends an item to a buffer, dropping the oldest beyond the limit, and asks for a
// send when the buffer holds a batch
func add[T any](c *Client, b *buffer[T], item T) {
	c.mu.Lock()
	b.items = append(b.items, item)
	if over := len(b.items) - c.config.MaxBuffer; over > 0 {
		b.items = b.items[over:]
		b.dropped += over
	}
	full := len(b.items) >= c.config.BatchSize
	c.mu.Unlock()

	if full {
		select {
		case c.kick <- struct{}{}:
		default:
		}
	}
}

// RecordSpan buffers a finished span
func (c *Client) RecordSpan(span *gogiv1.Span) {
	if c != nil {
		add(c, &c.spans, span)
	}
}

// RecordGeneration buffers a finished generation
func (c *Client) RecordGeneration(generation *gogiv1.Generation) {
	if c != nil {
		add(c, &c.generations, generation)
	}
}

// Log buffers a structured log event; the service, timestamp and trace context are filled in
func (c *Client) Log(ctx context.Context, severity gogiv1.Severity, eventType, message string,
	attributes map[string]string, numeric map[string]float64) {
	if c == nil {
		return
	}
	trace := FromContext(ctx)
	add(c, &c.logs, &gogiv1.LogEvent{TraceId: trace.TraceID, SpanId: trace.SpanID, Timestamp: c.now().UnixMilli(),
		Service: c.config.Service, Severity: severity, EventType: eventType, Message: message,
		Attributes: attributes, NumericAttributes: numeric, WorkflowId: trace.WorkflowID, UserId: trace.UserID})
}

// Counter buffers a value that accumulates, e.g. a request or its cost
func (c *Client) Counter(name string, value float64, labels map[string]string) {
	c.metric(name, gogiv1.MetricType_METRIC_TYPE_COUNTER, value, labels)
}

// Histogram buffers a measurement whose distribution matters, e.g. a latency
func (c *Client) Histogram(name string, value float64, labels map[string]string) {
	c.metric(name, gogiv1.MetricType_METRIC_TYPE_HISTOGRAM, value, labels)
}

func (c *Client) metric(name string, metricType gogiv1.MetricType, value float64, labels map[string]string) {
	if c == nil {
		return
	}
	add(c, &c.metrics, &gogiv1.MetricPoint{Name: name, Type: metricType, Value: value, Labels: labels,
		Timestamp: c.now().UnixMilli(), Service: c.config.Service})
}

func (c *Client) run() {
	defer close(c.stopped)
	ticker := time.NewTicker(c.config.FlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.Flush()
		case <-c.kick:
			c.Flush()
		case <-c.stop:
			c.Flush()
			return
		}
	}
}

// take removes up to a batch of items from a buffer
func take[T any](c *Client, b *buffer[T]) ([]T, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := min(len(b.items), c.config.BatchSize)
	batch := append([]T(nil), b.items[:n]...)
	b.items = b.items[n:]
	dropped := b.dropped
	b.dropped = 0
	return batch, dropped
}

// putBack returns a batch that could not be sent to the front of its buffer, as far as
// the buffer has room
func putBack[T any](c *Client, b *buffer[T], batch []T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	room := max(c.config.MaxBuffer-len(b.items), 0)
	if room < len(batch) {
		b.dropped += len(batch) - room
		batch = batch[len(batch)-room:]
	}
	b.items = append(batch, b.items...)
}

// flushBuffer sends a buffer in batches until it is empty or a send fails
func flushBuffer[T any](c *Client, b *buffer[T], kind string, send func(context.Context, []T) error) {
	for {
		batch, dropped := take(c, b)
		if dropped > 0 {
			log.Warnf("observability: dropped %d %s that could not be sent", dropped, kind)
		}
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), c.config.Timeout)
		err := send(ctx, batch)
		cancel()
		if err != nil {
			putBack(c, b, batch)
			log.Debugf("observability: failed to send %d %s, will retry: %v", len(batch), kind, err)
			return
		}
		if len(batch) < c.config.BatchSize {
			return
		}
	}
}

// Flush sends what is buffered now
func (c *Client) Flush() {
	if c == nil {
		return
	}
	flushBuffer(c, &c.spans, "spans", func(ctx context.Context, batch []*gogiv1.Span) error {
		_, err := c.exporter.RecordSpan(ctx, &gogiv1.RecordSpanRequest{Spans: batch})
		return err
	})
	flushBuffer(c, &c.generations, "generations", func(ctx context.Context, batch []*gogiv1.Generation) error {
		_, err := c.exporter.RecordGeneration(ctx, &gogiv1.RecordGenerationRequest{Generations: batch})
		return err
	})
	flushBuffer(c, &c.logs, "log events", func(ctx context.Context, batch []*gogiv1.LogEvent) error {
		_, err := c.exporter.IngestLogs(ctx, &gogiv1.IngestLogsRequest{Events: batch})
		return err
	})
	flushBuffer(c, &c.metrics, "metric points", func(ctx context.Context, batch []*gogiv1.MetricPoint) error {
		_, err := c.exporter.RecordMetrics(ctx, &gogiv1.RecordMetricsRequest{Points: batch})
		return err
	})
}

// Close sends what is buffered and stops sending
func (c *Client) Close() {
	if c == nil {
		return
	}
	c.once.Do(func() {
		close(c.stop)
		<-c.stopped
	})
}
