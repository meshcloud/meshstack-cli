package variant

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/client/types/enum"
)

type shape string

const (
	circle enum.Entry[shape] = "circle"
	square enum.Entry[shape] = "square"
)

type shapeHolder struct {
	Circle *struct{}
	Square *struct{ Side int }
}

func (s shapeHolder) inferType() (enum.Entry[shape], error) {
	return InferType(
		NewCandidate(circle, s.Circle != nil),
		NewCandidate(square, s.Square != nil),
	)
}

func TestInferType(t *testing.T) {
	t.Run("an empty struct counts as set", func(t *testing.T) {
		got, err := shapeHolder{Circle: &struct{}{}}.inferType()
		require.NoError(t, err)
		assert.Equal(t, circle, got)
	})

	t.Run("one variant", func(t *testing.T) {
		got, err := shapeHolder{Square: &struct{ Side int }{Side: 2}}.inferType()
		require.NoError(t, err)
		assert.Equal(t, square, got)
	})

	t.Run("no variant", func(t *testing.T) {
		_, err := shapeHolder{}.inferType()
		require.EqualError(t, err, "no variant is set")
	})

	t.Run("several variants", func(t *testing.T) {
		_, err := shapeHolder{Circle: &struct{}{}, Square: &struct{ Side int }{}}.inferType()
		require.EqualError(t, err, "more than one variant is set: circle and square")
	})
}
