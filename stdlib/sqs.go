package stdlib

import (
	"context"
	"fmt"
	"os"

	"github.com/adriangitvitz/yoru/interpreter"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// SQSProvider implements the SQS effect via AWS SDK v2.
//
// Reads env at install: AWS_REGION, AWS_ACCESS_KEY_ID/SECRET (default chain),
// optional SQS_ENDPOINT_URL for ElasticMQ/LocalStack. Method shape mirrors
// Rabbit (send/receive/delete/queue_size/ping). Failures surface as
// Result.Err{kind: "sqs_failed" | "sqs_bad_args"}.
type SQSProvider struct {
	Client   *sqs.Client
	WaitTime int32 // long-poll seconds for receive_message (0..20)
	// receipts hides the raw AWS receipt handle behind an opaque ack_id.
	receipts  map[string]sqsReceipt
	nextAckID int
}

type sqsReceipt struct {
	QueueURL      string
	ReceiptHandle string
}

// NewSQSProvider uses the default AWS config chain; SQS_ENDPOINT_URL
// overrides the endpoint for emulators. Returns nil if config load fails.
func NewSQSProvider() *SQSProvider {
	cfg, err := awsconfig.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil
	}

	var client *sqs.Client
	if endpoint := os.Getenv("SQS_ENDPOINT_URL"); endpoint != "" {
		client = sqs.NewFromConfig(cfg, func(o *sqs.Options) {
			o.BaseEndpoint = aws.String(endpoint)
		})
	} else {
		client = sqs.NewFromConfig(cfg)
	}

	return &SQSProvider{
		Client:   client,
		WaitTime: 5, // long-poll 5s by default; SQS max is 20s
		receipts: make(map[string]sqsReceipt),
	}
}

func (p *SQSProvider) EffectName() string { return "SQS" }

func (p *SQSProvider) Methods() map[string]interpreter.Value {
	return map[string]interpreter.Value{
		// send_message(queue_url, body) -> "ok"
		"send_message": &interpreter.BuiltinVal{Name: "SQS.send_message", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			queueURL, body, err := twoStrings("SQS.send_message", args)
			if err != nil {
				return sqsErr("sqs_bad_args", err.Error()), nil
			}
			_, err = p.Client.SendMessage(context.Background(), &sqs.SendMessageInput{
				QueueUrl:    aws.String(queueURL),
				MessageBody: aws.String(body),
			})
			if err != nil {
				return sqsErr("sqs_failed", err.Error()), nil
			}
			return &interpreter.StringVal{V: "ok"}, nil
		}},

		// receive_message(queue_url) -> Delivery { body, ack_id }. Long-polls
		// up to WaitTime; empty Delivery on timeout. SQS visibility timeout
		// (default 30s) returns un-deleted messages → at-least-once.
		"receive_message": &interpreter.BuiltinVal{Name: "SQS.receive_message", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 1 {
				return sqsErr("sqs_bad_args", "SQS.receive_message(queue_url) takes 1 argument"), nil
			}
			queueURL, ok := args[0].(*interpreter.StringVal)
			if !ok {
				return sqsErr("sqs_bad_args", "queue_url must be a String"), nil
			}
			out, err := p.Client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
				QueueUrl:            aws.String(queueURL.V),
				MaxNumberOfMessages: 1,
				WaitTimeSeconds:     p.WaitTime,
			})
			if err != nil {
				return sqsErr("sqs_failed", err.Error()), nil
			}
			if len(out.Messages) == 0 {
				return emptyDelivery(), nil
			}
			m := out.Messages[0]
			ackID := p.storeReceipt(queueURL.V, aws.ToString(m.ReceiptHandle))
			return &interpreter.ObjectVal{
				TypeName: "Delivery",
				Fields: map[string]interpreter.Value{
					"body":   &interpreter.StringVal{V: aws.ToString(m.Body)},
					"ack_id": &interpreter.StringVal{V: ackID},
				},
			}, nil
		}},

		// delete_message(ack_id) -> "ok". SQS's "ack": prevents redelivery
		// after the visibility timeout. Mirrors Rabbit.ack.
		"delete_message": &interpreter.BuiltinVal{Name: "SQS.delete_message", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 1 {
				return sqsErr("sqs_bad_args", "SQS.delete_message(ack_id) takes 1 argument"), nil
			}
			ackID, ok := args[0].(*interpreter.StringVal)
			if !ok {
				return sqsErr("sqs_bad_args", "ack_id must be a String"), nil
			}
			if ackID.V == "" {
				return sqsErr("sqs_bad_args", "delete_message called with empty ack_id"), nil
			}
			r, exists := p.receipts[ackID.V]
			if !exists {
				return sqsErr("sqs_failed", "unknown ack_id: "+ackID.V), nil
			}
			delete(p.receipts, ackID.V)
			_, err := p.Client.DeleteMessage(context.Background(), &sqs.DeleteMessageInput{
				QueueUrl:      aws.String(r.QueueURL),
				ReceiptHandle: aws.String(r.ReceiptHandle),
			})
			if err != nil {
				return sqsErr("sqs_failed", err.Error()), nil
			}
			return &interpreter.StringVal{V: "ok"}, nil
		}},

		// queue_size(queue_url) -> Int. ApproximateNumberOfMessages — not exact.
		"queue_size": &interpreter.BuiltinVal{Name: "SQS.queue_size", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 1 {
				return sqsErr("sqs_bad_args", "SQS.queue_size(queue_url) takes 1 argument"), nil
			}
			queueURL, ok := args[0].(*interpreter.StringVal)
			if !ok {
				return sqsErr("sqs_bad_args", "queue_url must be a String"), nil
			}
			out, err := p.Client.GetQueueAttributes(context.Background(), &sqs.GetQueueAttributesInput{
				QueueUrl:       aws.String(queueURL.V),
				AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameApproximateNumberOfMessages},
			})
			if err != nil {
				return sqsErr("sqs_failed", err.Error()), nil
			}
			countStr := out.Attributes[string(sqstypes.QueueAttributeNameApproximateNumberOfMessages)]
			var n int64
			_, _ = fmt.Sscanf(countStr, "%d", &n)
			return &interpreter.IntVal{V: n}, nil
		}},

		// create_queue(name) -> String. Returns queue URL; for tests/emulators.
		"create_queue": &interpreter.BuiltinVal{Name: "SQS.create_queue", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 1 {
				return sqsErr("sqs_bad_args", "SQS.create_queue(name) takes 1 argument"), nil
			}
			name, ok := args[0].(*interpreter.StringVal)
			if !ok {
				return sqsErr("sqs_bad_args", "name must be a String"), nil
			}
			out, err := p.Client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
				QueueName: aws.String(name.V),
			})
			if err != nil {
				return sqsErr("sqs_failed", err.Error()), nil
			}
			return &interpreter.StringVal{V: aws.ToString(out.QueueUrl)}, nil
		}},

		// ping() -> "ok"  (round-trips a list_queues to verify connectivity)
		"ping": &interpreter.BuiltinVal{Name: "SQS.ping", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			_, err := p.Client.ListQueues(context.Background(), &sqs.ListQueuesInput{})
			if err != nil {
				return sqsErr("sqs_failed", err.Error()), nil
			}
			return &interpreter.StringVal{V: "ok"}, nil
		}},
	}
}

// storeReceipt hides the AWS receipt handle behind an opaque ack_id.
// Uses a monotonic counter so deleted ids are not reused.
func (p *SQSProvider) storeReceipt(queueURL, handle string) string {
	p.nextAckID++
	id := fmt.Sprintf("sqs-%d", p.nextAckID)
	p.receipts[id] = sqsReceipt{QueueURL: queueURL, ReceiptHandle: handle}
	return id
}

// emptyDelivery signals "queue empty / poll timed out" via body == "".
func emptyDelivery() interpreter.Value {
	return &interpreter.ObjectVal{
		TypeName: "Delivery",
		Fields: map[string]interpreter.Value{
			"body":   &interpreter.StringVal{V: ""},
			"ack_id": &interpreter.StringVal{V: ""},
		},
	}
}

func sqsErr(kind, message string) interpreter.Value {
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
