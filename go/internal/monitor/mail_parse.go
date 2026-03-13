package monitor

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"regexp"
	"strings"

	"golang.org/x/net/html/charset"
)

var (
	emailScriptBlockRE = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	emailEventAttrRE   = regexp.MustCompile(`(?i)\son[a-z]+\s*=\s*(?:"[^"]*"|'[^']*')`)
)

type parsedInternetMessage struct {
	Subject   string
	From      MailAddress
	To        []MailAddress
	Cc        []MailAddress
	Date      string
	MessageID string
	TextBody  string
	HTMLBody  string
	Calendar  string
}

func parseMessageAddressList(value string) []MailAddress {
	value = strings.TrimSpace(value)
	if value == "" {
		return []MailAddress{}
	}
	list, err := mail.ParseAddressList(value)
	if err != nil {
		return []MailAddress{{Email: value}}
	}
	out := make([]MailAddress, 0, len(list))
	for _, entry := range list {
		if entry == nil || strings.TrimSpace(entry.Address) == "" {
			continue
		}
		out = append(out, MailAddress{
			Name:  strings.TrimSpace(entry.Name),
			Email: strings.ToLower(strings.TrimSpace(entry.Address)),
		})
	}
	return out
}

func sanitizeEmailHTML(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	cleaned := emailScriptBlockRE.ReplaceAllString(raw, "")
	cleaned = emailEventAttrRE.ReplaceAllString(cleaned, "")
	return strings.TrimSpace(cleaned)
}

func decodeMailTransferBytes(content []byte, transferEncoding string) []byte {
	switch strings.ToLower(strings.TrimSpace(transferEncoding)) {
	case "base64":
		decoded, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, bytes.NewReader(bytes.TrimSpace(content))))
		if err == nil && len(decoded) > 0 {
			return decoded
		}
	case "quoted-printable":
		decoded, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(content)))
		if err == nil && len(decoded) > 0 {
			return decoded
		}
	}
	return content
}

func decodeMailBody(content []byte, transferEncoding string, charsetName string) string {
	content = decodeMailTransferBytes(content, transferEncoding)
	if charsetName != "" {
		if decoded, err := decodeCharsetBytes(content, charsetName); err == nil && decoded != "" {
			return decoded
		}
	}
	return string(content)
}

func decodeCharsetBytes(content []byte, charsetName string) (string, error) {
	reader, err := charset.NewReaderLabel(strings.TrimSpace(charsetName), bytes.NewReader(content))
	if err != nil {
		return string(content), nil
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func appendParsedPart(contentType string, charsetName string, transferEncoding string, body []byte, parsed *parsedInternetMessage) {
	content := strings.TrimSpace(decodeMailBody(body, transferEncoding, charsetName))
	if content == "" {
		return
	}
	switch {
	case strings.HasPrefix(contentType, "text/plain"):
		if parsed.TextBody == "" {
			parsed.TextBody = content
		} else {
			parsed.TextBody += "\n\n" + content
		}
	case strings.HasPrefix(contentType, "text/html"):
		cleaned := sanitizeEmailHTML(content)
		if cleaned == "" {
			return
		}
		if parsed.HTMLBody == "" {
			parsed.HTMLBody = cleaned
		} else {
			parsed.HTMLBody += "\n" + cleaned
		}
	case strings.HasPrefix(contentType, "text/calendar"):
		if parsed.Calendar == "" {
			parsed.Calendar = content
		} else {
			parsed.Calendar += "\n" + content
		}
	}
}

func mimeFilename(header textproto.MIMEHeader, params map[string]string) string {
	if name := strings.TrimSpace(params["name"]); name != "" {
		return name
	}
	disposition := strings.TrimSpace(header.Get("Content-Disposition"))
	if disposition == "" {
		return ""
	}
	_, attachmentParams, err := mime.ParseMediaType(disposition)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(attachmentParams["filename"])
}

func parsedMessageRichnessScore(message parsedInternetMessage) int {
	score := 0
	if strings.TrimSpace(message.MessageID) != "" {
		score += 100
	}
	if strings.TrimSpace(message.Subject) != "" {
		score += 40
	}
	if strings.TrimSpace(message.From.Email) != "" || strings.TrimSpace(message.From.Name) != "" {
		score += 40
	}
	if strings.TrimSpace(message.Date) != "" {
		score += 20
	}
	score += len(message.To) * 5
	score += len(message.Cc) * 3
	score += minInt(len(strings.TrimSpace(message.TextBody)), 4000) / 16
	score += minInt(len(strings.TrimSpace(message.HTMLBody)), 4000) / 24
	if strings.TrimSpace(message.Calendar) != "" {
		score += 80
	}
	return score
}

func parsedMessageHasCanonicalContent(message parsedInternetMessage) bool {
	hasHeaders := strings.TrimSpace(message.MessageID) != "" ||
		strings.TrimSpace(message.Subject) != "" ||
		strings.TrimSpace(message.Date) != "" ||
		strings.TrimSpace(message.From.Email) != "" ||
		strings.TrimSpace(message.From.Name) != ""
	hasBody := strings.TrimSpace(message.TextBody) != "" ||
		strings.TrimSpace(message.HTMLBody) != "" ||
		strings.TrimSpace(message.Calendar) != ""
	return hasHeaders && hasBody
}

func mergeParsedInternetMessage(primary parsedInternetMessage, fallback parsedInternetMessage) parsedInternetMessage {
	out := primary
	if strings.TrimSpace(out.Subject) == "" {
		out.Subject = fallback.Subject
	}
	if strings.TrimSpace(out.From.Email) == "" && strings.TrimSpace(out.From.Name) == "" {
		out.From = fallback.From
	}
	if len(out.To) == 0 && len(fallback.To) > 0 {
		out.To = append([]MailAddress(nil), fallback.To...)
	}
	if len(out.Cc) == 0 && len(fallback.Cc) > 0 {
		out.Cc = append([]MailAddress(nil), fallback.Cc...)
	}
	if strings.TrimSpace(out.Date) == "" {
		out.Date = fallback.Date
	}
	if strings.TrimSpace(out.MessageID) == "" {
		out.MessageID = fallback.MessageID
	}
	if strings.TrimSpace(out.Calendar) == "" {
		out.Calendar = fallback.Calendar
	}
	return out
}

func parseNestedInternetMessage(header textproto.MIMEHeader, mediaType string, params map[string]string, transferEncoding string, body []byte) (parsedInternetMessage, bool) {
	filename := strings.ToLower(strings.TrimSpace(mimeFilename(header, params)))
	if mediaType != "message/rfc822" && !strings.HasSuffix(filename, ".eml") {
		return parsedInternetMessage{}, false
	}
	nestedBody := decodeMailTransferBytes(body, transferEncoding)
	parsed, err := parseInternetMessage(nestedBody)
	if err != nil || !parsedMessageHasCanonicalContent(parsed) {
		return parsedInternetMessage{}, false
	}
	return parsed, true
}

func parseMIMEPart(header textproto.MIMEHeader, body []byte, parsed *parsedInternetMessage, nested *[]parsedInternetMessage) {
	contentType := strings.TrimSpace(header.Get("Content-Type"))
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = "text/plain"
		params = map[string]string{}
	}
	transferEncoding := header.Get("Content-Transfer-Encoding")
	if nestedMessage, ok := parseNestedInternetMessage(header, mediaType, params, transferEncoding, body); ok {
		*nested = append(*nested, nestedMessage)
		return
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := strings.TrimSpace(params["boundary"])
		if boundary == "" {
			return
		}
		reader := multipart.NewReader(bytes.NewReader(body), boundary)
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				break
			}
			partBody, err := io.ReadAll(part)
			_ = part.Close()
			if err != nil {
				continue
			}
			parseMIMEPart(part.Header, partBody, parsed, nested)
		}
		return
	}
	appendParsedPart(mediaType, params["charset"], transferEncoding, body, parsed)
}

func parseInternetMessage(raw []byte) (parsedInternetMessage, error) {
	parsed := parsedInternetMessage{}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return parsed, err
	}
	header := msg.Header
	parsed.Subject = strings.TrimSpace(header.Get("Subject"))
	if parsed.Subject == "" {
		parsed.Subject = strings.TrimSpace(header.Get("Thread-Topic"))
	}
	if fromList := parseMessageAddressList(header.Get("From")); len(fromList) > 0 {
		parsed.From = fromList[0]
	}
	parsed.To = parseMessageAddressList(header.Get("To"))
	parsed.Cc = parseMessageAddressList(header.Get("Cc"))
	parsed.MessageID = strings.TrimSpace(header.Get("Message-Id"))
	if parsed.MessageID == "" {
		parsed.MessageID = strings.TrimSpace(header.Get("Message-ID"))
	}
	if normalized := normalizePossibleDate(header.Get("Date")); normalized != "" {
		parsed.Date = normalized
	}
	body, err := io.ReadAll(msg.Body)
	if err != nil {
		return parsed, err
	}
	nested := make([]parsedInternetMessage, 0, 2)
	parseMIMEPart(textproto.MIMEHeader(header), body, &parsed, &nested)
	if len(nested) > 0 {
		best := parsedInternetMessage{}
		bestScore := -1
		for _, candidate := range nested {
			score := parsedMessageRichnessScore(candidate)
			if score > bestScore {
				best = candidate
				bestScore = score
			}
		}
		if parsedMessageRichnessScore(best) > parsedMessageRichnessScore(parsed) {
			parsed = mergeParsedInternetMessage(best, parsed)
		}
	}
	if strings.TrimSpace(parsed.TextBody) == "" && strings.TrimSpace(parsed.HTMLBody) != "" {
		parsed.TextBody = normalizeTextSnippet(parsed.HTMLBody, 120000)
	}
	return parsed, nil
}
