package web

import (
	"encoding/json"

	"github.com/leikonga/doofus-rick/internal/tracer"
	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"
)

func debugLayout(content g.Node) g.Node {
	return rootLayout(QuotesPageProps{Title: "debug - doofus-rick"}, content)
}

func DebugListPage(entries []*tracer.Entry) g.Node {
	return debugLayout(
		g.Group([]g.Node{
			H2(g.Text("trace timeline")),
			P(g.Textf("%d entries (only failures are persisted, rest is in-memory)", len(entries))),
			Figure(
				g.Attr("role", "region"),
				g.Attr("aria-label", "trace timeline"),
				g.Attr("style", "overflow-x:auto"),
				Table(
					THead(Tr(
						Th(g.Text("time")),
						Th(g.Text("channel")),
						Th(g.Text("user")),
						Th(g.Text("status")),
						Th(g.Text("tokens in/out")),
						Th(g.Text("ms")),
						Th(g.Text("")),
					)),
					TBody(g.Map(entries, debugTraceRow)),
				),
			),
		}),
	)
}

func debugTraceRow(e *tracer.Entry) g.Node {
	var statusText, statusColor string
	switch {
	case e.Err != "":
		statusText = "error"
		statusColor = "color:var(--pico-color-red-500)"
	case e.Decline:
		statusText = "decline"
		statusColor = "color:var(--pico-color-amber-500)"
	default:
		statusText = "ok"
		statusColor = "color:var(--pico-color-green-500)"
	}

	return Tr(
		Td(g.Text(e.At.Format("01-02 15:04:05"))),
		Td(Code(g.Text(e.ChannelID))),
		Td(Code(g.Text(e.UserID))),
		Td(Span(g.Attr("style", statusColor), g.Text(statusText))),
		Td(g.Textf("%d/%d", e.InputTokens, e.OutputTokens)),
		Td(g.Textf("%d", e.LatencyMS)),
		Td(A(Href("/debug/trace/"+e.ID), g.Text("view"))),
	)
}

func DebugTracePage(e *tracer.Entry) g.Node {
	var statusText, statusColor string
	switch {
	case e.Err != "":
		statusText = "error"
		statusColor = "color:var(--pico-color-red-500)"
	case e.Decline:
		statusText = "decline"
		statusColor = "color:var(--pico-color-amber-500)"
	default:
		statusText = "ok"
		statusColor = "color:var(--pico-color-green-500)"
	}

	return debugLayout(
		g.Group([]g.Node{
			HGroup(
				H2(g.Textf("trace %s", e.ID)),
				P(g.Text(e.At.Format("2006-01-02 15:04:05"))),
			),
			Table(
				TBody(
					Tr(Th(g.Text("status")), Td(Span(g.Attr("style", statusColor), g.Text(statusText)))),
					Tr(Th(g.Text("channel")), Td(Code(g.Text(e.ChannelID)))),
					Tr(Th(g.Text("user")), Td(Code(g.Text(e.UserID)))),
					Tr(Th(g.Text("latency")), Td(g.Textf("%d ms", e.LatencyMS))),
					Tr(Th(g.Text("tokens")), Td(g.Textf("%d in / %d out", e.InputTokens, e.OutputTokens))),
				),
			),
			H3(g.Text("prompt")),
			Pre(Code(g.Text(e.Prompt))),
			g.If(len(e.Tools) > 0, g.Group([]g.Node{
				H3(g.Textf("tool calls (%d)", len(e.Tools))),
				g.Map(e.Tools, func(t tracer.ToolEvent) g.Node {
					label := t.Name
					if t.IsErr {
						label += " (error)"
					}
					return Details(
						Summary(g.Text(label)),
						H4(g.Text("input")),
						Pre(Code(g.Text(t.Input))),
						H4(g.Text("result")),
						Pre(Code(g.Text(t.Result))),
					)
				}),
			})),
			g.If(e.Response != "", g.Group([]g.Node{
				H3(g.Text("response")),
				Pre(Code(g.Text(e.Response))),
			})),
			g.If(e.Err != "", g.Group([]g.Node{
				H3(g.Text("error")),
				Pre(Code(g.Attr("style", "color:var(--pico-color-red-500)"), g.Text(e.Err))),
			})),
			g.If(len(e.Messages) > 0, g.Group([]g.Node{
				H3(g.Text("messages sent")),
				Details(
					Summary(g.Text("expand")),
					Pre(Code(g.Text(prettyMessages(e.Messages)))),
				),
			})),
			H3(g.Text("system prompt")),
			Details(
				Summary(g.Text("expand")),
				Pre(Code(g.Text(e.SystemPrompt))),
			),
		}),
	)
}

func prettyMessages(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(out)
}
