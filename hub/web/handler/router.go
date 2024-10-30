package handler

import (
	"github.com/a-h/templ"
	"github.com/frbrno/iot/hub/web/view"
	"github.com/frbrno/iot/hub/web/view/view_nodes"
	"github.com/frbrno/iot/hub/web/view/view_rustyfalcon"
	"github.com/frbrno/iot/lib/go/rpc"
	"github.com/frbrno/iot/lib/go/tempil"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
)

func Setup(app *fiber.App, rpc_rusty_falcon *rpc.Conn) {
	app.Get("/", HandleView)
	app.Get("/nodes", HandleViewNodes)
	app.Get("/rusty_falcon", HandleViewRustyFalcon)

	// app.Get("/sse", func(c fiber.Ctx) error {
	// 	c.Set("Content-Type", "text/event-stream")
	// 	c.Set("Cache-Control", "no-cache")
	// 	c.Set("Connection", "keep-alive")
	// 	c.Set("Transfer-Encoding", "chunked")

	// 	queries := c.Queries()
	// 	log.Printf("subs: %v", queries["suuuuuuubs"])
	// 	ctx, fn_cancel := context.WithCancel(context.Background())
	// 	cons, err := rpc_rusty_falcon.JetStream().OrderedConsumer(ctx, "rusty_falcon_all", jetstream.OrderedConsumerConfig{
	// 		MaxResetAttempts: 5,
	// 		DeliverPolicy:    jetstream.DeliverByStartSequencePolicy,
	// 		OptStartSeq:      10,
	// 	})
	// 	if err != nil {
	// 		fn_cancel()
	// 		return err
	// 	}

	// 	msg_sig := make(chan jetstream.Msg, 24)
	// 	cons.Consume(func(msg jetstream.Msg) {
	// 		select {
	// 		case msg_sig <- msg:
	// 			//default:
	// 		}
	// 	})

	// 	c.Context().SetBodyStreamWriter(fasthttp.StreamWriter(func(w *bufio.Writer) {
	// 		defer fn_cancel()
	// 		for {
	// 			msg := <-msg_sig
	// 			sb := strings.Builder{}
	// 			sb.WriteString(fmt.Sprintf("event: %s\n", "rusty_falcon_all"))
	// 			sb.WriteString(fmt.Sprintf("data: %v\n\n", fmt.Sprintf("<div>subject: %s, payload: %s</div>", msg.Subject(), string(msg.Data()))))

	// 			fmt.Fprint(w, sb.String()) //nolint:errcheck, revive // It is fine to ignore the error
	// 			if err := w.Flush(); err != nil {
	// 				// user visits other side
	// 				return
	// 			}
	// 			//TODO the only way knowing the web client disconnected is by writing to it e.g. w.Flush
	// 			//if there is no more msg coming from jetstream, hang here forever
	// 		}
	// 	}))

	// 	log.Printf("New SSE Request\n")
	// 	return nil
	// })

}

func HandleView(c fiber.Ctx) error {
	v := tempil.LoadVars(c).Set("Home")
	p := view.Index()
	l := view.Layout(v, p)

	handler := adaptor.HTTPHandler(templ.Handler(l))

	return handler(c)
}

func HandleViewRustyFalcon(c fiber.Ctx) error {
	v := tempil.LoadVars(c).Set("Control")

	p := view_rustyfalcon.Index()
	l := view_rustyfalcon.Layout(v, p)

	handler := adaptor.HTTPHandler(templ.Handler(l))

	return handler(c)
}

func HandleViewNodes(c fiber.Ctx) error {
	v := tempil.LoadVars(c).Set("Nodes")

	p := view_nodes.Index()
	l := view_nodes.Layout(v, p)

	handler := adaptor.HTTPHandler(templ.Handler(l))

	return handler(c)
}
