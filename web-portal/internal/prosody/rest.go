package prosody

import (
	"context"
	"crypto/sha1" // #nosec G505 -- XEP-0084 avatar content addressing uses SHA-1
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/sudo-ivan/snikketx/web-portal/internal/xmpp"
)

// ErrNoAvatar reports that an entity publishes no avatar.
var ErrNoAvatar = &HTTPError{Status: http.StatusNotFound, Message: "entity has no avatar"}

// TestSession reports whether the token is still accepted, by pinging the
// account itself.
func (c *Client) TestSession(ctx context.Context, token, address string) (bool, error) {
	status, _, err := c.jsonIQCall(ctx, token, restIQRequest{
		Kind: "iq",
		Type: "get",
		To:   address,
		Ping: true,
	})
	if err != nil {
		return false, err
	}
	return status == http.StatusOK, nil
}

// GetServerVersion returns the Prosody version string of the account's domain.
// It reports "unknown" when the server does not answer with a usable version.
func (c *Client) GetServerVersion(ctx context.Context, token, address string) (string, error) {
	_, domain, _ := xmpp.SplitJID(address)

	status, body, err := c.jsonIQCall(ctx, token, restIQRequest{
		Kind:    "iq",
		Type:    "get",
		To:      domain,
		Version: json.RawMessage(`{}`),
	})
	if err != nil {
		return "unknown", err
	}
	if status != http.StatusOK {
		return "unknown", nil
	}

	var reply struct {
		Version struct {
			Version string `json:"version"`
		} `json:"version"`
	}
	if err := json.Unmarshal(body, &reply); err != nil || reply.Version.Version == "" {
		return "unknown", nil
	}
	return reply.Version.Version, nil
}

// GetUserInfo assembles the profile summary shown by the portal. A missing
// avatar is not an error.
func (c *Client) GetUserInfo(ctx context.Context, token, address string, isAdmin bool) (*UserInfo, error) {
	localpart, _, _ := xmpp.SplitJID(address)

	nickname, err := c.GetUserNickname(ctx, token, address)
	if err != nil {
		return nil, err
	}

	var avatarHash string
	if avatar, err := c.GetAvatar(ctx, token, address, true); err == nil {
		avatarHash = avatar.SHA1
	}

	displayName := nickname
	if displayName == "" {
		displayName = localpart
	}

	return &UserInfo{
		Address:     address,
		Username:    localpart,
		Nickname:    nickname,
		DisplayName: displayName,
		AvatarHash:  avatarHash,
		IsAdmin:     isAdmin,
	}, nil
}

// GetUserNickname returns the published nickname, or an empty string when the
// account publishes none.
func (c *Client) GetUserNickname(ctx context.Context, token, address string) (string, error) {
	inner, err := c.xmlIQCall(ctx, token, xmpp.NicknameGetIQ(address))
	if err != nil {
		if isNotFound(err) {
			return "", nil
		}
		return "", err
	}

	nickname, ok := xmpp.ExtractNickname(inner)
	if !ok {
		return "", nil
	}
	return nickname, nil
}

// SetUserNickname publishes a new nickname on the account's pubsub node.
func (c *Client) SetUserNickname(ctx context.Context, token, address, nickname string) error {
	_, err := c.xmlIQCall(ctx, token, xmpp.NicknameSetIQ(address, nickname))
	return err
}

// GetAvatar returns the avatar published by an entity. With metadataOnly set
// the image data is left out. ErrNoAvatar is returned when the entity has no
// avatar.
func (c *Client) GetAvatar(ctx context.Context, token, address string, metadataOnly bool) (*Avatar, error) {
	inner, err := c.xmlIQCall(ctx, token, xmpp.AvatarMetadataGetIQ(address))
	if err != nil {
		if isNotFound(err) {
			return nil, ErrNoAvatar
		}
		return nil, err
	}

	meta, ok := xmpp.ExtractAvatarMetadata(inner)
	if !ok {
		return nil, ErrNoAvatar
	}

	avatar := &Avatar{
		SHA1:   meta.SHA1,
		Type:   meta.Type,
		Bytes:  meta.Bytes,
		Width:  meta.Width,
		Height: meta.Height,
	}

	if metadataOnly {
		return avatar, nil
	}

	data, err := c.GetAvatarData(ctx, token, address, meta.SHA1)
	if err != nil {
		return nil, err
	}
	avatar.Data = data
	return avatar, nil
}

// GetAvatarData fetches one avatar image by its published item id.
func (c *Client) GetAvatarData(ctx context.Context, token, address, id string) ([]byte, error) {
	inner, err := c.xmlIQCall(ctx, token, xmpp.AvatarDataGetIQ(address, id))
	if err != nil {
		return nil, err
	}

	data, ok := xmpp.ExtractAvatarData(inner)
	if !ok {
		return nil, nil
	}
	return data, nil
}

// SetUserAvatar publishes an avatar. The item id is the SHA1 of the image
// bytes, and the data node is published before the metadata node so clients
// never see metadata pointing at missing data.
func (c *Client) SetUserAvatar(ctx context.Context, token, address string, data []byte, mimetype string) error {
	sum := sha1.Sum(data) // #nosec G401 -- XEP-0084 content id is the SHA-1 of the avatar bytes
	id := hex.EncodeToString(sum[:])
	encoded := base64.StdEncoding.EncodeToString(data)

	if _, err := c.xmlIQCall(ctx, token, xmpp.AvatarDataSetIQ(address, id, encoded)); err != nil {
		return err
	}

	_, err := c.xmlIQCall(ctx, token, xmpp.AvatarMetadataSetIQ(address, id, len(data), mimetype))
	return err
}

// GetPubSubNodeAccessModel reads the access model of a pubsub node, falling
// back to fallback when the node config carries no such field.
func (c *Client) GetPubSubNodeAccessModel(ctx context.Context, token, address, node, fallback string) (string, error) {
	inner, err := c.xmlIQCall(ctx, token, xmpp.PubSubConfigGetIQ(address, node))
	if err != nil {
		return "", err
	}
	return xmpp.ExtractAccessModel(inner, fallback), nil
}

// SetPubSubNodeAccessModel reconfigures the access model of a pubsub node.
// With ignoreNotFound set, a node that does not exist yet is not an error.
func (c *Client) SetPubSubNodeAccessModel(ctx context.Context, token, address, node, accessModel string, ignoreNotFound bool) error {
	_, err := c.xmlIQCall(ctx, token, xmpp.PubSubAccessModelSetIQ(address, node, accessModel))
	if err != nil {
		if ignoreNotFound && isNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// GetNicknameAccessModel reads the access model of the nickname node.
func (c *Client) GetNicknameAccessModel(ctx context.Context, token, address string) (string, error) {
	return c.GetPubSubNodeAccessModel(ctx, token, address, xmpp.NSNick, AccessModelOpen)
}

// SetNicknameAccessModel reconfigures the access model of the nickname node.
func (c *Client) SetNicknameAccessModel(ctx context.Context, token, address, accessModel string) error {
	return c.SetPubSubNodeAccessModel(ctx, token, address, xmpp.NSNick, accessModel, true)
}

// GetAvatarAccessModel reads the access model of the avatar metadata node.
func (c *Client) GetAvatarAccessModel(ctx context.Context, token, address string) (string, error) {
	return c.GetPubSubNodeAccessModel(ctx, token, address, xmpp.NSAvatarMeta, AccessModelOpen)
}

// SetAvatarAccessModel reconfigures both avatar nodes, since the data and the
// metadata must stay reachable by the same audience.
func (c *Client) SetAvatarAccessModel(ctx context.Context, token, address, accessModel string) error {
	if err := c.SetPubSubNodeAccessModel(ctx, token, address, xmpp.NSAvatarData, accessModel, true); err != nil {
		return err
	}
	return c.SetPubSubNodeAccessModel(ctx, token, address, xmpp.NSAvatarMeta, accessModel, true)
}

// GetVCardAccessModel reads the access model of the vcard node.
func (c *Client) GetVCardAccessModel(ctx context.Context, token, address string) (string, error) {
	return c.GetPubSubNodeAccessModel(ctx, token, address, xmpp.NSVCard4, AccessModelOpen)
}

// SetVCardAccessModel reconfigures the access model of the vcard node.
func (c *Client) SetVCardAccessModel(ctx context.Context, token, address, accessModel string) error {
	return c.SetPubSubNodeAccessModel(ctx, token, address, xmpp.NSVCard4, accessModel, true)
}

// GuessProfileAccessModel summarises the visibility of a profile as a single
// access model. Nodes that do not exist are skipped, and the most open model
// found across the avatar, nickname and vcard nodes wins, since that is what
// actually governs how far the profile reaches.
func (c *Client) GuessProfileAccessModel(ctx context.Context, token, address string) (string, error) {
	order := []string{AccessModelOpen, AccessModelPresence, AccessModelWhitelist}

	getters := []func(context.Context, string, string) (string, error){
		c.GetAvatarAccessModel,
		c.GetNicknameAccessModel,
		c.GetVCardAccessModel,
	}

	worst := -1
	for _, get := range getters {
		model, err := get(ctx, token, address)
		if err != nil {
			if isNotFound(err) {
				continue
			}
			return "", err
		}

		index := 0
		for i, candidate := range order {
			if candidate == model {
				index = i
				break
			}
		}
		if worst < 0 || index < worst {
			worst = index
		}
	}

	if worst < 0 {
		return AccessModelOpen, nil
	}
	return order[worst], nil
}
