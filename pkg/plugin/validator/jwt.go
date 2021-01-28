package validator

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/dgrijalva/jwt-go"
	"github.com/megaease/easegateway/pkg/context"
)

// JWTValidatorSpec defines the configuration of JWT validator
type JWTValidatorSpec struct {
	Algorithm string `yaml:"algorithm" jsonschema:"enum=HS256,enum=HS384,enum=HS512"`
	// Secret is in hex encoding
	Secret string `yaml:"secret" jsonschema:"required,pattern=^[A-Fa-f0-9]+$"`
}

// NewJWTValidator creates a new JWT validator
func NewJWTValidator(spec *JWTValidatorSpec) *JWTValidator {
	secret, _ := hex.DecodeString(spec.Secret)
	return &JWTValidator{Algorithm: spec.Algorithm, Secret: secret}
}

// JWTValidator defines the JWT validator
type JWTValidator struct {
	Algorithm string
	Secret    []byte
}

// Validate validates a hte authorization header of a http request
func (v *JWTValidator) Validate(req context.HTTPRequest) error {
	const prefix = "Bearer "

	authHdr := req.Header().Get("Authorization")
	if !strings.HasPrefix(authHdr, prefix) {
		return fmt.Errorf("Unexpected authrization header: %s", authHdr)
	}

	token := authHdr[len(prefix):]
	// jwt.Parse does everything incuding parsing and verification
	_, e := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
		if alg := token.Method.Alg(); alg != v.Algorithm {
			return nil, fmt.Errorf("Unexpected signing method: %v", alg)
		}
		return v.Secret, nil
	})

	return e
}