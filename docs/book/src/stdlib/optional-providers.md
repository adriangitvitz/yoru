# Optional providers

Message-bus providers are auto-installed when their environment variable
is present. They give you at-least-once delivery semantics with the
standard ack pattern.

## `Redis`

Set `REDIS_URL` (e.g. `redis://127.0.0.1:6379/0`) to enable.

| Function | Purpose |
|----------|---------|
| `Redis.get(key)` / `Redis.set(key, val)` / `Redis.del(key)` | String key/value. |
| `Redis.lpush(key, v)` / `Redis.rpush(key, v)` / `Redis.lpop(key)` | Lists. |
| `Redis.brpop(key, timeout)` | Blocking pop. |
| `Redis.llen(key)`           | List length. |
| `Redis.publish(channel, v)` | Pub/sub publish. |
| `Redis.xadd(stream, fields)` / `Redis.xread_next(stream, id)` | Streams. |
| `Redis.ping()`              | Health check. |

## `Rabbit`

Set `RABBITMQ_URL` (e.g. `amqp://user:pass@127.0.0.1:5672/`).

| Function | Purpose |
|----------|---------|
| `Rabbit.publish(queue, body)`      | Publish a message.                       |
| `Rabbit.consume(queue)`            | Get next `Delivery{body, ack_id}`.        |
| `Rabbit.ack(ack_id)`               | Acknowledge a delivery.                   |
| `Rabbit.queue_size(queue)`         | Approximate depth.                        |
| `Rabbit.ping()`                    | Health check.                             |

```yoru
let d = Rabbit.consume("orders")
match process(d.body) {
  Err(e) => Log.warn("retrying later: " + e.kind)   // do NOT ack
  _      => Rabbit.ack(d.ack_id)
}
```

## `SQS`

Set `AWS_REGION` plus standard AWS credentials. For LocalStack or other
emulators, also set `SQS_ENDPOINT_URL`.

**All `SQS.*` functions take the queue's full URL, not its name.**
Create the queue (or look it up) first to obtain the URL.

| Function | Purpose |
|----------|---------|
| `SQS.create_queue(name)`            | Create queue; returns its URL.            |
| `SQS.send_message(queue_url, body)` | Enqueue.                                  |
| `SQS.receive_message(queue_url)`    | Receive next `Delivery{body, ack_id}` (long-poll 5s). |
| `SQS.delete_message(queue_url, ack_id)` | Ack a delivery.                       |
| `SQS.queue_size(queue_url)`         | Approximate depth.                        |
| `SQS.ping()`                        | Health check.                             |

```yoru
let url = SQS.create_queue("orders")
SQS.send_message(url, "order-1234")
let d = SQS.receive_message(url)
SQS.delete_message(url, d.ack_id)
```

At-least-once delivery via visibility timeout — if you don't
`delete_message` before the timeout fires, the message reappears.

## `Kafka`

Set `KAFKA_BROKERS` (comma-separated `host:port`). Uses a pure-Go client
(no cgo).

| Function | Purpose |
|----------|---------|
| `Kafka.write_message(topic, key, value)` | Produce.                            |
| `Kafka.read_message(topic, group)`       | Consume next `Delivery{body, key, ack_id}`. |
| `Kafka.commit(ack_id)`                   | Commit the offset.                  |
| `Kafka.create_topic(topic, partitions)`  | Create a topic.                     |
| `Kafka.ping()`                           | Health check.                       |

Manual commit gives you at-least-once. For exactly-once, store the
processing receipt next to the data write in the same transaction.

## When to use which

| Need | Reach for |
|------|-----------|
| Tiny cache, ephemeral state, rate-limit buckets | `Redis` |
| Transactional outbox, traditional work queue, RPC over messaging | `Rabbit` |
| AWS-native deployment with simple semantics | `SQS` |
| High-throughput event streaming, replay | `Kafka` |
