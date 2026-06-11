// Package bus is the in-process event bus: live notification only.
// Durability lives in the store; a dropped bus message is never data loss.
package bus

import (
	"sync"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

type Bus struct {
	mu   sync.Mutex
	subs map[int]chan core.Event
	next int
}

func New() *Bus {
	return &Bus{subs: map[int]chan core.Event{}}
}

// Subscribe returns a buffered channel and a cancel func. Cancel closes
// the channel and unregisters it; calling cancel twice is safe.
// An unbuffered subscriber would miss nearly every event under
// non-blocking publish, so buffers below 1 are raised to 1.
func (b *Bus) Subscribe(buffer int) (<-chan core.Event, func()) {
	if buffer < 1 {
		buffer = 1
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.next
	b.next++
	ch := make(chan core.Event, buffer)
	b.subs[id] = ch
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if c, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(c)
		}
	}
}

// Publish never blocks: a full subscriber's message is dropped.
func (b *Bus) Publish(ev core.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}
