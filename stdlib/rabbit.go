package stdlib

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/adriangitvitz/yoru/interpreter"
	amqp "github.com/rabbitmq/amqp091-go"
)

// RabbitProvider implements the Rabbit effect.
//
// Lazy single Connection plus one Channel per queue. Consumed messages land
// on a per-queue in-memory channel so Rabbit.consume() can be a blocking
// pull rather than driving a callback API. Failures surface as
// Result.Err{kind: "rabbit_failed"}.
type RabbitProvider struct {
	URL string

	mu        sync.Mutex
	conn      *amqp.Connection
	channels  map[string]*amqp.Channel        // queue name -> channel
	deliveries map[string]<-chan amqp.Delivery // queue name -> in-memory buffer
	tagsByID  map[string]amqp.Delivery        // delivery id -> Delivery (for ack lookup)
	nextTagID int

	// ConsumeTimeout bounds Rabbit.consume(); returns empty Delivery on timeout.
	ConsumeTimeout time.Duration
}

// NewRabbitProvider configures lazy dialing of url (amqp://user:pass@host:5672/vhost).
func NewRabbitProvider(url string) *RabbitProvider {
	return &RabbitProvider{
		URL:            url,
		channels:       make(map[string]*amqp.Channel),
		deliveries:     make(map[string]<-chan amqp.Delivery),
		tagsByID:       make(map[string]amqp.Delivery),
		ConsumeTimeout: 30 * time.Second,
	}
}

func (p *RabbitProvider) EffectName() string { return "Rabbit" }

func (p *RabbitProvider) Methods() map[string]interpreter.Value {
	return map[string]interpreter.Value{
		// publish(queue, body) -> "ok". Default exchange, routing key = queue.
		// Queue is auto-declared durable on first use.
		"publish": &interpreter.BuiltinVal{Name: "Rabbit.publish", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			queue, body, err := twoStrings("Rabbit.publish", args)
			if err != nil {
				return rabbitErr("rabbit_bad_args", err.Error()), nil
			}
			ch, err := p.openChannel(queue)
			if err != nil {
				return rabbitErr("rabbit_failed", err.Error()), nil
			}
			err = ch.PublishWithContext(context.Background(),
				"",      // default exchange
				queue,   // routing key = queue name
				false,   // mandatory
				false,   // immediate
				amqp.Publishing{
					ContentType: "text/plain",
					Body:        []byte(body),
				})
			if err != nil {
				return rabbitErr("rabbit_failed", err.Error()), nil
			}
			return &interpreter.StringVal{V: "ok"}, nil
		}},

		// consume(queue) -> Delivery { body, ack_id }. Blocks up to
		// ConsumeTimeout; empty Delivery on timeout signals drained queue.
		"consume": &interpreter.BuiltinVal{Name: "Rabbit.consume", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 1 {
				return rabbitErr("rabbit_bad_args", "Rabbit.consume(queue) takes 1 argument"), nil
			}
			queue, ok := args[0].(*interpreter.StringVal)
			if !ok {
				return rabbitErr("rabbit_bad_args", "queue must be a String"), nil
			}
			deliveries, err := p.startConsumer(queue.V)
			if err != nil {
				return rabbitErr("rabbit_failed", err.Error()), nil
			}
			select {
			case d, ok := <-deliveries:
				if !ok {
					return rabbitErr("rabbit_failed", "consumer channel closed"), nil
				}
				tagID := p.storeDelivery(d)
				return &interpreter.ObjectVal{
					TypeName: "Delivery",
					Fields: map[string]interpreter.Value{
						"body":   &interpreter.StringVal{V: string(d.Body)},
						"ack_id": &interpreter.StringVal{V: tagID},
					},
				}, nil
			case <-time.After(p.ConsumeTimeout):
				return &interpreter.ObjectVal{
					TypeName: "Delivery",
					Fields: map[string]interpreter.Value{
						"body":   &interpreter.StringVal{V: ""},
						"ack_id": &interpreter.StringVal{V: ""},
					},
				}, nil
			}
		}},

		// ack(tag) -> "ok". ACKs the message identified by an opaque tag from consume().
		"ack": &interpreter.BuiltinVal{Name: "Rabbit.ack", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 1 {
				return rabbitErr("rabbit_bad_args", "Rabbit.ack(tag) takes 1 argument"), nil
			}
			tag, ok := args[0].(*interpreter.StringVal)
			if !ok {
				return rabbitErr("rabbit_bad_args", "tag must be a String"), nil
			}
			if tag.V == "" {
				return rabbitErr("rabbit_bad_args", "ack called with empty tag (consume returned no message)"), nil
			}
			p.mu.Lock()
			d, exists := p.tagsByID[tag.V]
			if !exists {
				p.mu.Unlock()
				return rabbitErr("rabbit_failed", "unknown delivery tag: "+tag.V), nil
			}
			delete(p.tagsByID, tag.V)
			p.mu.Unlock()
			if err := d.Ack(false); err != nil {
				return rabbitErr("rabbit_failed", err.Error()), nil
			}
			return &interpreter.StringVal{V: "ok"}, nil
		}},

		// queue_size(queue) -> Int. Health checks and tests.
		"queue_size": &interpreter.BuiltinVal{Name: "Rabbit.queue_size", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 1 {
				return rabbitErr("rabbit_bad_args", "Rabbit.queue_size(queue) takes 1 argument"), nil
			}
			queue, ok := args[0].(*interpreter.StringVal)
			if !ok {
				return rabbitErr("rabbit_bad_args", "queue must be a String"), nil
			}
			ch, err := p.openChannel(queue.V)
			if err != nil {
				return rabbitErr("rabbit_failed", err.Error()), nil
			}
			// Passive QueueDeclare replaces the deprecated QueueInspect:
			// reads metadata without creating.
			q, err := ch.QueueDeclarePassive(queue.V, false, false, false, false, nil)
			if err != nil {
				return rabbitErr("rabbit_failed", err.Error()), nil
			}
			return &interpreter.IntVal{V: int64(q.Messages)}, nil
		}},

		// ping() -> "ok"  (verifies the connection is alive)
		"ping": &interpreter.BuiltinVal{Name: "Rabbit.ping", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if _, err := p.dial(); err != nil {
				return rabbitErr("rabbit_failed", err.Error()), nil
			}
			return &interpreter.StringVal{V: "ok"}, nil
		}},
	}
}

// dial returns the cached Connection, opening it on first use.
func (p *RabbitProvider) dial() (*amqp.Connection, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn != nil && !p.conn.IsClosed() {
		return p.conn, nil
	}
	conn, err := amqp.Dial(p.URL)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", p.URL, err)
	}
	p.conn = conn
	return conn, nil
}

// openChannel opens (and caches) a channel, declaring the queue durable on first use.
func (p *RabbitProvider) openChannel(queue string) (*amqp.Channel, error) {
	if _, err := p.dial(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if ch, ok := p.channels[queue]; ok && !ch.IsClosed() {
		return ch, nil
	}
	ch, err := p.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("open channel: %w", err)
	}
	if _, err := ch.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		_ = ch.Close()
		return nil, fmt.Errorf("declare queue: %w", err)
	}
	p.channels[queue] = ch
	return ch, nil
}

// startConsumer registers an AMQP Consume for queue, caching the delivery channel.
func (p *RabbitProvider) startConsumer(queue string) (<-chan amqp.Delivery, error) {
	ch, err := p.openChannel(queue)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if d, ok := p.deliveries[queue]; ok {
		return d, nil
	}
	// Manual ack: caller must invoke Rabbit.ack(tag) after processing.
	d, err := ch.Consume(queue, "yoru-consumer", false, false, false, false, nil)
	if err != nil {
		return nil, fmt.Errorf("start consume: %w", err)
	}
	p.deliveries[queue] = d
	return d, nil
}

// storeDelivery records an in-flight Delivery and returns its opaque id.
func (p *RabbitProvider) storeDelivery(d amqp.Delivery) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextTagID++
	id := fmt.Sprintf("rmq-%d", p.nextTagID)
	p.tagsByID[id] = d
	return id
}

// rabbitErr is the Result.Err{kind, message} constructor for this provider.
func rabbitErr(kind, message string) interpreter.Value {
	errObj := &interpreter.ObjectVal{
		TypeName: "Error",
		Fields: map[string]interpreter.Value{
			"kind":    &interpreter.StringVal{V: kind},
			"message": &interpreter.StringVal{V: message},
		},
	}
	return &interpreter.EnumVal{
		TypeName: "Result",
		Variant:  "Err",
		Fields: map[string]interpreter.Value{
			"error": errObj,
		},
	}
}
