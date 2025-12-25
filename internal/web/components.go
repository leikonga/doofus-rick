package web

import (
	"fmt"

	. "maragu.dev/gomponents"
	hx "maragu.dev/gomponents-htmx"
	. "maragu.dev/gomponents/components"
	. "maragu.dev/gomponents/html"
)

type QuotesPageProps struct {
	Title       string
	Description string
	HeadExtra   []Node
}

func rootLayout(props QuotesPageProps, content Node) Node {
	headNodes := []Node{
		Script(Src("https://unpkg.com/htmx.org@2.0.0")),
		Script(Src("https://cdn.tailwindcss.com")),
	}
	if len(props.HeadExtra) > 0 {
		headNodes = append(headNodes, props.HeadExtra...)
	}
	if props.Title == "" {
		props.Title = "doofus-rick"
	}
	if props.Description == "" {
		props.Description = "because it can't be worse than this"
	}

	return HTML5(HTML5Props{
		Title:       props.Title,
		Description: props.Description,
		Language:    "en",
		Head:        headNodes,
		Body: []Node{
			Div(Class("py-4 px-2"),
				H1(Class("text-xl font-bold"), Text("doofus-rick")),
				Div(ID("main-content"),
					content,
				),
			),
		},
	})
}

func QuotesLayout(props QuotesPageProps, quotes []QuoteDisplay) Node {
	return rootLayout(props, QuoteList(quotes))
}

func QuoteSingleLayout(props QuotesPageProps, quote QuoteDisplay) Node {
	return rootLayout(props, QuoteCard(quote))
}

func QuoteList(quotes []QuoteDisplay) Node {
	return Div(Class("p-6 space-y-4"),
		Search(
			Input(
				Type("text"),
				Name("q"),
				Attr("placeholder", "Search quotes..."),
				Class("w-full p-2 border border-gray-300 rounded"),
				hx.Get("/search"),
				hx.Trigger("input changed delay:300ms"),
				hx.Target("#quote-results"),
				Attr("hx-include", "this"),
			),
		),
		Div(ID("quote-results"), Class("flex flex-col gap-4"),
			QuoteResults(quotes),
		),
	)
}

func QuoteResults(quotes []QuoteDisplay) Node {
	if len(quotes) == 0 {
		return P(Text("No quotes found."))
	}

	return Map(quotes, func(q QuoteDisplay) Node {
		return QuoteCard(q)
	})
}

func QuoteCard(quote QuoteDisplay) Node {
	return A(
		Href(fmt.Sprintf("/quote/%d", quote.ID)),
		Class("block p-4 bg-gray-200 rounded border border-gray-400 hover:bg-gray-300 transition-colors"),
		P(Class("text-lg italic"), Text(quote.Content)),
		Div(Class("mt-2 text-sm text-gray-400"),
			Textf("Added on %s by %s", quote.Timestamp.Format("Jan 02, 2006"), quote.CreatorName),
		),
	)
}
