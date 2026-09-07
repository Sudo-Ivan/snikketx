package xmpp

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

const (
	NSPubSub      = "http://jabber.org/protocol/pubsub"
	NSPubSubOwner = "http://jabber.org/protocol/pubsub#owner"
	NSNick        = "http://jabber.org/protocol/nick"
	NSAvatarMeta  = "urn:xmpp:avatar:metadata"
	NSAvatarData  = "urn:xmpp:avatar:data"
	NSVCard4      = "urn:xmpp:vcard4"
	NSDataForms   = "jabber:x:data"
	NSNodeConfig  = "http://jabber.org/protocol/pubsub#node_config"
	NSRegister    = "jabber:iq:register"
	NSStanzas     = "urn:ietf:params:xml:ns:xmpp-stanzas"
)

type IQError struct {
	Status    int
	Condition string
}

func (e *IQError) Error() string {
	return fmt.Sprintf("iq error %s (http %d)", e.Condition, e.Status)
}

func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func SplitJID(jid string) (local, domain, resource string) {
	bare := jid
	if before, after, ok := strings.Cut(jid, "/"); ok {
		bare = before
		resource = after
	}
	if i := strings.IndexByte(bare, '@'); i >= 0 {
		return bare[:i], bare[i+1:], resource
	}
	return "", bare, resource
}

func PasswordChangeIQ(jid, username, password string) string {
	return fmt.Sprintf(
		`<iq to="%s" type="set" id="%s"><query xmlns="%s"><username>%s</username><password>%s</password></query></iq>`,
		xmlEscape(jid), NewID(), NSRegister, xmlEscape(username), xmlEscape(password),
	)
}

func NicknameGetIQ(jid string) string {
	return fmt.Sprintf(
		`<iq type="get" to="%s" id="%s"><pubsub xmlns="%s"><items node="%s" max_items="1"/></pubsub></iq>`,
		xmlEscape(jid), NewID(), NSPubSub, NSNick,
	)
}

func NicknameSetIQ(jid, nick string) string {
	return fmt.Sprintf(
		`<iq type="set" to="%s" id="%s"><pubsub xmlns="%s"><publish node="%s"><item><nick xmlns="%s">%s</nick></item></publish></pubsub></iq>`,
		xmlEscape(jid), NewID(), NSPubSub, NSNick, NSNick, xmlEscape(nick),
	)
}

func AvatarMetadataGetIQ(jid string) string {
	return fmt.Sprintf(
		`<iq type="get" to="%s" id="%s"><pubsub xmlns="%s"><items node="%s" max_items="1"/></pubsub></iq>`,
		xmlEscape(jid), NewID(), NSPubSub, NSAvatarMeta,
	)
}

func AvatarDataGetIQ(jid, id string) string {
	return fmt.Sprintf(
		`<iq type="get" to="%s" id="%s"><pubsub xmlns="%s"><items node="%s"><item id="%s"/></items></pubsub></iq>`,
		xmlEscape(jid), NewID(), NSPubSub, NSAvatarData, xmlEscape(id),
	)
}

func AvatarDataSetIQ(jid, id, b64 string) string {
	return fmt.Sprintf(
		`<iq type="set" to="%s" id="%s"><pubsub xmlns="%s"><publish node="%s"><item id="%s"><data xmlns="%s">%s</data></item></publish></pubsub></iq>`,
		xmlEscape(jid), NewID(), NSPubSub, NSAvatarData, xmlEscape(id), NSAvatarData, b64,
	)
}

func AvatarMetadataSetIQ(jid, id string, bytes int, mime string) string {
	return fmt.Sprintf(
		`<iq type="set" to="%s" id="%s"><pubsub xmlns="%s"><publish node="%s"><item id="%s"><metadata xmlns="%s"><info xmlns="%s" id="%s" bytes="%d" type="%s"/></metadata></item></publish></pubsub></iq>`,
		xmlEscape(jid), NewID(), NSPubSub, NSAvatarMeta, xmlEscape(id), NSAvatarMeta, NSAvatarMeta, xmlEscape(id), bytes, xmlEscape(mime),
	)
}

func PubSubConfigGetIQ(jid, node string) string {
	return fmt.Sprintf(
		`<iq type="get" to="%s" id="%s"><pubsub xmlns="%s"><configure node="%s"/></pubsub></iq>`,
		xmlEscape(jid), NewID(), NSPubSubOwner, xmlEscape(node),
	)
}

func PubSubAccessModelSetIQ(jid, node, model string) string {
	return fmt.Sprintf(
		`<iq type="set" to="%s" id="%s"><pubsub xmlns="%s"><configure node="%s"><x xmlns="%s" type="submit"><field var="FORM_TYPE" type="hidden"><value>%s</value></field><field var="pubsub#access_model"><value>%s</value></field></x></configure></pubsub></iq>`,
		xmlEscape(jid), NewID(), NSPubSubOwner, xmlEscape(node), NSDataForms, NSNodeConfig, xmlEscape(model),
	)
}

type iqEnvelope struct {
	XMLName xml.Name
	Type    string `xml:"type,attr"`
	Inner   []byte `xml:",innerxml"`
}

func ExtractIQReply(body []byte) ([]byte, error) {
	var iq iqEnvelope
	if err := xml.Unmarshal(body, &iq); err != nil {
		return nil, &IQError{Status: 500, Condition: "malformed"}
	}
	switch iq.Type {
	case "result":
		return iq.Inner, nil
	case "error":
		cond := findCondition(iq.Inner)
		status := 500
		if cond == "item-not-found" {
			status = 404
		}
		return nil, &IQError{Status: status, Condition: cond}
	default:
		return nil, &IQError{Status: 500, Condition: "unexpected-type"}
	}
}

func ExtractNickname(inner []byte) (string, bool) {
	dec := xml.NewDecoder(strings.NewReader(string(inner)))
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", false
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "nick" && (se.Name.Space == NSNick || se.Name.Space == "") {
			var nick struct {
				XMLName xml.Name
				Text    string `xml:",chardata"`
			}
			if err := dec.DecodeElement(&nick, &se); err != nil {
				return "", false
			}
			return nick.Text, true
		}
	}
}

type AvatarMeta struct {
	SHA1   string
	Type   string
	Bytes  int
	Width  int
	Height int
}

func ExtractAvatarMetadata(inner []byte) (*AvatarMeta, bool) {
	dec := xml.NewDecoder(strings.NewReader(string(inner)))
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, false
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "info" {
			m := &AvatarMeta{}
			for _, a := range se.Attr {
				switch a.Name.Local {
				case "id":
					m.SHA1 = a.Value
				case "type":
					m.Type = a.Value
				case "bytes":
					m.Bytes, _ = strconv.Atoi(a.Value)
				case "width":
					m.Width, _ = strconv.Atoi(a.Value)
				case "height":
					m.Height, _ = strconv.Atoi(a.Value)
				}
			}
			if m.SHA1 == "" {
				return nil, false
			}
			return m, true
		}
	}
}

func ExtractAvatarData(inner []byte) ([]byte, bool) {
	dec := xml.NewDecoder(strings.NewReader(string(inner)))
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, false
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "data" && (se.Name.Space == NSAvatarData || se.Name.Space == "") {
			var data struct {
				XMLName xml.Name
				Text    string `xml:",chardata"`
			}
			if err := dec.DecodeElement(&data, &se); err != nil {
				return nil, false
			}
			raw, err := decodeBase64(strings.TrimSpace(data.Text))
			if err != nil {
				return nil, false
			}
			return raw, true
		}
	}
}

func ExtractAccessModel(inner []byte, fallback string) string {
	dec := xml.NewDecoder(strings.NewReader(string(inner)))
	var currentVar string
	for {
		tok, err := dec.Token()
		if err != nil {
			return fallback
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "field" {
				currentVar = ""
				for _, a := range t.Attr {
					if a.Name.Local == "var" {
						currentVar = a.Value
					}
				}
			}
			if t.Name.Local == "value" && currentVar == "pubsub#access_model" {
				var v struct {
					Text string `xml:",chardata"`
				}
				if err := dec.DecodeElement(&v, &t); err == nil && strings.TrimSpace(v.Text) != "" {
					return strings.TrimSpace(v.Text)
				}
			}
		}
	}
}

func findCondition(inner []byte) string {
	dec := xml.NewDecoder(strings.NewReader(string(inner)))
	for {
		tok, err := dec.Token()
		if err != nil {
			return "undefined-condition"
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Space == NSStanzas {
			return se.Name.Local
		}
		if se.Name.Local == "error" {
			continue
		}
	}
}

func xmlEscape(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

func decodeBase64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}
