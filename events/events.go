package events

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"time"
)

var (
	// ErrBusClosed indicates the event bus has been closed.
	ErrBusClosed = errors.New("event bus is closed")
	// ErrChannelFull indicates the event channel is full and cannot accept more events.
	ErrChannelFull = errors.New("event channel is full")
	// ErrNoHandler indicates no handlers are registered for the event type.
	ErrNoHandler = errors.New("no handlers registered for event type")
)

// Event represents a workflow event.
type Event struct {
	Type       string                 // e.g., "state_changed", "error_occurred"
	InstanceID uint64                 // Workflow instance ID
	Data       map[string]interface{} // Additional event data
}

// EventHandler defines the interface for handling events.
type EventHandler interface {
	Handle(ctx context.Context, event Event) error
}

// EventHandlerFunc is a function adapter for EventHandler.
type EventHandlerFunc func(ctx context.Context, event Event) error

// Handle implements the EventHandler interface.
func (f EventHandlerFunc) Handle(ctx context.Context, event Event) error {
	return f(ctx, event)
}

// EventBus manages event subscriptions and publishing.
type EventBus struct {
	handlers     map[string][]EventHandler
	mu           sync.RWMutex
	eventCh      chan Event
	errHandler   func(event Event, err error)
	errHandlerMu sync.RWMutex
	wg           sync.WaitGroup
	closed       bool
	closeMu      sync.RWMutex
}

// EventBusOption defines functional options for configuring EventBus.
type EventBusOption func(*EventBus)

// WithBufferSize sets the event channel buffer size.
func WithBufferSize(size int) EventBusOption {
	return func(eb *EventBus) {
		eb.eventCh = make(chan Event, size)
	}
}

// WithErrorHandler sets a custom error handler function.
func WithErrorHandler(handler func(event Event, err error)) EventBusOption {
	return func(eb *EventBus) {
		eb.errHandlerMu.Lock()
		defer eb.errHandlerMu.Unlock()
		eb.errHandler = handler
	}
}

// NewEventBus creates a new EventBus instance with async processing.
// The default buffer size is 100, and errors are handled by defaultErrorHandler.
// Use options to customize buffer size or error handling.
func NewEventBus(options ...EventBusOption) *EventBus {
	eb := &EventBus{
		handlers:   make(map[string][]EventHandler),
		eventCh:    make(chan Event, 100), // Default buffer size
		errHandler: defaultErrorHandler,
	}

	// Apply options
	for _, option := range options {
		option(eb)
	}

	// Start event processor
	eb.wg.Add(1)
	go eb.processEvents()

	return eb
}

// Subscribe subscribes a handler to an event type.
func (eb *EventBus) Subscribe(eventType string, handler EventHandler) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.handlers[eventType] = append(eb.handlers[eventType], handler)
}

// SubscribeFunc subscribes a function as a handler to an event type.
func (eb *EventBus) SubscribeFunc(eventType string, handlerFunc func(ctx context.Context, event Event) error) {
	eb.Subscribe(eventType, EventHandlerFunc(handlerFunc))
}

// Unsubscribe removes a specific handler from an event type.
// Returns true if the handler was found and removed, false otherwise.
// If no handlers remain for the event type, the entry is deleted.
func (eb *EventBus) Unsubscribe(eventType string, handler EventHandler) bool {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	handlers, exists := eb.handlers[eventType]
	if !exists {
		return false
	}

	for i, h := range handlers {
		if fmt.Sprintf("%p", h) == fmt.Sprintf("%p", handler) { // Compare pointer address
			handlers[i] = handlers[len(handlers)-1]
			eb.handlers[eventType] = handlers[:len(handlers)-1]
			if len(eb.handlers[eventType]) == 0 {
				delete(eb.handlers, eventType)
			}
			return true
		}
	}
	return false
}

// HasSubscribers checks if there are any subscribers for a given event type.
func (eb *EventBus) HasSubscribers(eventType string) bool {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	handlers, exists := eb.handlers[eventType]
	return exists && len(handlers) > 0
}

// Publish publishes an event asynchronously to all subscribed handlers.
// Returns an error if the context is canceled, the bus is closed, or the channel is full.
// Does not guarantee immediate execution; handlers are invoked in a separate goroutine.
func (eb *EventBus) Publish(ctx context.Context, event Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	eb.closeMu.RLock()
	if eb.closed {
		eb.closeMu.RUnlock()
		return ErrBusClosed
	}
	eb.closeMu.RUnlock()

	eb.mu.RLock()
	_, hasHandlers := eb.handlers[event.Type]
	eb.mu.RUnlock()

	if !hasHandlers {
		return ErrNoHandler
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case eb.eventCh <- event:
		return nil
	default:
		return ErrChannelFull
	}
}

// PublishSync publishes an event synchronously and returns all handler errors.
// Execution is subject to a 5-second timeout unless the context specifies otherwise.
// Returns an error slice if the bus is closed, no handlers exist, or handlers fail.
func (eb *EventBus) PublishSync(ctx context.Context, event Event) []error {
	eb.closeMu.RLock()
	if eb.closed {
		eb.closeMu.RUnlock()
		return []error{ErrBusClosed}
	}
	eb.closeMu.RUnlock()

	eb.mu.RLock()
	handlers, ok := eb.handlers[event.Type]
	eb.mu.RUnlock()

	if !ok || len(handlers) == 0 {
		return []error{ErrNoHandler}
	}

	// Apply a default timeout to prevent indefinite blocking
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return eb.executeHandlers(timeoutCtx, handlers, event)
}

// Stop stops the event processing goroutine and waits for completion.
// Any unprocessed events are discarded to ensure a clean shutdown.
func (eb *EventBus) Stop() {
	eb.closeMu.Lock()
	if !eb.closed {
		eb.closed = true
		// Drain remaining events to prevent blocking
		for len(eb.eventCh) > 0 {
			<-eb.eventCh
		}
		close(eb.eventCh)
	}
	eb.closeMu.Unlock()

	eb.wg.Wait()
}

// processEvents handles events asynchronously in a separate goroutine.
func (eb *EventBus) processEvents() {
	defer eb.wg.Done()

	for event := range eb.eventCh {
		eb.mu.RLock()
		handlers, ok := eb.handlers[event.Type]
		eb.mu.RUnlock()

		if !ok || len(handlers) == 0 {
			continue
		}

		errs := eb.executeHandlers(context.Background(), handlers, event)

		eb.errHandlerMu.RLock()
		handler := eb.errHandler
		eb.errHandlerMu.RUnlock()

		for _, err := range errs {
			handler(event, err)
		}
	}
}

// executeHandlers executes all handlers for an event and collects errors.
// Handlers are run concurrently, and the function waits for all to complete.
func (eb *EventBus) executeHandlers(ctx context.Context, handlers []EventHandler, event Event) []error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(handlers))

	for _, handler := range handlers {
		wg.Add(1)
		go func(h EventHandler) {
			defer wg.Done()
			if err := h.Handle(ctx, event); err != nil {
				errCh <- err
			}
		}(handler)
	}

	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}

	return errs
}

// defaultErrorHandler logs errors with stack traces for debugging.
func defaultErrorHandler(event Event, err error) {
	fmt.Printf("Error handling event %s (instance %d): %v\nStack: %s\n",
		event.Type, event.InstanceID, err, debug.Stack())
}
