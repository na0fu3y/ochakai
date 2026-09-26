// `ochakai serve-chat`: answer questions asked in Google Chat with the
// deployment's own agent (design doc 0150). Same image as `serve`, deployed
// as its own Cloud Run service with `--args=serve-chat`, the way serve-ui
// is.
//
// It is a bridge, not a second entrance to the agent: it receives Chat's
// event, asks the ochakai server named by $OCHAKAI_URL through the one
// operation every face uses (POST /api/v1/agent, design doc 0145 §4), on
// behalf of the person who wrote the message, and hands the answer back
// as Chat's synchronous reply. Nothing here holds a secret:
//
//   - Chat's request carries a Google-signed ID token, checked against
//     Google's public keys here (and by Cloud Run, when the service is
//     private and only Chat may invoke it).
//   - The person is the event's user, whose email Google put there; the
//     bridge names them in Ochakai-On-Behalf-Of, which the server honours
//     only for callers it lists in OCHAKAI_DELEGATING_CALLERS (0065 §3).
//   - The server is reached with this service's own identity token, from
//     the metadata server, as serve-ui reaches it.
//   - The reply is the HTTP response: no Chat API call, so no token for one.
//
// What Chat cannot do is run a query as the person: there is no token of
// theirs here. A proposal is shown as SQL to run in the web UI, where it
// runs as them (0142 §4).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"google.golang.org/api/idtoken"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/httpauth"
)

// chatBudget is how long an answer may take. Chat waits 30 seconds for a
// synchronous reply; the rest is the bridge's own margin.
const chatBudget = 25 * time.Second

// chatCallers are the identities Google Chat calls an HTTP endpoint as:
// the Chat service for an interaction-event app, and a project's Google
// Workspace add-ons service agent for an app built as an add-on.
func chatCaller(email string) bool {
	return email == "chat@system.gserviceaccount.com" ||
		(strings.HasPrefix(email, "service-") && strings.HasSuffix(email, "@gcp-sa-gsuiteaddons.iam.gserviceaccount.com"))
}

func serveChat(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := config.CheckEnv(); err != nil {
		return err
	}
	target := os.Getenv("OCHAKAI_URL")
	if target == "" {
		return fmt.Errorf("OCHAKAI_URL is required: serve-chat asks the ochakai server there, on behalf of the person who wrote in Chat")
	}
	u, err := url.Parse(target)
	if err != nil {
		return fmt.Errorf("invalid OCHAKAI_URL %q: %w", target, err)
	}
	b := &chatBridge{
		agentURL: strings.TrimRight(u.String(), "/") + "/api/v1/agent",
		tokens:   &metadataTokenSource{audience: target},
		verify:   googleChatToken,
		log:      log,
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Info("serve-chat listening", "addr", ":"+port, "target", target, "version", version)
	return runServer(ctx, ":"+port, b)
}

// chatBridge is the handler. verify is how a request's token is checked,
// a field so tests can stand in for Google's keys.
type chatBridge struct {
	agentURL string
	tokens   serviceTokenSource
	verify   func(ctx context.Context, token, audience string) (email string, err error)
	log      *slog.Logger
}

// googleChatToken checks a Google-signed ID token for the audience Chat
// was configured with — the endpoint's own URL — and returns the email
// it was issued to.
func googleChatToken(ctx context.Context, token, audience string) (string, error) {
	p, err := idtoken.Validate(ctx, token, audience)
	if err != nil {
		return "", err
	}
	email, _ := p.Claims["email"].(string)
	if verified, _ := p.Claims["email_verified"].(bool); email == "" || !verified {
		return "", errors.New("the token names no verified email")
	}
	return email, nil
}

func (b *chatBridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "serve-chat takes Google Chat's POST", http.StatusMethodNotAllowed)
		return
	}
	// The audience is the URL Chat was told to call, which is the URL it
	// called: Cloud Run terminates TLS, so the scheme is https.
	audience := "https://" + r.Host + r.URL.Path
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	email, err := b.verify(r.Context(), token, audience)
	if err == nil && !chatCaller(email) {
		err = fmt.Errorf("%s is not Google Chat", email)
	}
	if err != nil {
		b.log.Warn("refusing a request Google Chat did not sign", "error", err)
		http.Error(w, "serve-chat answers Google Chat only", http.StatusUnauthorized)
		return
	}
	var ev chatEvent
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&ev); err != nil {
		http.Error(w, "the event is not JSON", http.StatusBadRequest)
		return
	}
	who, text, addOn, ok := ev.question()
	if !ok {
		// Not a person's message (a bot, a space event with nothing to
		// answer): an empty reply is Chat's "nothing to say".
		writeChatReply(w, addOn, "")
		return
	}
	writeChatReply(w, addOn, b.answer(r.Context(), who, text))
}

// answer asks the agent as the person and renders what came back.
func (b *chatBridge) answer(ctx context.Context, who, text string) string {
	if strings.TrimSpace(text) == "" {
		return "ochakai のエージェントです。データのナレッジについて訊いてください(例: 先月の売上はどう数える?)。"
	}
	ctx, cancel := context.WithTimeout(ctx, chatBudget)
	defer cancel()
	body, _ := json.Marshal(map[string]any{"messages": []map[string]string{{"role": "user", "text": text}}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.agentURL, bytes.NewReader(body))
	if err != nil {
		return "答えられませんでした: " + err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(httpauth.OnBehalfOfHeader, domain.ActorHuman+":"+who)
	v := version
	if v == "" {
		v = "dev"
	}
	req.Header.Set("Ochakai-Producer", "ochakai-chat/"+v)
	if tok, err := b.tokens.token(); err == nil {
		req.Header.Set("X-Serverless-Authorization", "Bearer "+tok)
	} else {
		b.log.Warn("no service identity token; asking without it", "error", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "考えるのに時間がかかっています。Web UI のエージェントで同じことを訊いてください — そこなら待てて、SQL もあなたの権限で走らせられます。"
	}
	if err != nil {
		return "答えられませんでした: " + err.Error()
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		b.log.Warn("the agent did not answer", "status", resp.StatusCode, "error", e.Error)
		// Each reply says whose move it is: the person's (wait, ask again)
		// or the operator's (a setting), and never blames the model for
		// a refusal that was the bridge's configuration.
		switch {
		case resp.StatusCode == http.StatusNotImplemented:
			return "このデプロイにはエージェントが入っていません(運用者が OCHAKAI_AGENT を設定すると使えます)。"
		case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized:
			return "この Chat の橋は ochakai に受け入れられていません。運用者に伝えてください: 橋のサービスアカウントに ochakai の invoker を与え、OCHAKAI_DELEGATING_CALLERS に入れる必要があります。"
		case resp.StatusCode >= 500:
			return "エージェントが答えられませんでした(モデルが混み合っているかもしれません)。少し待ってからもう一度訊いてください。"
		default:
			return "エージェントに問えませんでした: " + e.Error
		}
	}
	var ans struct {
		Text string   `json:"text"`
		Read []string `json:"read"`
		SQL  *struct {
			Query string `json:"query"`
		} `json:"sql"`
	}
	if err := json.Unmarshal(raw, &ans); err != nil {
		return "答えを読めませんでした: " + err.Error()
	}
	return renderForChat(ans.Text, ans.Read, ans.SQL != nil, sqlOf(ans.SQL))
}

func sqlOf(p *struct {
	Query string `json:"query"`
}) string {
	if p == nil {
		return ""
	}
	return p.Query
}

// chatBold is markdown's **bold**, which Chat spells *bold*.
var chatBold = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)

// renderForChat turns the agent's markdown into Chat's text formatting,
// and says what the answer read and what it would need run.
func renderForChat(text string, read []string, proposed bool, sql string) string {
	var b strings.Builder
	b.WriteString(chatBold.ReplaceAllString(strings.TrimSpace(text), "*$1*"))
	if proposed {
		b.WriteString("\n\n数字を確かめるには、次の SQL をあなたの権限で走らせる必要があります。Chat では走らせられないので、Web UI のエージェントで同じことを訊いてください:\n```\n")
		b.WriteString(strings.TrimSpace(sql))
		b.WriteString("\n```")
	}
	if len(read) > 0 {
		b.WriteString("\n\n読んだナレッジ: ")
		for i, id := range read {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString("`" + id + "`")
		}
	}
	return b.String()
}

// chatEvent is what Google Chat posts, in either of its two shapes: an
// interaction event (the Chat API's) or a Google Workspace add-on event,
// where the same fields sit under "chat".
type chatEvent struct {
	Type    string       `json:"type"`
	User    *chatUser    `json:"user"`
	Message *chatMessage `json:"message"`
	Chat    *struct {
		User           *chatUser `json:"user"`
		MessagePayload *struct {
			Message *chatMessage `json:"message"`
		} `json:"messagePayload"`
		AddedToSpacePayload *struct{} `json:"addedToSpacePayload"`
	} `json:"chat"`
}

type chatUser struct {
	Email string `json:"email"`
	Type  string `json:"type"`
}

type chatMessage struct {
	Text         string `json:"text"`
	ArgumentText string `json:"argumentText"`
}

// question returns who asked and what, whether the event is the add-on
// shape (which decides the reply's shape), and whether there is a
// person's message to answer. Being added to a space is answered with an
// empty text, which answer turns into a greeting.
func (e *chatEvent) question() (who, text string, addOn, ok bool) {
	user, msg := e.User, e.Message
	if e.Chat != nil {
		addOn = true
		user = e.Chat.User
		if e.Chat.MessagePayload != nil {
			msg = e.Chat.MessagePayload.Message
		}
	}
	if user == nil || user.Email == "" || user.Type == "BOT" {
		return "", "", addOn, false
	}
	if msg == nil {
		// Added to a space: say what this is.
		added := e.Type == "ADDED_TO_SPACE" || (e.Chat != nil && e.Chat.AddedToSpacePayload != nil)
		return user.Email, "", addOn, added
	}
	// argumentText is the message without the @mention that reached us.
	text = strings.TrimSpace(msg.ArgumentText)
	if text == "" {
		text = strings.TrimSpace(msg.Text)
	}
	return user.Email, text, addOn, true
}

// writeChatReply answers in the shape the event came in. An empty text is
// no message at all.
func writeChatReply(w http.ResponseWriter, addOn bool, text string) {
	w.Header().Set("Content-Type", "application/json")
	var body any = map[string]any{}
	switch {
	case text == "":
	case addOn:
		body = map[string]any{"hostAppDataAction": map[string]any{"chatDataAction": map[string]any{
			"createMessageAction": map[string]any{"message": map[string]any{"text": text}}}}}
	default:
		body = map[string]any{"text": text}
	}
	_ = json.NewEncoder(w).Encode(body)
}
