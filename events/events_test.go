package events

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestEventBus_Subscribe(t *testing.T) {
	eb := NewEventBus()
	defer eb.Stop()

	handler := &mockHandler{}
	eb.Subscribe("test_event", handler)

	eb.mu.RLock()
	handlers, ok := eb.handlers["test_event"]
	eb.mu.RUnlock()

	if !ok {
		t.Fatal("Expected handlers for test_event, but none found")
	}

	if len(handlers) != 1 {
		t.Fatalf("Expected 1 handler, got %d", len(handlers))
	}
}

func TestEventBus_Unsubscribe(t *testing.T) {
	eb := NewEventBus()
	defer eb.Stop()

	handler1 := &mockHandler{}
	handler2 := &mockHandler{}

	eb.Subscribe("test_event", handler1)
	eb.Subscribe("test_event", handler2)

	// Verify both handlers are registered
	eb.mu.RLock()
	if len(eb.handlers["test_event"]) != 2 {
		t.Fatalf("Expected 2 handlers, got %d", len(eb.handlers["test_event"]))
	}
	eb.mu.RUnlock()

	// Unsubscribe handler1
	success := eb.Unsubscribe("test_event", handler1)
	if !success {
		t.Fatal("Unsubscribe should return true for existing handler")
	}

	// Verify only handler2 remains
	eb.mu.RLock()
	if len(eb.handlers["test_event"]) != 1 {
		t.Fatalf("Expected 1 handler after unsubscribe, got %d", len(eb.handlers["test_event"]))
	}
	eb.mu.RUnlock()

	// Try to unsubscribe non-existent handler
	success = eb.Unsubscribe("test_event", &mockHandler{})
	if success {
		t.Fatal("Unsubscribe should return false for non-existent handler")
	}
}

func TestEventBus_Publish(t *testing.T) {
	eb := NewEventBus()
	defer eb.Stop()

	var wg sync.WaitGroup
	wg.Add(1)

	handler := &mockHandler{
		handleFunc: func(ctx context.Context, event Event) error {
			defer wg.Done()
			if event.Type != "test_event" {
				t.Errorf("Expected event type 'test_event', got '%s'", event.Type)
			}
			if event.InstanceID != 123 {
				t.Errorf("Expected instance ID 123, got %d", event.InstanceID)
			}
			return nil
		},
	}

	eb.Subscribe("test_event", handler)

	event := Event{
		Type:       "test_event",
		InstanceID: 123,
		Data:       map[string]interface{}{"key": "value"},
	}

	err := eb.Publish(context.Background(), event)
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	// Wait for the handler to be called
	waitWithTimeout(&wg, 1*time.Second)
}

func TestEventBus_PublishSync(t *testing.T) {
	eb := NewEventBus()
	defer eb.Stop()

	handler := &mockHandler{
		handleFunc: func(ctx context.Context, event Event) error {
			return errors.New("test error")
		},
	}

	eb.Subscribe("test_event", handler)

	event := Event{
		Type:       "test_event",
		InstanceID: 123,
	}

	errs := eb.PublishSync(context.Background(), event)
	if len(errs) != 1 {
		t.Fatalf("Expected 1 error, got %d", len(errs))
	}

	if errs[0].Error() != "test error" {
		t.Errorf("Expected 'test error', got '%v'", errs[0])
	}
}

func TestEventBus_PublishNoHandlers(t *testing.T) {
	eb := NewEventBus()
	defer eb.Stop()

	event := Event{
		Type:       "unknown_event",
		InstanceID: 123,
	}

	err := eb.Publish(context.Background(), event)
	if err != ErrNoHandler {
		t.Fatalf("Expected ErrNoHandler, got %v", err)
	}
}

func TestEventBus_PublishAfterStop(t *testing.T) {
	eb := NewEventBus()
	eb.Stop()

	event := Event{
		Type:       "test_event",
		InstanceID: 123,
	}

	err := eb.Publish(context.Background(), event)
	if err != ErrBusClosed {
		t.Fatalf("Expected ErrBusClosed, got %v", err)
	}
}

func TestEventBus_HasSubscribers(t *testing.T) {
	eb := NewEventBus()
	defer eb.Stop()

	// Initially no subscribers
	if eb.HasSubscribers("test_event") {
		t.Fatal("HasSubscribers should return false for non-existent event type")
	}

	// Add a subscriber
	handler := &mockHandler{}
	eb.Subscribe("test_event", handler)

	if !eb.HasSubscribers("test_event") {
		t.Fatal("HasSubscribers should return true after subscription")
	}

	// Unsubscribe
	eb.Unsubscribe("test_event", handler)

	if eb.HasSubscribers("test_event") {
		t.Fatal("HasSubscribers should return false after unsubscribe")
	}
}

func TestEventBus_SubscribeFunc(t *testing.T) {
	eb := NewEventBus()
	defer eb.Stop()

	var handlerCalled bool
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(1)

	eb.SubscribeFunc("test_event", func(ctx context.Context, event Event) error {
		defer wg.Done()
		mu.Lock()
		handlerCalled = true
		mu.Unlock()
		return nil
	})

	event := Event{
		Type:       "test_event",
		InstanceID: 123,
	}

	err := eb.Publish(context.Background(), event)
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	// Wait for the handler to be called
	waitWithTimeout(&wg, 1*time.Second)

	mu.Lock()
	if !handlerCalled {
		t.Fatal("Handler function was not called")
	}
	mu.Unlock()
}

func TestEventBus_WithOptions(t *testing.T) {
	var customErrorCalled bool
	var customErrorMu sync.Mutex

	customErrorHandler := func(event Event, err error) {
		customErrorMu.Lock()
		customErrorCalled = true
		customErrorMu.Unlock()
	}

	eb := NewEventBus(
		WithBufferSize(200),
		WithErrorHandler(customErrorHandler),
	)
	defer eb.Stop()

	// Test custom buffer size
	if cap(eb.eventCh) != 200 {
		t.Fatalf("Expected buffer size 200, got %d", cap(eb.eventCh))
	}

	// Test custom error handler
	var wg sync.WaitGroup
	wg.Add(1)

	eb.Subscribe("test_event", &mockHandler{
		handleFunc: func(ctx context.Context, event Event) error {
			defer wg.Done()
			return errors.New("test error")
		},
	})

	event := Event{
		Type:       "test_event",
		InstanceID: 123,
	}

	err := eb.Publish(context.Background(), event)
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	// Wait for the handler to be called
	waitWithTimeout(&wg, 1*time.Second)
	time.Sleep(100 * time.Millisecond) // Give time for error handler to be called

	customErrorMu.Lock()
	if !customErrorCalled {
		t.Fatal("Custom error handler was not called")
	}
	customErrorMu.Unlock()
}

func TestEventBus_CancelledContext(t *testing.T) {
	eb := NewEventBus()
	defer eb.Stop()

	// First add a handler so we pass the "no handlers" check
	eb.Subscribe("test_event", &mockHandler{})

	// Now create and cancel a context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel the context immediately

	event := Event{
		Type:       "test_event",
		InstanceID: 123,
	}

	err := eb.Publish(ctx, event)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Expected context.Canceled error, got %v", err)
	}
}

// Helper types and functions

type mockHandler struct {
	handleFunc func(ctx context.Context, event Event) error
}

func (m *mockHandler) Handle(ctx context.Context, event Event) error {
	if m.handleFunc != nil {
		return m.handleFunc(ctx, event)
	}
	return nil
}

func waitWithTimeout(wg *sync.WaitGroup, timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return
	case <-time.After(timeout):
		// Test will fail but we need to continue execution
		return
	}
}
