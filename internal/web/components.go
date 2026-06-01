package web

import (
	"fmt"

	g "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"
)

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

	return g.Map(quotes, func(q QuoteDisplay) g.Node {
		return QuoteCard(q)
	})
}

func QuoteCard(quote QuoteDisplay) g.Node {
	return A(Href(fmt.Sprintf("/quote/%d", quote.ID)), Class("quote-card"),
		Article(
			BlockQuote(
				g.Text(quote.Content),
				Footer(
					Cite(g.Textf("Added on %s by %s", quote.CreatedAt.Format("Jan 02, 2006"), quote.CreatorName)),
				),
			),
		),
	)
}
