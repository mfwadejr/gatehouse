// Package events is the in-process message bus behind the console's live widgets.
package events

import (
	"encoding/json"
	"sync"
	"time"
)

// Message is one update on a topic.
type Message struct {
	Topic string          `json:"topic"`
	Data  json.RawMessage `json:"data"`
	At    int64           `json:"at"`
}

// Bus fans messages out by topic and remembers the latest one per topic so a new
// subscriber gets the current state immediately.
type Bus struct {
	mu   sync.RWMutex
	subs map[chan Message]map[string]bool
	last map[string]Message
}

func NewBus() *Bus {
	return &Bus{subs: map[chan Message]map[string]bool{}, last: map[string]Message{}}
}

// Publish sends v (JSON-encoded) to every subscriber of topic.
func (b *Bus) Publish(topic string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	m := Message{Topic: topic, Data: data, At: time.Now().UnixMilli()}
	b.mu.Lock()
	b.last[topic] = m
	for ch, topics := range b.subs {
		if topics[topic] {
			select {
			case ch <- m:
			default: // slow consumer: drop the oldest queued message, keep the newest
				select {
				case <-ch:
				default:
				}
				select {
				case ch <- m:
				default:
				}
			}
		}
	}
	b.mu.Unlock()
}

// Latest returns the most recent message for a topic.
func (b *Bus) Latest(topic string) (Message, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	m, ok := b.last[topic]
	return m, ok
}

// Subscribe returns a channel of messages for the given topics, primed with the latest
// value of each, and a cancel function.
func (b *Bus) Subscribe(topics ...string) (<-chan Message, func()) {
	ch := make(chan Message, 32)
	set := map[string]bool{}
	for _, t := range topics {
		set[t] = true
	}
	b.mu.Lock()
	b.subs[ch] = set
	for _, t := range topics {
		if m, ok := b.last[t]; ok {
			select {
			case ch <- m:
			default:
			}
		}
	}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
	}
}
