package main

import (
	"github.com/frbrno/iot/node"
	"github.com/gofiber/fiber/v3"
)

func main() {
	node.Init()

	// data := struct{ Value string }{}

	// var cfg = node.Config{}

	// id, err := node.ConfigLoad(&cfg)
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// log.Println("what", id)
	// log.Println(cfg.ID)
	// log.Println(data)
	// log.Println(cfg)
	// return

	app := fiber.New()

	app.Get("/", func(c fiber.Ctx) error {
		return c.SendString("Hello, World 69! ")
	})

	app.Listen(":8080", fiber.ListenConfig{
		DisableStartupMessage: true,
	})
}
