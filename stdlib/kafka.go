package stdlib

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adriangitvitz/yoru/interpreter"
	"github.com/segmentio/kafka-go"
)

// KafkaProvider implements the Kafka effect via kafka-go (pure Go, no cgo).
//
// Caches one Writer per topic and one Reader per (topic, group_id). Auto-
// commit is disabled: read_message returns an opaque ack_id and the caller
// must Kafka.commit(ack_id) for at-least-once delivery. A crash before
// commit causes the partition to be reassigned and the message redelivered.
//
// Failures surface as Result.Err{kind: "kafka_failed" | "kafka_bad_args"}.
type KafkaProvider struct {
	Brokers     []string
	ReadTimeout time.Duration

	mu        sync.Mutex
	writers   map[string]*kafka.Writer       // topic -> writer
	readers   map[string]*kafka.Reader       // "topic|group" -> reader
	messages  map[string]kafkaPending        // ack_id -> pending message
	nextAckID int
}

type kafkaPending struct {
	Reader  *kafka.Reader
	Message kafka.Message
}

// NewKafkaProvider builds a provider configured against a comma-separated
// list of broker host:port addresses (e.g. "127.0.0.1:9092,b2:9092").
func NewKafkaProvider(brokers string) *KafkaProvider {
	parts := strings.Split(brokers, ",")
	clean := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			clean = append(clean, t)
		}
	}
	return &KafkaProvider{
		Brokers:     clean,
		ReadTimeout: 5 * time.Second,
		writers:     make(map[string]*kafka.Writer),
		readers:     make(map[string]*kafka.Reader),
		messages:    make(map[string]kafkaPending),
	}
}

func (p *KafkaProvider) EffectName() string { return "Kafka" }

func (p *KafkaProvider) Methods() map[string]interpreter.Value {
	return map[string]interpreter.Value{
		// write_message(topic, key, value) -> "ok". Key drives partition
		// routing (same key → same partition); empty key = round-robin.
		"write_message": &interpreter.BuiltinVal{Name: "Kafka.write_message", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 3 {
				return kafkaErr("kafka_bad_args", "Kafka.write_message(topic, key, value) takes 3 arguments"), nil
			}
			topic, ok := args[0].(*interpreter.StringVal)
			if !ok {
				return kafkaErr("kafka_bad_args", "topic must be a String"), nil
			}
			key, ok := args[1].(*interpreter.StringVal)
			if !ok {
				return kafkaErr("kafka_bad_args", "key must be a String"), nil
			}
			value, ok := args[2].(*interpreter.StringVal)
			if !ok {
				return kafkaErr("kafka_bad_args", "value must be a String"), nil
			}
			w := p.writerFor(topic.V)
			msg := kafka.Message{Value: []byte(value.V)}
			if key.V != "" {
				msg.Key = []byte(key.V)
			}
			if err := w.WriteMessages(context.Background(), msg); err != nil {
				return kafkaErr("kafka_failed", err.Error()), nil
			}
			return &interpreter.StringVal{V: "ok"}, nil
		}},

		// read_message(topic, group_id) -> Delivery { body, key, ack_id }.
		// Blocks up to ReadTimeout; empty ack_id on timeout signals a drained topic.
		"read_message": &interpreter.BuiltinVal{Name: "Kafka.read_message", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			topic, group, err := twoStrings("Kafka.read_message", args)
			if err != nil {
				return kafkaErr("kafka_bad_args", err.Error()), nil
			}
			r := p.readerFor(topic, group)
			ctx, cancel := context.WithTimeout(context.Background(), p.ReadTimeout)
			defer cancel()
			m, err := r.FetchMessage(ctx)
			if err != nil {
				if err == context.DeadlineExceeded {
					return emptyKafkaDelivery(), nil
				}
				return kafkaErr("kafka_failed", err.Error()), nil
			}
			ackID := p.storeMessage(r, m)
			return &interpreter.ObjectVal{
				TypeName: "Delivery",
				Fields: map[string]interpreter.Value{
					"body":   &interpreter.StringVal{V: string(m.Value)},
					"key":    &interpreter.StringVal{V: string(m.Key)},
					"ack_id": &interpreter.StringVal{V: ackID},
				},
			}, nil
		}},

		// commit(ack_id) -> "ok". Mirrors SQS.delete_message / Rabbit.ack:
		// only after commit is the message durably processed.
		"commit": &interpreter.BuiltinVal{Name: "Kafka.commit", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 1 {
				return kafkaErr("kafka_bad_args", "Kafka.commit(ack_id) takes 1 argument"), nil
			}
			ackID, ok := args[0].(*interpreter.StringVal)
			if !ok {
				return kafkaErr("kafka_bad_args", "ack_id must be a String"), nil
			}
			if ackID.V == "" {
				return kafkaErr("kafka_bad_args", "commit called with empty ack_id"), nil
			}
			p.mu.Lock()
			pending, exists := p.messages[ackID.V]
			if !exists {
				p.mu.Unlock()
				return kafkaErr("kafka_failed", "unknown ack_id: "+ackID.V), nil
			}
			delete(p.messages, ackID.V)
			p.mu.Unlock()
			if err := pending.Reader.CommitMessages(context.Background(), pending.Message); err != nil {
				return kafkaErr("kafka_failed", err.Error()), nil
			}
			return &interpreter.StringVal{V: "ok"}, nil
		}},

		// create_topic(name, partitions) -> "ok". Idempotent; for demos/tests.
		"create_topic": &interpreter.BuiltinVal{Name: "Kafka.create_topic", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 2 {
				return kafkaErr("kafka_bad_args", "Kafka.create_topic(name, partitions) takes 2 arguments"), nil
			}
			name, ok := args[0].(*interpreter.StringVal)
			if !ok {
				return kafkaErr("kafka_bad_args", "name must be a String"), nil
			}
			parts, ok := args[1].(*interpreter.IntVal)
			if !ok {
				return kafkaErr("kafka_bad_args", "partitions must be an Int"), nil
			}
			if err := p.createTopic(name.V, int(parts.V)); err != nil {
				return kafkaErr("kafka_failed", err.Error()), nil
			}
			return &interpreter.StringVal{V: "ok"}, nil
		}},

		// ping() -> "ok"
		"ping": &interpreter.BuiltinVal{Name: "Kafka.ping", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(p.Brokers) == 0 {
				return kafkaErr("kafka_failed", "no brokers configured"), nil
			}
			conn, err := kafka.DialContext(context.Background(), "tcp", p.Brokers[0])
			if err != nil {
				return kafkaErr("kafka_failed", err.Error()), nil
			}
			defer func() { _ = conn.Close() }()
			if _, err := conn.Brokers(); err != nil {
				return kafkaErr("kafka_failed", err.Error()), nil
			}
			return &interpreter.StringVal{V: "ok"}, nil
		}},
	}
}

// writerFor returns (and caches) a Writer for the given topic.
func (p *KafkaProvider) writerFor(topic string) *kafka.Writer {
	p.mu.Lock()
	defer p.mu.Unlock()
	if w, ok := p.writers[topic]; ok {
		return w
	}
	w := &kafka.Writer{
		Addr:                   kafka.TCP(p.Brokers...),
		Topic:                  topic,
		Balancer:               &kafka.Hash{}, // key-based routing when key != ""
		AllowAutoTopicCreation: true,
		BatchTimeout:           50 * time.Millisecond,
	}
	p.writers[topic] = w
	return w
}

// readerFor returns (and caches) a Reader for the (topic, group_id) pair.
func (p *KafkaProvider) readerFor(topic, group string) *kafka.Reader {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := topic + "|" + group
	if r, ok := p.readers[key]; ok {
		return r
	}
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        p.Brokers,
		Topic:          topic,
		GroupID:        group,
		MinBytes:       1,
		MaxBytes:       10e6,
		CommitInterval: 0, // disable auto-commit; we commit explicitly per message
	})
	p.readers[key] = r
	return r
}

// storeMessage records a fetched message under an opaque ack_id.
func (p *KafkaProvider) storeMessage(r *kafka.Reader, m kafka.Message) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextAckID++
	id := "kfk-" + strconv.Itoa(p.nextAckID)
	p.messages[id] = kafkaPending{Reader: r, Message: m}
	return id
}

// createTopic creates a topic if missing; "already exists" → success.
func (p *KafkaProvider) createTopic(name string, partitions int) error {
	if len(p.Brokers) == 0 {
		return fmt.Errorf("no brokers configured")
	}
	if partitions < 1 {
		partitions = 1
	}
	conn, err := kafka.DialContext(context.Background(), "tcp", p.Brokers[0])
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer func() { _ = conn.Close() }()
	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("controller: %w", err)
	}
	ctrlConn, err := kafka.DialContext(context.Background(), "tcp", net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port)))
	if err != nil {
		return fmt.Errorf("dial controller: %w", err)
	}
	defer func() { _ = ctrlConn.Close() }()
	err = ctrlConn.CreateTopics(kafka.TopicConfig{
		Topic:             name,
		NumPartitions:     partitions,
		ReplicationFactor: 1,
	})
	if err != nil && !strings.Contains(err.Error(), "Topic with this name already exists") {
		return err
	}
	return nil
}

// emptyKafkaDelivery is the "no message available" sentinel.
func emptyKafkaDelivery() interpreter.Value {
	return &interpreter.ObjectVal{
		TypeName: "Delivery",
		Fields: map[string]interpreter.Value{
			"body":   &interpreter.StringVal{V: ""},
			"key":    &interpreter.StringVal{V: ""},
			"ack_id": &interpreter.StringVal{V: ""},
		},
	}
}

func kafkaErr(kind, message string) interpreter.Value {
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
