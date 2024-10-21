package handler

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/a-h/templ"
	"github.com/frbrno/iot/goofy_lib/rpc"
	"github.com/frbrno/iot/node/goofy_hawk/web/view"
	"github.com/frbrno/iot/node/goofy_hawk/web/view/view_base"
	"github.com/frbrno/iot/node/goofy_hawk/web/view/view_base_rustyfalcon"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/valyala/fasthttp"
)

func Setup(app *fiber.App, rpc_rusty_falcon *rpc.Conn) {
	app.Get("/", HandleViewBase)
	app.Get("/rusty_falcon", HandleViewBaseRustyFalcon)

	app.Get("/sse", func(c fiber.Ctx) error {
		c.Set("Content-Type", "text/event-stream")
		c.Set("Cache-Control", "no-cache")
		c.Set("Connection", "keep-alive")
		c.Set("Transfer-Encoding", "chunked")

		queries := c.Queries()
		log.Printf("subs: %v", queries["subs"])
		ctx, fn_cancel := context.WithCancel(context.Background())
		rpc_rusty_falcon.JetStream().DeleteConsumer(context.Background(), "rusty_falcon_all", "goofy_hawk_web")
		cons, err := rpc_rusty_falcon.JetStream().OrderedConsumer(ctx, "rusty_falcon_all", jetstream.OrderedConsumerConfig{
			MaxResetAttempts: 5,
		})
		if err != nil {
			fn_cancel()
			return err
		}

		msg_sig := make(chan jetstream.Msg, 24)
		cons.Consume(func(msg jetstream.Msg) {
			select {
			case msg_sig <- msg:
				//default:
			}
		})

		c.Context().SetBodyStreamWriter(fasthttp.StreamWriter(func(w *bufio.Writer) {
			defer fn_cancel()
			for {
				msg := <-msg_sig
				sb := strings.Builder{}
				sb.WriteString(fmt.Sprintf("event: %s\n", "rusty_falcon_all"))
				sb.WriteString(fmt.Sprintf("data: %v\n\n", fmt.Sprintf("<div>subject: %s, payload: %s</div>", msg.Subject(), string(msg.Data()))))

				fmt.Fprint(w, sb.String()) //nolint:errcheck, revive // It is fine to ignore the error
				if err := w.Flush(); err != nil {
					// user visits other side
					return
				}
				//TODO the only way knowing the web client disconnected is by writing to it e.g. w.Flush
				//if there is no more msg coming from jetstream, hang here forever
			}
		}))

		log.Printf("New SSE Request\n")
		return nil
	})

}

func HandleViewBase(c fiber.Ctx) error {
	v := view.LoadVars(c).Set("Home")
	p := view_base.Index()
	l := view_base.Layout(v, p)

	handler := adaptor.HTTPHandler(templ.Handler(l))

	return handler(c)
}

func HandleViewBaseRustyFalcon(c fiber.Ctx) error {
	v := view.LoadVars(c).Set("Control")

	p := view_base_rustyfalcon.Index()
	l := view_base_rustyfalcon.Layout(v, p)

	handler := adaptor.HTTPHandler(templ.Handler(l))

	return handler(c)
}
