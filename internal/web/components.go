package web

import (
	"bytes"
	"fmt"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	g "maragu.dev/gomponents"
	//nolint:staticcheck // ST1001: dot-import is the documented gomponents/html idiom, keeps the HTML DSL terse
	. "maragu.dev/gomponents/html"
)

var markdown = goldmark.New(goldmark.WithExtensions(extension.GFM, extension.Linkify))

type QuotesPageProps struct {
	Title       string
	Description string
	HeadExtra   []g.Node
}

func rootLayout(props QuotesPageProps, content g.Node) g.Node {
	if props.Title == "" {
		props.Title = "doofus-rick"
	}
	if props.Description == "" {
		props.Description = "because it can't be worse than this"
	}

	head := []g.Node{
		Meta(Charset("utf-8")),
		Meta(Name("viewport"), Content("width=device-width, initial-scale=1")),
		Meta(Name("description"), Content(props.Description)),
		TitleEl(g.Text(props.Title)),
		Link(Rel("stylesheet"), Href("/static/pico.min.css")),
		Link(Rel("stylesheet"), Href("/static/app.css")),
		Script(Src("/static/htmx.min.js"), Defer()),
	}
	head = append(head, props.HeadExtra...)

	return Doctype(
		HTML(Lang("en"),
			Head(head...),
			Body(
				Header(Class("container"),
					HGroup(
						H1(g.Text("doofus-rick")),
						P(g.Text(props.Description)),
					),
					Nav(
						Ul(
							Li(A(Href("/"), g.Text("quotes"))),
							Li(A(Href("/debug"), g.Text("debug"))),
						),
					),
				),
				Main(Class("container"),
					Div(ID("main-content"), content),
				),
			),
		),
	)
}

func QuotesLayout(props QuotesPageProps, quotes []QuoteDisplay) g.Node {
	return rootLayout(props, QuoteList(quotes))
}

func QuoteSingleLayout(props QuotesPageProps, quote QuoteDisplay) g.Node {
	return rootLayout(props, QuoteCard(quote))
}

func QuoteList(quotes []QuoteDisplay) g.Node {
	return g.Group([]g.Node{
		Search(
			Input(
				Type("search"),
				Name("q"),
				Placeholder("Search quotes..."),
				g.Attr("hx-get", "/search"),
				g.Attr("hx-trigger", "input changed delay:300ms"),
				g.Attr("hx-target", "#quote-results"),
				g.Attr("hx-include", "this"),
			),
		),
		Div(ID("quote-results"), Class("quote-list"),
			QuoteResults(quotes),
		),
	})
}

func QuoteResults(quotes []QuoteDisplay) g.Node {
	if len(quotes) == 0 {
		return P(g.Text("No quotes found."))
	}

	return g.Map(quotes, QuoteCard)
}

func renderMarkdown(src string) g.Node {
	var buf bytes.Buffer
	if err := markdown.Convert([]byte(src), &buf); err != nil {
		return g.Text(src)
	}
	return g.Raw(buf.String())
}

func QuoteCard(quote QuoteDisplay) g.Node {
	meta := []g.Node{
		g.Textf("Added on %s by ", quote.CreatedAt.Format("Jan 02, 2006")),
		A(Href("/user/"+quote.Creator), g.Text(quote.CreatorName)),
	}
	if len(quote.ParticipantNames) > 0 {
		meta = append(meta, g.Text(" · with "))
		for i, name := range quote.ParticipantNames {
			if i > 0 {
				meta = append(meta, g.Text(", "))
			}
			if quote.Participants != nil && i < len(*quote.Participants) {
				meta = append(meta, A(Href("/user/"+(*quote.Participants)[i]), g.Text(name)))
			} else {
				meta = append(meta, g.Text(name))
			}
		}
	}
	meta = append(meta, g.Text(" · "), A(Href(fmt.Sprintf("/quote/%d", quote.ID)), g.Text("permalink")))

	return Article(Class("quote-card"),
		BlockQuote(
			Div(Class("quote-content"), renderMarkdown(quote.Content)),
			Footer(Cite(meta...)),
		),
	)
}

func UserLayout(props QuotesPageProps, name string, quotes []QuoteDisplay) g.Node {
	props.Title = name + " · doofus-rick"
	return rootLayout(props, g.Group([]g.Node{
		H2(g.Textf("%d quotes with %s", len(quotes), name)),
		Div(Class("quote-list"), QuoteResults(quotes)),
	}))
}
