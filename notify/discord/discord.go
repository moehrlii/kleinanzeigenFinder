package discord

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Kamuyin/kleinanzeigenFinder/config"
	"github.com/Kamuyin/kleinanzeigenFinder/model"
	"github.com/Kamuyin/kleinanzeigenFinder/storage"
	"github.com/bwmarrin/discordgo"
	"github.com/sirupsen/logrus"
)

type DiscordNotifier struct {
	settings config.DiscordSettings
	session  *discordgo.Session
	monRepo  *storage.MonitorRepository
}

func NewDiscordNotifier(settings config.DiscordSettings, monRepo *storage.MonitorRepository) (*DiscordNotifier, error) {
	if !settings.Enabled {
		return &DiscordNotifier{settings: settings, monRepo: monRepo}, nil
	}
	if settings.Token() == "" {
		return nil, fmt.Errorf("discord is enabled but token is missing")
	}
	sess, err := discordgo.New("Bot " + settings.Token())
	if err != nil {
		return nil, err
	}

	sess.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages | discordgo.IntentsGuildMessageReactions | discordgo.IntentsGuildMembers
	return &DiscordNotifier{settings: settings, session: sess, monRepo: monRepo}, nil
}

func (d *DiscordNotifier) Session() *discordgo.Session { return d.session }

func (d *DiscordNotifier) NotifyAccepted(ctx context.Context, monitor config.MonitorConfig, listing model.Listing, classification model.Classification) (bool, error) {
	if !d.settings.Enabled || d.session == nil {
		return false, nil
	}

	channelID, err := d.ensureMonitorChannel(monitor)
	if err != nil {
		return false, fmt.Errorf("ensure channel: %w", err)
	}

	embed := buildListingEmbed(monitor.Name, listing, classification, true)
	components := feedbackComponents(monitor.Name, listing.Identity())

	msg, err := d.session.ChannelMessageSendComplex(strconv.FormatInt(channelID, 10), &discordgo.MessageSend{
		Embed:      embed,
		Components: components,
	})
	if err != nil {
		return false, err
	}

	var msgID int64
	fmt.Sscanf(msg.ID, "%d", &msgID)
	_ = d.monRepo.SaveDiscordMessage(monitor.Name, listing.Identity(), channelID, msgID, true)

	return true, nil
}

func (d *DiscordNotifier) NotifyRejected(ctx context.Context, monitor config.MonitorConfig, listing model.Listing, classification model.Classification) (bool, error) {
	if !d.settings.Enabled || d.session == nil || d.settings.ReviewChannelName == "" {
		return false, nil
	}

	channelID, err := d.ensureReviewChannel()
	if err != nil {
		return false, fmt.Errorf("ensure review channel: %w", err)
	}

	embed := buildListingEmbed(monitor.Name, listing, classification, false)
	components := feedbackComponents(monitor.Name, listing.Identity())

	msg, err := d.session.ChannelMessageSendComplex(strconv.FormatInt(channelID, 10), &discordgo.MessageSend{
		Embed:      embed,
		Components: components,
	})
	if err != nil {
		return false, err
	}

	var msgID int64
	fmt.Sscanf(msg.ID, "%d", &msgID)
	_ = d.monRepo.SaveDiscordMessage(monitor.Name, listing.Identity(), channelID, msgID, false)

	return true, nil
}

func (d *DiscordNotifier) ensureMonitorSubChannel(monitorName, suffix string) (int64, error) {
	guildID := d.settings.GuildID()
	categoryID, err := d.ensureCategory(guildID)
	if err != nil {
		return 0, err
	}

	channelName := slugify(d.settings.ChannelPrefix + monitorName + "-" + suffix)
	channels, err := d.session.GuildChannels(guildID)
	if err != nil {
		return 0, err
	}
	for _, ch := range channels {
		if ch.Name == channelName {
			id, _ := strconv.ParseInt(ch.ID, 10, 64)
			return id, nil
		}
	}

	ch, err := d.session.GuildChannelCreateComplex(guildID, discordgo.GuildChannelCreateData{
		Name:     channelName,
		Type:     discordgo.ChannelTypeGuildText,
		ParentID: categoryID,
	})
	if err != nil {
		return 0, err
	}
	logrus.Infof("Created Discord channel #%s", channelName)
	id, _ := strconv.ParseInt(ch.ID, 10, 64)
	return id, nil
}

func (d *DiscordNotifier) ensureMonitorChannel(monitor config.MonitorConfig) (int64, error) {
	// Check cached channel id
	if id := d.monRepo.GetChannelID(monitor.Name); id != nil {
		if _, err := d.session.Channel(strconv.FormatInt(*id, 10)); err == nil {
			return *id, nil
		}
	}

	guildID := d.settings.GuildID()
	categoryID, err := d.ensureCategory(guildID)
	if err != nil {
		return 0, err
	}

	channelName := slugify(d.settings.ChannelPrefix + monitor.Name)
	channels, err := d.session.GuildChannels(guildID)
	if err != nil {
		return 0, err
	}
	for _, ch := range channels {
		if ch.Name == channelName {
			id, _ := strconv.ParseInt(ch.ID, 10, 64)
			_ = d.monRepo.SetChannelID(monitor.Name, id)
			return id, nil
		}
	}

	ch, err := d.session.GuildChannelCreateComplex(guildID, discordgo.GuildChannelCreateData{
		Name:     channelName,
		Type:     discordgo.ChannelTypeGuildText,
		ParentID: categoryID,
	})
	if err != nil {
		return 0, err
	}
	logrus.Infof("Created Discord channel #%s", channelName)
	id, _ := strconv.ParseInt(ch.ID, 10, 64)
	_ = d.monRepo.SetChannelID(monitor.Name, id)
	return id, nil
}

func (d *DiscordNotifier) ensureReviewChannel() (int64, error) {
	guildID := d.settings.GuildID()
	categoryID, err := d.ensureCategory(guildID)
	if err != nil {
		return 0, err
	}

	channels, err := d.session.GuildChannels(guildID)
	if err != nil {
		return 0, err
	}
	reviewName := d.settings.ReviewChannelName
	for _, ch := range channels {
		if ch.Name == reviewName {
			id, _ := strconv.ParseInt(ch.ID, 10, 64)
			return id, nil
		}
	}
	ch, err := d.session.GuildChannelCreateComplex(guildID, discordgo.GuildChannelCreateData{
		Name:     reviewName,
		Type:     discordgo.ChannelTypeGuildText,
		ParentID: categoryID,
	})
	if err != nil {
		return 0, err
	}
	logrus.Infof("Created Discord review channel #%s", reviewName)
	id, _ := strconv.ParseInt(ch.ID, 10, 64)
	return id, nil
}

func (d *DiscordNotifier) ensureCategory(guildID string) (string, error) {
	channels, err := d.session.GuildChannels(guildID)
	if err != nil {
		return "", err
	}
	for _, ch := range channels {
		if ch.Type == discordgo.ChannelTypeGuildCategory && strings.EqualFold(ch.Name, d.settings.CategoryName) {
			return ch.ID, nil
		}
	}
	cat, err := d.session.GuildChannelCreateComplex(guildID, discordgo.GuildChannelCreateData{
		Name: d.settings.CategoryName,
		Type: discordgo.ChannelTypeGuildCategory,
	})
	if err != nil {
		return "", err
	}
	logrus.Infof("Created Discord category %s", d.settings.CategoryName)
	return cat.ID, nil
}

func feedbackComponents(monitorName, listingID string) []discordgo.MessageComponent {
	id := func(action string) string {
		return "kf:" + action + ":" + monitorName + ":" + listingID
	}
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: "Interested", Emoji: &discordgo.ComponentEmoji{Name: "🟢"}, Style: discordgo.SuccessButton, CustomID: id("interested")},
			discordgo.Button{Label: "Reject", Emoji: &discordgo.ComponentEmoji{Name: "🔴"}, Style: discordgo.DangerButton, CustomID: id("reject")},
			discordgo.Button{Label: "Maybe", Emoji: &discordgo.ComponentEmoji{Name: "🟡"}, Style: discordgo.SecondaryButton, CustomID: id("maybe")},
			discordgo.Button{Label: "Favorite", Emoji: &discordgo.ComponentEmoji{Name: "⭐"}, Style: discordgo.SecondaryButton, CustomID: id("favorite")},
		}},
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: "Follow-up", Emoji: &discordgo.ComponentEmoji{Name: "📌"}, Style: discordgo.SecondaryButton, CustomID: id("follow_up")},
			discordgo.Button{Label: "Note", Style: discordgo.PrimaryButton, CustomID: id("note")},
		}},
	}
}

func buildListingEmbed(monitorName string, listing model.Listing, classification model.Classification, accepted bool) *discordgo.MessageEmbed {
	color := 0x57F287 // green
	if !accepted {
		color = 0xED4245 // red
	}

	title := listing.Title
	if len(title) > 256 {
		title = title[:253] + "..."
	}
	if title == "" {
		title = "Kleinanzeigen offer"
	}

	desc := listing.Description
	if len(desc) > 1500 {
		desc = desc[:1497] + "..."
	}

	embed := &discordgo.MessageEmbed{
		Title:       title,
		URL:         listing.URL,
		Color:       color,
		Description: desc,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("monitor=%s listing=%s", monitorName, listing.Identity()),
		},
	}

	if listing.ImageURL != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: listing.ImageURL}
	}

	var fields []*discordgo.MessageEmbedField
	if listing.Price != "" {
		fields = append(fields, &discordgo.MessageEmbedField{Name: "Preis", Value: listing.Price, Inline: true})
	}
	if listing.Location != "" {
		fields = append(fields, &discordgo.MessageEmbedField{Name: "Ort", Value: listing.Location, Inline: true})
	}
	if listing.PostedAt != "" {
		fields = append(fields, &discordgo.MessageEmbedField{Name: "Online", Value: listing.PostedAt, Inline: true})
	}
	for _, key := range []string{"Wohnfläche", "Zimmer", "Warmmiete", "Kaltmiete"} {
		if val, ok := listing.Attributes[key]; ok && val != "" {
			fields = append(fields, &discordgo.MessageEmbedField{Name: key, Value: val, Inline: true})
		}
	}

	reason := classification.Reason
	if len(reason) > 1024 {
		reason = reason[:1021] + "..."
	}
	if reason != "" {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("Score %d/100", classification.Score),
			Value:  reason,
			Inline: false,
		})
	}
	if len(classification.RedFlags) > 0 {
		flags := classification.RedFlags
		if len(flags) > 5 {
			flags = flags[:5]
		}
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Red flags",
			Value:  strings.Join(flags, ", "),
			Inline: false,
		})
	}

	embed.Fields = fields
	if classification.VisualAssessment != "" {
		setEmbedField(embed, fmt.Sprintf("Photos (%d analyzed)", classification.ImagesAnalyzed), classification.VisualAssessment, false)
	}
	if len(classification.Uncertainties) > 0 {
		setEmbedField(embed, "To confirm", strings.Join(classification.Uncertainties, "\n"), false)
	}
	return embed
}

// slugify converts a string to a Discord-safe channel name.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prev := '-'
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			if r == '-' && prev == '-' {
				continue
			}
			b.WriteRune(r)
			prev = r
		} else {
			if prev != '-' {
				b.WriteRune('-')
				prev = '-'
			}
		}
	}
	result := strings.Trim(b.String(), "-")
	if len(result) > 80 {
		result = result[:80]
	}
	if result == "" {
		return "monitor"
	}
	return result
}
