package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"maunium.net/go/mautrix/event"
)

func TestDiscordSpoilerSpacing(t *testing.T) {
	for _, inlineLinks := range []bool{false, true} {
		for _, body := range []string{"like this", " like this", "like this ", " like this "} {
			input := "before ||" + body + "|| after"
			html := (&Portal{}).renderDiscordMarkdownOnlyHTML(input, inlineLinks)
			assert.Equal(t, "before <span data-mx-spoiler>"+body+"</span> after", html, input)
			outbound, _ := expressionTestPortal().parseMatrixHTML(&event.MessageEventContent{Format: event.FormatHTML, FormattedBody: html}, nil)
			assert.Equal(t, "before ||like this|| after", outbound, "bridge round trip keeps text hidden")
		}
	}
}

func TestDiscordSpoilerFormatting(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"|| **bold** ||", "<span data-mx-spoiler> <strong>bold</strong> </span>"},
		{"|| one || and ||two ||", "<span data-mx-spoiler> one </span> and <span data-mx-spoiler>two </span>"},
		{"|| café 🙂 ||", "<span data-mx-spoiler> café 🙂 </span>"},
		{"|| <unsafe> ||", "<span data-mx-spoiler> &lt;unsafe&gt; </span>"},
		{"`|| literal ||`", "<code>|| literal ||</code>"},
		{"```\n|| literal ||\n```", "<pre><code>|| literal ||\n</code></pre>"},
		{`\|\| literal \|\|`, "|| literal ||"},
		{`\|| literal ||`, "|| literal ||"},
		{"|| unmatched", "|| unmatched"},
		{"unmatched ||", "unmatched ||"},
		{"| single |", "| single |"},
	} {
		for _, inlineLinks := range []bool{false, true} {
			assert.Equal(t, tc.want, (&Portal{}).renderDiscordMarkdownOnlyHTML(tc.input, inlineLinks), tc.input)
		}
	}
}
