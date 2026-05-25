package stdlib

import (
	"context"
	"fmt"
	"time"

	"github.com/adriangitvitz/yoru/interpreter"
	"github.com/redis/go-redis/v9"
)

// RedisProvider wraps a go-redis client. Methods stay thin around Redis
// primitives (lists, streams, pub/sub) instead of presenting a queue facade.
// Failures surface as Result.Err{kind: "redis_failed"}.
type RedisProvider struct {
	Client *redis.Client
	// BlockTimeout bounds blocking ops (BRPop, XRead); 0 = forever. Default
	// 30s so a misconfigured queue cannot silently freeze the caller.
	BlockTimeout time.Duration
}

// NewRedisProvider parses a redis:// or rediss:// URL; nil on parse failure.
func NewRedisProvider(redisURL string) *RedisProvider {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil
	}
	return &RedisProvider{
		Client:       redis.NewClient(opts),
		BlockTimeout: 30 * time.Second,
	}
}

func (p *RedisProvider) EffectName() string { return "Redis" }

func (p *RedisProvider) Methods() map[string]interpreter.Value {
	ctx := context.Background()

	return map[string]interpreter.Value{
		// ---- List operations (the simplest queue pattern) ----

		// lpush(key, value) -> Int  (returns new list length)
		"lpush": &interpreter.BuiltinVal{Name: "Redis.lpush", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			key, val, err := twoStrings("Redis.lpush", args)
			if err != nil {
				return redisErr("redis_bad_args", err.Error()), nil
			}
			n, err := p.Client.LPush(ctx, key, val).Result()
			if err != nil {
				return redisErr("redis_failed", err.Error()), nil
			}
			return &interpreter.IntVal{V: n}, nil
		}},

		// rpush(key, value) -> Int
		"rpush": &interpreter.BuiltinVal{Name: "Redis.rpush", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			key, val, err := twoStrings("Redis.rpush", args)
			if err != nil {
				return redisErr("redis_bad_args", err.Error()), nil
			}
			n, err := p.Client.RPush(ctx, key, val).Result()
			if err != nil {
				return redisErr("redis_failed", err.Error()), nil
			}
			return &interpreter.IntVal{V: n}, nil
		}},

		// lpop(key) -> String  (empty string if no element)
		"lpop": &interpreter.BuiltinVal{Name: "Redis.lpop", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 1 {
				return redisErr("redis_bad_args", "Redis.lpop(key) takes 1 argument"), nil
			}
			key, ok := args[0].(*interpreter.StringVal)
			if !ok {
				return redisErr("redis_bad_args", "key must be a String"), nil
			}
			val, err := p.Client.LPop(ctx, key.V).Result()
			if err == redis.Nil {
				return &interpreter.StringVal{V: ""}, nil
			}
			if err != nil {
				return redisErr("redis_failed", err.Error()), nil
			}
			return &interpreter.StringVal{V: val}, nil
		}},

		// brpop(key) -> String  (blocks up to BlockTimeout; empty string on timeout)
		"brpop": &interpreter.BuiltinVal{Name: "Redis.brpop", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) < 1 {
				return redisErr("redis_bad_args", "Redis.brpop(key) takes at least 1 argument"), nil
			}
			key, ok := args[0].(*interpreter.StringVal)
			if !ok {
				return redisErr("redis_bad_args", "key must be a String"), nil
			}
			res, err := p.Client.BRPop(ctx, p.BlockTimeout, key.V).Result()
			if err == redis.Nil {
				return &interpreter.StringVal{V: ""}, nil
			}
			if err != nil {
				return redisErr("redis_failed", err.Error()), nil
			}
			if len(res) < 2 {
				return &interpreter.StringVal{V: ""}, nil
			}
			return &interpreter.StringVal{V: res[1]}, nil
		}},

		// llen(key) -> Int
		"llen": &interpreter.BuiltinVal{Name: "Redis.llen", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 1 {
				return redisErr("redis_bad_args", "Redis.llen(key) takes 1 argument"), nil
			}
			key, ok := args[0].(*interpreter.StringVal)
			if !ok {
				return redisErr("redis_bad_args", "key must be a String"), nil
			}
			n, err := p.Client.LLen(ctx, key.V).Result()
			if err != nil {
				return redisErr("redis_failed", err.Error()), nil
			}
			return &interpreter.IntVal{V: n}, nil
		}},

		// ---- Key/value operations ----

		// set(key, value) -> String
		"set": &interpreter.BuiltinVal{Name: "Redis.set", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			key, val, err := twoStrings("Redis.set", args)
			if err != nil {
				return redisErr("redis_bad_args", err.Error()), nil
			}
			if err := p.Client.Set(ctx, key, val, 0).Err(); err != nil {
				return redisErr("redis_failed", err.Error()), nil
			}
			return &interpreter.StringVal{V: "OK"}, nil
		}},

		// get(key) -> String  (empty string if key missing)
		"get": &interpreter.BuiltinVal{Name: "Redis.get", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 1 {
				return redisErr("redis_bad_args", "Redis.get(key) takes 1 argument"), nil
			}
			key, ok := args[0].(*interpreter.StringVal)
			if !ok {
				return redisErr("redis_bad_args", "key must be a String"), nil
			}
			val, err := p.Client.Get(ctx, key.V).Result()
			if err == redis.Nil {
				return &interpreter.StringVal{V: ""}, nil
			}
			if err != nil {
				return redisErr("redis_failed", err.Error()), nil
			}
			return &interpreter.StringVal{V: val}, nil
		}},

		// del(key) -> Int  (number of keys removed)
		"del": &interpreter.BuiltinVal{Name: "Redis.del", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 1 {
				return redisErr("redis_bad_args", "Redis.del(key) takes 1 argument"), nil
			}
			key, ok := args[0].(*interpreter.StringVal)
			if !ok {
				return redisErr("redis_bad_args", "key must be a String"), nil
			}
			n, err := p.Client.Del(ctx, key.V).Result()
			if err != nil {
				return redisErr("redis_failed", err.Error()), nil
			}
			return &interpreter.IntVal{V: n}, nil
		}},

		// ---- Pub/Sub ----

		// publish(channel, payload) -> Int  (number of subscribers receiving)
		"publish": &interpreter.BuiltinVal{Name: "Redis.publish", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			ch, payload, err := twoStrings("Redis.publish", args)
			if err != nil {
				return redisErr("redis_bad_args", err.Error()), nil
			}
			n, err := p.Client.Publish(ctx, ch, payload).Result()
			if err != nil {
				return redisErr("redis_failed", err.Error()), nil
			}
			return &interpreter.IntVal{V: n}, nil
		}},

		// ---- Streams (Redis 5+) ----

		// xadd(key, payload) -> String. Single-field stream entry under
		// "payload". Multi-field streams require the Go API directly.
		"xadd": &interpreter.BuiltinVal{Name: "Redis.xadd", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			key, payload, err := twoStrings("Redis.xadd", args)
			if err != nil {
				return redisErr("redis_bad_args", err.Error()), nil
			}
			id, err := p.Client.XAdd(ctx, &redis.XAddArgs{
				Stream: key,
				Values: map[string]any{"payload": payload},
			}).Result()
			if err != nil {
				return redisErr("redis_failed", err.Error()), nil
			}
			return &interpreter.StringVal{V: id}, nil
		}},

		// xread_next(key, last_id) -> Object{id, payload}
		// Blocks up to BlockTimeout. Pass "$" as last_id to start at the tail.
		"xread_next": &interpreter.BuiltinVal{Name: "Redis.xread_next", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			key, lastID, err := twoStrings("Redis.xread_next", args)
			if err != nil {
				return redisErr("redis_bad_args", err.Error()), nil
			}
			streams, err := p.Client.XRead(ctx, &redis.XReadArgs{
				Streams: []string{key, lastID},
				Count:   1,
				Block:   p.BlockTimeout,
			}).Result()
			if err == redis.Nil {
				return &interpreter.ObjectVal{TypeName: "RedisMessage", Fields: map[string]interpreter.Value{
					"id":      &interpreter.StringVal{V: ""},
					"payload": &interpreter.StringVal{V: ""},
				}}, nil
			}
			if err != nil {
				return redisErr("redis_failed", err.Error()), nil
			}
			if len(streams) == 0 || len(streams[0].Messages) == 0 {
				return &interpreter.ObjectVal{TypeName: "RedisMessage", Fields: map[string]interpreter.Value{
					"id":      &interpreter.StringVal{V: ""},
					"payload": &interpreter.StringVal{V: ""},
				}}, nil
			}
			msg := streams[0].Messages[0]
			payload := ""
			if v, ok := msg.Values["payload"]; ok {
				if s, ok := v.(string); ok {
					payload = s
				}
			}
			return &interpreter.ObjectVal{
				TypeName: "RedisMessage",
				Fields: map[string]interpreter.Value{
					"id":      &interpreter.StringVal{V: msg.ID},
					"payload": &interpreter.StringVal{V: payload},
				},
			}, nil
		}},

		// ping() -> String  ("PONG" on success)
		"ping": &interpreter.BuiltinVal{Name: "Redis.ping", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			res, err := p.Client.Ping(ctx).Result()
			if err != nil {
				return redisErr("redis_failed", err.Error()), nil
			}
			return &interpreter.StringVal{V: res}, nil
		}},
	}
}

// twoStrings extracts two String args, used by the common (key, value) shape.
func twoStrings(fnName string, args []interpreter.Value) (string, string, error) {
	if len(args) != 2 {
		return "", "", fmt.Errorf("%s takes 2 arguments, got %d", fnName, len(args))
	}
	a, ok := args[0].(*interpreter.StringVal)
	if !ok {
		return "", "", fmt.Errorf("%s: first argument must be a String", fnName)
	}
	b, ok := args[1].(*interpreter.StringVal)
	if !ok {
		return "", "", fmt.Errorf("%s: second argument must be a String", fnName)
	}
	return a.V, b.V, nil
}

// redisErr builds a Result.Err with structured kind/message.
func redisErr(kind, message string) interpreter.Value {
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
