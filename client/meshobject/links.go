// Package meshobject holds the types that several meshObject kinds share. The generator places
// a schema here once more than one kind and version reaches it.
package meshobject

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Links are the HAL links of a meshObject by relation. HAL writes a relation as one link or as an
// array of them, and Links reads both as a slice.
//
//nolint:recvcheck // MarshalJSONTo takes a value, so that json.Marshal finds it on a field that is not addressable
type Links map[string][]Link

type Link struct {
	Href        string `json:"href"`
	Templated   bool   `json:"templated,omitzero"`
	Type        string `json:"type,omitzero"`
	Deprecation string `json:"deprecation,omitzero"`
	Profile     string `json:"profile,omitzero"`
	Name        string `json:"name,omitzero"`
	Title       string `json:"title,omitzero"`
	Hreflang    string `json:"hreflang,omitzero"`
}

func (l *Links) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	var relations map[string]jsontext.Value
	if err := json.UnmarshalDecode(dec, &relations); err != nil {
		return err
	}
	*l = make(Links, len(relations))
	for rel, value := range relations {
		var links []Link
		if value.Kind() != '[' {
			value = jsontext.Value("[" + string(value) + "]")
		}
		if err := json.Unmarshal(value, &links); err != nil {
			return fmt.Errorf("link %s: %w", rel, err)
		}
		(*l)[rel] = links
	}
	return nil
}

func (l Links) MarshalJSONTo(enc *jsontext.Encoder) error {
	relations := make(map[string]any, len(l))
	for rel, links := range l {
		if len(links) == 1 {
			relations[rel] = links[0]
		} else {
			relations[rel] = links
		}
	}
	return json.MarshalEncode(enc, relations, json.Deterministic(true))
}

// Href is the first link of the relation, with a template left unexpanded.
func (l Links) Href(rel string) (string, bool) {
	if len(l[rel]) == 0 {
		return "", false
	}
	return l[rel][0].Href, true
}

var expressionRe = regexp.MustCompile(`\{([^}]*)\}`)

// Expand fills the template of the relation's first link with vars, leaving out a variable that
// vars lacks. It knows the expressions meshStack writes, of RFC 6570: {var}, {+var}, {?var,...}
// and {&var,...}.
func (l Links) Expand(rel string, vars map[string]string) (string, error) {
	href, ok := l.Href(rel)
	if !ok {
		return "", fmt.Errorf("no link %s", rel)
	}
	if !l[rel][0].Templated {
		return href, nil
	}
	var err error
	expanded := expressionRe.ReplaceAllStringFunc(href, func(expression string) string {
		expression = strings.Trim(expression, "{}")
		operator := ""
		if expression != "" && strings.ContainsAny(expression[:1], "+?&#./;") {
			operator, expression = expression[:1], expression[1:]
		}
		var parts []string
		for name := range strings.SplitSeq(expression, ",") {
			value, set := vars[name]
			if !set {
				continue
			}
			switch operator {
			case "":
				parts = append(parts, url.PathEscape(value))
			case "+":
				parts = append(parts, value)
			case "?", "&":
				parts = append(parts, url.QueryEscape(name)+"="+url.QueryEscape(value))
			default:
				err = fmt.Errorf("link %s: the expression {%s%s} is not supported", rel, operator, expression)
			}
		}
		switch {
		case len(parts) == 0:
			return ""
		case operator == "?" || operator == "&":
			return operator + strings.Join(parts, "&")
		default:
			return strings.Join(parts, ",")
		}
	})
	return expanded, err
}
