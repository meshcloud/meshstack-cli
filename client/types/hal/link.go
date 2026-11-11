package hal

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"

	"github.com/meshcloud/meshstack-cli/client/http"
	"github.com/meshcloud/meshstack-cli/internal/json"
)

var ErrNoLink = errors.New("meshStack sent no link")

type Links map[string]Link

type Link struct {
	Href string `json:"href"`

	action LinkAction
}

type LinkAction func(ctx context.Context, method, href string, options ...http.RequestOption) (io.ReadCloser, error)

func NewLink(href string, action LinkAction) Link {
	return Link{Href: href, action: action}
}

func LinkUnmarshalers(action LinkAction) *json.Unmarshalers {
	return json.UnmarshalFromFunc(func(_ *jsontext.Decoder, l *Link) error {
		l.action = action
		return errors.ErrUnsupported
	})
}

func (ls Links) Get[R any](ctx context.Context, rel string, options ...http.RequestOption) (R, error) {
	return follow[R](ctx, ls, http.MethodGet, rel, options)
}

func (ls Links) Post[R any](ctx context.Context, rel string, payload any, options ...http.RequestOption) (R, error) {
	return follow[R](ctx, ls, http.MethodPost, rel, append(options, http.WithJsonBody(payload)))
}

func (ls Links) Delete(ctx context.Context, rel string, options ...http.RequestOption) error {
	_, err := follow[struct{}](ctx, ls, http.MethodDelete, rel, options)
	return err
}

func follow[R any](ctx context.Context, ls Links, method, rel string, options []http.RequestOption) (result R, _ error) {
	link, found := ls[rel]
	if !found || link.action == nil {
		return result, fmt.Errorf("%w %q", ErrNoLink, rel)
	}
	body, err := link.action(ctx, method, link.Href, options...)
	return http.UnmarshalJsonBody[R](body, err, LinkUnmarshalers(link.action))
}
