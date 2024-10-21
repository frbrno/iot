package main

import (
	"embed"
	"io/fs"
	"log"
	"time"

	"github.com/frbrno/iot/goofy_lib/rpc"
	"github.com/frbrno/iot/node/goofy_hawk/web/handler"
	"github.com/frbrno/iot/node/goofy_hawk/web/view"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/static"
	"github.com/nats-io/nats.go"
)

func main() {
	app := fiber.New(fiber.Config{
		// Setting centralized error hanling.
		//ErrorHandler: handlers.CustomErrorHandler,
		StreamRequestBody: true,
	})

	app.Get("/static*", static.New("", static.Config{
		FS:       static_dir_stripped(),
		Browse:   true,
		Compress: true,
	}))

	app.Use(logger.New())
	app.Use(func(c fiber.Ctx) error {
		view.LoadVars(c)
		return c.Next()
	})

	o := nats.GetDefaultOptions()
	o.Name = "goofy_hawk"
	o.MaxReconnect = -1
	o.PingInterval = time.Second * 10
	o.Url = "192.168.10.124:4222"
	nc, err := o.Connect()
	if err != nil {
		log.Fatal(err)
	}
	defer nc.Close()

	rpc_rusty_falcon, err := rpc.Dial(nc, o.Name, "rusty_falcon")
	if err != nil {
		log.Fatal(err)
	}
	defer rpc_rusty_falcon.Close()

	handler.Setup(app, rpc_rusty_falcon)

	log.Fatal(app.Listen(":3000", fiber.ListenConfig{DisableStartupMessage: true}))
}

//go:embed web/static
var static_dir embed.FS

func static_dir_stripped() fs.FS {
	static_dir_stripped, err := fs.Sub(static_dir, "web/static")
	if err != nil {
		log.Fatal(err)
	}
	return static_dir_stripped
}
