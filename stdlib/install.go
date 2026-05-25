package stdlib

import (
	"io"
	"os"

	"github.com/adriangitvitz/yoru/interpreter"
)

// InstallAll registers all stdlib effect providers. logWriter targets Log
// output (os.Stderr in production, a buffer in tests).
func InstallAll(interp *interpreter.Interpreter, logWriter io.Writer) {
	// JSON and DB carry the interp handle to validate types / run closures.
	interp.InstallProvider(&JSONProvider{Interp: interp})
	interp.InstallProvider(&CryptoProvider{})
	interp.InstallProvider(&TimeProvider{})
	interp.InstallProvider(NewLogProvider(logWriter, LogInfo))
	interp.InstallProvider(NewDBProvider(NewMemoryDriver()).WithInterp(interp))
	interp.InstallProvider(NewHTTPProvider())
	interp.InstallProvider(NewSubprocessProvider())

	// Opt-in providers (env-gated to avoid useless connections).
	if redisURL := os.Getenv("REDIS_URL"); redisURL != "" {
		if rp := NewRedisProvider(redisURL); rp != nil {
			interp.InstallProvider(rp)
		}
	}
	if rabbitURL := os.Getenv("RABBITMQ_URL"); rabbitURL != "" {
		interp.InstallProvider(NewRabbitProvider(rabbitURL))
	}
	// SQS: also honour SQS_ENDPOINT_URL for ElasticMQ/LocalStack.
	if os.Getenv("AWS_REGION") != "" {
		if sp := NewSQSProvider(); sp != nil {
			interp.InstallProvider(sp)
		}
	}
	// Kafka: KAFKA_BROKERS = comma-separated host:port list.
	if brokers := os.Getenv("KAFKA_BROKERS"); brokers != "" {
		interp.InstallProvider(NewKafkaProvider(brokers))
	}
}
