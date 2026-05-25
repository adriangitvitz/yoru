package stdlib

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/adriangitvitz/yoru/interpreter"
)

// CryptoProvider implements the Crypto effect.
type CryptoProvider struct{}

func (p *CryptoProvider) EffectName() string { return "Crypto" }

func (p *CryptoProvider) Methods() map[string]interpreter.Value {
	return map[string]interpreter.Value{
		"hmac_sha256": &interpreter.BuiltinVal{Name: "Crypto.hmac_sha256", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("Crypto.hmac_sha256() takes 2 arguments (key, data)")
			}
			key, ok := args[0].(*interpreter.StringVal)
			if !ok {
				return nil, fmt.Errorf("Crypto.hmac_sha256() key must be String")
			}
			data, ok := args[1].(*interpreter.StringVal)
			if !ok {
				return nil, fmt.Errorf("Crypto.hmac_sha256() data must be String")
			}
			mac := hmac.New(sha256.New, []byte(key.V))
			mac.Write([]byte(data.V))
			return &interpreter.StringVal{V: hex.EncodeToString(mac.Sum(nil))}, nil
		}},
		"sha256": &interpreter.BuiltinVal{Name: "Crypto.sha256", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("Crypto.sha256() takes 1 argument")
			}
			data, ok := args[0].(*interpreter.StringVal)
			if !ok {
				return nil, fmt.Errorf("Crypto.sha256() argument must be String")
			}
			h := sha256.Sum256([]byte(data.V))
			return &interpreter.StringVal{V: hex.EncodeToString(h[:])}, nil
		}},
		"base64url_encode": &interpreter.BuiltinVal{Name: "Crypto.base64url_encode", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("Crypto.base64url_encode() takes 1 argument")
			}
			data, ok := args[0].(*interpreter.StringVal)
			if !ok {
				return nil, fmt.Errorf("Crypto.base64url_encode() argument must be String")
			}
			encoded := base64.RawURLEncoding.EncodeToString([]byte(data.V))
			return &interpreter.StringVal{V: encoded}, nil
		}},
		"base64url_decode": &interpreter.BuiltinVal{Name: "Crypto.base64url_decode", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("Crypto.base64url_decode() takes 1 argument")
			}
			data, ok := args[0].(*interpreter.StringVal)
			if !ok {
				return nil, fmt.Errorf("Crypto.base64url_decode() argument must be String")
			}
			decoded, err := base64.RawURLEncoding.DecodeString(data.V)
			if err != nil {
				return &interpreter.StringVal{V: ""}, nil
			}
			return &interpreter.StringVal{V: string(decoded)}, nil
		}},
		"constant_time_eq": &interpreter.BuiltinVal{Name: "Crypto.constant_time_eq", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("Crypto.constant_time_eq() takes 2 arguments")
			}
			a, ok := args[0].(*interpreter.StringVal)
			if !ok {
				return nil, fmt.Errorf("Crypto.constant_time_eq() arguments must be Strings")
			}
			b, ok := args[1].(*interpreter.StringVal)
			if !ok {
				return nil, fmt.Errorf("Crypto.constant_time_eq() arguments must be Strings")
			}
			eq := subtle.ConstantTimeCompare([]byte(a.V), []byte(b.V)) == 1
			return &interpreter.BoolVal{V: eq}, nil
		}},
		"random_hex": &interpreter.BuiltinVal{Name: "Crypto.random_hex", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("Crypto.random_hex() takes 1 argument")
			}
			n, ok := args[0].(*interpreter.IntVal)
			if !ok {
				return nil, fmt.Errorf("Crypto.random_hex() argument must be Int")
			}
			buf := make([]byte, n.V)
			if _, err := rand.Read(buf); err != nil {
				return nil, fmt.Errorf("Crypto.random_hex() failed: %s", err)
			}
			return &interpreter.StringVal{V: hex.EncodeToString(buf)}, nil
		}},
	}
}
