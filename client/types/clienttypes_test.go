package types

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsSet(t *testing.T) {
	type (
		someStruct struct {
			A string
		}
		someString string
		someSet    Set[someString]
	)
	tests := []struct {
		name string
		t    reflect.Type
		want bool
	}{
		{"bool", reflect.TypeFor[bool](), false},
		{"any", reflect.TypeFor[any](), false},
		{"int", reflect.TypeFor[any](), false},
		{"some set (not supported)", reflect.TypeFor[someSet](), false},
		{"set of string", reflect.TypeFor[Set[string]](), true},
		{"set of int", reflect.TypeFor[Set[string]](), true},
		{"set of struct", reflect.TypeFor[Set[someStruct]](), true},
		{"set of some string", reflect.TypeFor[Set[someString]](), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, IsSet(tt.t), "IsSet(%v)", tt.t)
		})
	}
}
