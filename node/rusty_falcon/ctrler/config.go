package main

import "github.com/frbrno/iot/node"

var cfg = node.Config{
	Nodes: map[string]any{"rusty_falcon_1": struct{ Value string }{}},
}

type Config struct {
	ID_Self string `yaml:"id_self" env:"ID_SELF" env-description:"mcu id"`
}
