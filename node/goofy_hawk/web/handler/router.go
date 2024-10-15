package handler

import (
	"bufio"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/frbrno/iot/node/goofy_hawk/web/view"
	"github.com/frbrno/iot/node/goofy_hawk/web/view/view_base"
	"github.com/frbrno/iot/node/goofy_hawk/web/view/view_base_control"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/nats-io/nats.go"
	"github.com/valyala/fasthttp"
)

func Setup(app *fiber.App) {
	app.Get("/", HandleViewBase)
	app.Get("/control", HandleViewBaseControl)
	app.Post("/api/node/rusty_falcon/update_board", HandlePostControlUpdateBoard)

	app.Get("/api/node/rusty_falcon/firmware.bin", func(c fiber.Ctx) error {
		return c.SendFile("/tmp/rusty_falcon_firmware.bin", fiber.SendFile{
			Compress:  false,
			ByteRange: true,
			Download:  true,
		})
	})

	app.Get("/sse", func(c fiber.Ctx) error {
		c.Set("Content-Type", "text/event-stream")
		c.Set("Cache-Control", "no-cache")
		c.Set("Connection", "keep-alive")
		c.Set("Transfer-Encoding", "chunked")

		queries := c.Queries()
		log.Printf("subs: %v", queries["subs"])

		c.Context().SetBodyStreamWriter(fasthttp.StreamWriter(func(w *bufio.Writer) {
			for {
				select {
				case <-time.After(time.Second):
					sb := strings.Builder{}
					sb.WriteString(fmt.Sprintf("event: %s\n", "EventName"))
					sb.WriteString(fmt.Sprintf("data: %v\n\n", "<div>Content to swap into your HTML page.</div>"))

					fmt.Fprint(w, sb.String()) //nolint:errcheck, revive // It is fine to ignore the error
					if err := w.Flush(); err != nil {
						// user visits other side
						return
					}
				}
			}
		}))

		log.Printf("New SSE Request\n")
		return nil
	})

}

func HandlePostControlUpdateBoard(c fiber.Ctx) error {
	nc, err := nats.Connect("192.168.10.124:4222")
	if err != nil {
		return err
	}
	defer nc.Close()

	time.Sleep(time.Second * 2)
	token := time.Now().Unix()
	err = nc.Publish(fmt.Sprintf("goofy_hawk.rusty_falcon.update_board.run.%v", token), []byte(`{"url":"http://192.168.10.130:3000/api/node/rusty_falcon/firmware.bin"}`))
	if err != nil {
		return err
	}
	return nil
}

func HandleViewBase(c fiber.Ctx) error {
	v := view.LoadVars(c).Set("Home")
	p := view_base.Index()
	l := view_base.Layout(v, p)

	handler := adaptor.HTTPHandler(templ.Handler(l))

	return handler(c)
}

func HandleViewBaseControl(c fiber.Ctx) error {
	v := view.LoadVars(c).Set("Control")

	p := view_base_control.Index()
	l := view_base_control.Layout(v, p)

	handler := adaptor.HTTPHandler(templ.Handler(l))

	return handler(c)
}
