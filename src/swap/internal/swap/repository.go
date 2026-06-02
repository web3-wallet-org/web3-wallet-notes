package swap

import (
	"fmt"
	"sync"
	"time"
)

type Repository interface {
	SaveQuote(quote NormalizedQuote) (NormalizedQuote, error)
	GetQuote(quoteID string) (NormalizedQuote, error)
	CreateOrder(order StoredOrder) (StoredOrder, bool, error)
	GetOrder(orderID string) (StoredOrder, error)
	GetOrderByQuoteID(quoteID string) (StoredOrder, error)
	UpdateOrder(order StoredOrder) error
	AddEvent(event SwapEvent) error
	ListEvents(orderID string) ([]SwapEvent, error)
}

type MemoryRepository struct {
	mu             sync.RWMutex
	now            func() time.Time
	quotes         map[string]NormalizedQuote
	orders         map[string]StoredOrder
	orderByQuoteID map[string]string
	events         map[string][]SwapEvent
}

func NewMemoryRepository(now func() time.Time) *MemoryRepository {
	return &MemoryRepository{
		now:            now,
		quotes:         map[string]NormalizedQuote{},
		orders:         map[string]StoredOrder{},
		orderByQuoteID: map[string]string{},
		events:         map[string][]SwapEvent{},
	}
}

func (r *MemoryRepository) SaveQuote(quote NormalizedQuote) (NormalizedQuote, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if quote.ID == "" {
		quote.ID = newID()
	}
	r.quotes[quote.ID] = quote
	return quote, nil
}

func (r *MemoryRepository) GetQuote(quoteID string) (NormalizedQuote, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	quote, ok := r.quotes[quoteID]
	if !ok {
		return NormalizedQuote{}, fmt.Errorf("%w: quote %s", ErrNotFound, quoteID)
	}
	return quote, nil
}

func (r *MemoryRepository) CreateOrder(order StoredOrder) (StoredOrder, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existingID, ok := r.orderByQuoteID[order.QuoteID]; ok {
		return r.orders[existingID], false, nil
	}
	if order.ID == "" {
		order.ID = newID()
	}
	now := r.now()
	if order.CreatedAt.IsZero() {
		order.CreatedAt = now
	}
	order.UpdatedAt = now
	r.orders[order.ID] = order
	r.orderByQuoteID[order.QuoteID] = order.ID
	return order, true, nil
}

func (r *MemoryRepository) GetOrder(orderID string) (StoredOrder, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	order, ok := r.orders[orderID]
	if !ok {
		return StoredOrder{}, fmt.Errorf("%w: order %s", ErrNotFound, orderID)
	}
	return order, nil
}

func (r *MemoryRepository) GetOrderByQuoteID(quoteID string) (StoredOrder, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	orderID, ok := r.orderByQuoteID[quoteID]
	if !ok {
		return StoredOrder{}, fmt.Errorf("%w: quote %s has no order", ErrNotFound, quoteID)
	}
	return r.orders[orderID], nil
}

func (r *MemoryRepository) UpdateOrder(order StoredOrder) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.orders[order.ID]; !ok {
		return fmt.Errorf("%w: order %s", ErrNotFound, order.ID)
	}
	order.UpdatedAt = r.now()
	r.orders[order.ID] = order
	return nil
}

func (r *MemoryRepository) AddEvent(event SwapEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if event.At.IsZero() {
		event.At = r.now()
	}
	r.events[event.OrderID] = append(r.events[event.OrderID], event)
	return nil
}

func (r *MemoryRepository) ListEvents(orderID string) ([]SwapEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	events := append([]SwapEvent(nil), r.events[orderID]...)
	return events, nil
}
