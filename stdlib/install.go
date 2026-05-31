package stdlib

import (
	"io"
	"os"

	"github.com/adriangitvitz/yoru/interpreter"
)

func InstallAll(interp *interpreter.Interpreter, logWriter io.Writer) {
	interp.InstallProvider(&JSONProvider{Interp: interp})
	interp.InstallProvider(&CryptoProvider{})
	interp.InstallProvider(&TimeProvider{})
	interp.InstallProvider(NewLogProvider(logWriter, LogInfo))
	interp.InstallProvider(NewDBProvider(NewMemoryDriver()).WithInterp(interp))
	interp.InstallProvider(NewHTTPProvider())
	interp.InstallProvider(NewSubprocessProvider())
	interp.InstallProvider((&FSProvider{}).WithInterp(interp))
	interp.InstallProvider(&PathProvider{})
	interp.InstallProvider(&FuzzyProvider{})
	interp.InstallProvider(&DiffProvider{})

	if redisURL := os.Getenv("REDIS_URL"); redisURL != "" {
		if rp := NewRedisProvider(redisURL); rp != nil {
			interp.InstallProvider(rp)
		}
	}

	if rabbitURL := os.Getenv("RABBITMQ_URL"); rabbitURL != "" {
		interp.InstallProvider(NewRabbitProvider(rabbitURL))
	}

	if os.Getenv("AWS_REGION") != "" {
		if sp := NewSQSProvider(); sp != nil {
			interp.InstallProvider(sp)
		}
	}

	if brokers := os.Getenv("KAFKA_BROKERS"); brokers != "" {
		interp.InstallProvider(NewKafkaProvider(brokers))
	}
}
