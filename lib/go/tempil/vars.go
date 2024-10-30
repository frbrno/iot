package tempil

import (
	"strings"

	"github.com/gofiber/fiber/v3"
)

var vars_key = struct{}{}

type Vars struct {
	HxRequest bool
	HxTarget  string
	Title     string
}

func (v *Vars) Set(title string) *Vars {
	v.Title = title
	return v
}

func (v *Vars) HxTargetHasPrefix(target string) bool {
	return strings.HasPrefix(v.HxTarget, target)
}

func (v *Vars) HxTargetEquals(target string) bool {
	return v.HxTarget == target
}

func LoadVars(c fiber.Ctx) *Vars {
	if v := c.Locals(vars_key); v != nil {
		return v.(*Vars)
	}
	v := new(Vars)
	if c.Get("HX-Request") != "" || c.Get("HX-Request") == "true" {
		v.HxRequest = true
		v.HxTarget = c.Get("HX-Target")
	}
	c.Locals(vars_key, v)

	return v
}
