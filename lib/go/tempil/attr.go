package tempil

import (
	"strings"

	"github.com/a-h/templ"
)

func AttrFunc(fn func(a Attr)) templ.Attributes {
	a := Attr{}
	fn(a)
	return templ.Attributes(a)
}

type Attr map[string]any

func (a Attr) Func(fn func(a Attr)) templ.Attributes {
	fn(a)
	return templ.Attributes(a)
}

func (a Attr) HxGet(s string) Attr {
	a["hx-get"] = s
	return a
}

func (a Attr) HxPost(s string) Attr {
	a["hx-post"] = s
	return a
}

func (a Attr) HxTarget(s string) Attr {
	a["hx-target"] = "#" + s
	return a
}

func (a Attr) HxPushUrl(b bool) Attr {
	if b {
		a["hx-push-url"] = "true"
	}
	return a
}

func (a Attr) Class(class ...string) Attr {
	if class_set, ok := a["class"].(string); ok {
		a["class"] = class_set + " " + strings.Join(class, " ")
		return a
	}
	a["class"] = strings.Join(class, " ")
	return a
}

func (a Attr) ID(id string) Attr {
	a["id"] = id
	return a
}

func (a Attr) Templ() templ.Attributes {
	return templ.Attributes(a)
}
