package main

import (
	"embed"
	"io/fs"
	"log"

	"github.com/frbrno/iot/node/goofy_hawk/web/handler"
	"github.com/frbrno/iot/node/goofy_hawk/web/view"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/static"
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

	handler.Setup(app)

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
