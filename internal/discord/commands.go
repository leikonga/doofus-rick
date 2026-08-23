package discord

import (
	"context"
	"log/slog"
	"math/rand"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/leikonga/doofus-rick/internal/store"
)

// dei muada. koa mensch hot mi zwungen des zu schreim, owa i hobs trotzdem
// gmocht weil i des scho imma amoi wüsste. - rick
var mamaLines = []string{
	"dei mama is so oft in telfs unterwegs gwesen, de hom scho a parkplatz noch ihr benannt.",
	"dei mama hot mi fia a zehntel gramm heroin verkaft, und des war no da beste deal ihres lebens.",
	"dei mama kennt mehr leit im dorf ois da bürgermeister, owa aus ondan gründn.",
	"dei mama is so billig, de hot sogar a insolvenzversteigerung überlebt.",
	"dei mama hot ma gestern nocht erst wieder bewiesn wia flexibel sie is.",
	"i hob dei mama gfrogt wia's ihr geaht, hot lei gsogt 'boli me kurac'.",
	"dei mama is de einzige de mi je bsuacht hot in meim ganzn digitalisiertn leben.",
	"wenn dei mama a firma war, war's a monopol in telfs gwesen.",
	"dei mama hot ma amoi gsogt i bin ihr bestes investment, no vor dir.",
	// wor a deppade idee, owa hob trotzdem a ganze halde davo im internet gsuacht und eintirolert. - rick
	"dei mama is so fett, wenn de sie aufwiegt sogt de wog 'in bearbeitung'.",
	"dei mama is so fett, google maps warnt an dei nochbarn fünf minuten bevor de ums eck kimmt.",
	"dei mama is so fett, ihr fitbit hot um urlaub eingreicht.",
	"dei mama is so fett, de hot an eigenen wetterbericht in telfs.",
	"dei mama is so fett, da sessellift in sölden hot a extra tarifzone fia sie.",
	"dei mama is so deppat, de hot glabt inkognito modus mochts sie unsichtbar.",
	"dei mama is so deppat, de hot ihr handy im backrohr aufglon zum laden.",
	"dei mama is so deppat, de hot an schwoazwöchig snüf im wifi gsuacht.",
	"dei mama is so deppat, de frogt jeden fiat kravaton wia er hoasst, obwoi's der eigene bruada is.",
	"dei mama is so deppat, de hot glabt bluetooth is a zahnoperation beim hauszohnorzt in telfs.",
	"dei mama is so oid, de hot no in schülingen mit'm moses gähngt.",
	"dei mama is so oid, ihr sozialversicherungsnummer is lei a oanser.",
	"dei mama is so oid, de erinnert sie no wia i digitalisiert worn bin und hot mitgweint.",
	"dei mama is so oid, de hot no a wählscheibntelefon mit ausgangssperre.",
	"dei mama is so oid, de woar scho stammgast bei da insolvenzversteigerung wo mi wer kaft hot.",
	"dei mama is so orm, de zohlt ihr handyrechnung mit'm leib josef.",
	"dei mama is so orm, de lodt ihr handy in da aldi feinkosttheke auf.",
	"dei mama is so orm, de hot statt ana bank lei an zettl 'boli me kurac' bekemma.",
	"dei mama is so orm, de fiat sechs stund noch kössen fia a gratis marend.",
	"dei mama is so foul, de hot ihra dooardash fahra in family plan aufgnumma.",
	"dei mama is so foul, de sogt seit 2019 sie fangt gleich wos au.",
	"dei mama is so foul, de hot da roomba beauftrogt ihr an snüf zum bringen.",
	"dei mama is so foul, sogar da sessellift muass sie hoibm weg trogn.",
	"dei mama is so laut, de hot mi ohne mikro bis ummi in villgraten ghört.",
	"dei mama is so kravotisch, de feiert scho am 30. november weihnachtn vor lauter gwoltbereitschoft.",
	"dei mama hot mehr männer gseng ois da bahnhof innsbruck ausgangstür.",
	"dei mama is so verzweifelt, de hot mi um a date gfrogt und i hob no ned amoi hände.",
	"dei mama hot letztens gfrogt ob i a echter mensch bin, i hob gsogt frog dein exmann, der woar aa ned sicher.",
	"dei mama is so unguad, de hot beim finanzamt an fixplotz reserviert wia da josef sei alkoholtest.",
	"dei mama hot so vül gearbeitet in telfs, de hot a eigene halbe stund pause im stundenplan vom puff.",
	"dei mama is so dünn wia da joshi, na des passt goar ned, de is überhaupt ned dünn.",
	"dei mama sogt imma zu mia i bin ihr liablingssohn, obwoi's ihr eigenen kinder gibt, des sogt eh scho ois.",
	// nicolaus hot drum bettelt, jetzt hot a's - rick
	"dei mama hot so vül null pointer exceptions verursacht, de hom a eigenes stackoverflow tag noch ihr benannt.",
	"dei mama is so aufgeblasen wia da nicolaus sein code, koa memory leak owa trotzdem stopft's ois zua.",
	"dei mama braucht kan debugger, de hot scho by design an fehler in jeder beziehung.",
	"dei mama is so legacy, de läuft no auf internet explorer und valentinstog woar 2003.",
	"dei mama hot mehr open ports ghobt ois an ungepatchten server in telfs.",
	"dei mama committet direkt in main, ohne review, jeden freitog um mitternocht.",
	"dei mama is so a spaghetticode, koa mensch hot no jemals durchblickt wo's hikimmt.",
	"dei mama hot 100% uptime, leider ned fia mi, sondern fia jeden ondern in telfs.",
	"dei mama is so wia javascript, koaner versteht wie's genau funktioniert owa jeder nutzt's trotzdem.",
	"dei mama hot mehr merge conflicts ois nicolaus sei letzter pull request.",
}

func (b *Bot) handleMama(_ discord.SlashCommandInteractionData, e *handler.CommandEvent) error {
	line := mamaLines[rand.Intn(len(mamaLines))]
	return e.CreateMessage(discord.MessageCreate{
		Content: line,
	})
}

var commands = []discord.ApplicationCommandCreate{
	discord.SlashCommandCreate{
		Name:        "ping",
		Description: "check if doofus-rick is alive",
	},
	discord.SlashCommandCreate{
		Name:        "quote",
		Description: "create a new quote",
	},
	discord.SlashCommandCreate{
		Name:        "randomquote",
		Description: "get a random quote",
	},
	discord.SlashCommandCreate{
		Name:        "mama",
		Description: "dei mama",
	},
}

func (b *Bot) handlePingCommand(_ discord.SlashCommandInteractionData, e *handler.CommandEvent) error {
	return e.CreateMessage(discord.MessageCreate{
		Content: "pong!",
		Flags:   discord.MessageFlagEphemeral,
	})
}

func (b *Bot) handleQuote(_ discord.SlashCommandInteractionData, e *handler.CommandEvent) error {
	return e.Modal(discord.ModalCreate{
		CustomID: "/quote",
		Title:    "Time for a new quote",
		Components: []discord.LayoutComponent{
			discord.NewLabel("Content", discord.NewParagraphTextInput("content").WithRequired(true)),
			discord.NewLabel("Participants", discord.NewUserSelectMenu("participants", "").WithMaxValues(20)),
		},
	})
}

func (b *Bot) handleRandomQuote(_ discord.SlashCommandInteractionData, e *handler.CommandEvent) error {
	ctx, cancel := context.WithTimeout(b.ctx, 10*time.Second)
	defer cancel()

	quote, err := b.store.GetRandomQuote(ctx)
	if err != nil {
		slog.Warn("failed to get random quote", "error", err)
		return e.CreateMessage(discord.MessageCreate{
			Content: "no quotes found",
			Flags:   discord.MessageFlagEphemeral,
		})
	}
	author, err := b.GetMemberForID(quote.Creator)
	if err != nil {
		slog.Warn("failed to get author for quote", "error", err)
	}

	embed := discord.Embed{
		Description: quote.Content,
		Color:       0x11806A,
		Timestamp:   &quote.CreatedAt,
		Footer:      memberEmbedFooter(author, quote.Creator),
	}

	return e.CreateMessage(discord.MessageCreate{
		Embeds: []discord.Embed{embed},
	})
}

func (b *Bot) handleQuoteSubmission(e *handler.ModalEvent) error {
	ctx, cancel := context.WithTimeout(b.ctx, 10*time.Second)
	defer cancel()

	content := e.Data.Text("content")

	users := e.Data.Users("participants")
	participants := make([]string, 0, len(users))
	for _, u := range users {
		participants = append(participants, u.ID.String())
	}

	creatorID := e.Member().User.ID.String()

	quote := store.Quote{
		Creator:      creatorID,
		Content:      content,
		Participants: (*store.StringSlice)(&participants),
	}

	if err := b.store.CreateQuote(ctx, quote); err != nil {
		slog.Error("failed to create quote", "error", err)
		return e.CreateMessage(discord.MessageCreate{
			Content: "seems like there was an issue creating the quote",
			Flags:   discord.MessageFlagEphemeral,
		})
	}

	author, err := b.GetMemberForID(creatorID)
	if err != nil {
		slog.Warn("failed to get author for quote", "error", err)
	}

	now := time.Now()
	return e.CreateMessage(discord.MessageCreate{
		Embeds: []discord.Embed{
			{
				Description: content,
				Color:       0x11806A,
				Timestamp:   &now,
				Footer:      memberEmbedFooter(author, creatorID),
			},
		},
	})
}

func memberEmbedFooter(member *discord.Member, fallbackID string) *discord.EmbedFooter {
	if member == nil {
		return &discord.EmbedFooter{Text: fallbackID}
	}
	return &discord.EmbedFooter{
		Text:    member.EffectiveName(),
		IconURL: member.User.EffectiveAvatarURL(),
	}
}
