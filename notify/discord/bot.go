package discord

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/Kamuyin/kleinanzeigenFinder/config"
	"github.com/Kamuyin/kleinanzeigenFinder/llm"
	"github.com/Kamuyin/kleinanzeigenFinder/storage"
	"github.com/bwmarrin/discordgo"
	"github.com/sirupsen/logrus"
)

type Bot struct {
	session    *discordgo.Session
	monRepo    *storage.MonitorRepository
	notifier   *DiscordNotifier
	classifier *llm.Classifier
	cfg        config.DiscordSettings
}

func NewBot(session *discordgo.Session, notifier *DiscordNotifier, monRepo *storage.MonitorRepository, classifier *llm.Classifier, cfg config.DiscordSettings) *Bot {
	return &Bot{
		session:    session,
		monRepo:    monRepo,
		notifier:   notifier,
		classifier: classifier,
		cfg:        cfg,
	}
}

func (b *Bot) Start() error {
	if b.session == nil {
		return nil
	}
	b.session.AddHandler(b.onReady)
	b.session.AddHandler(b.onInteraction)
	b.session.AddHandler(b.onReactionAdd)

	if err := b.session.Open(); err != nil {
		return err
	}
	return nil
}

func (b *Bot) Stop() {
	if b.session != nil {
		_ = b.session.Close()
	}
}

func (b *Bot) onReady(s *discordgo.Session, event *discordgo.Ready) {
	logrus.Infof("Discord bot ready: %s", event.User.Username)
	if b.cfg.SyncCommands {
		b.registerCommands()
	}
}

func (b *Bot) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		b.handleCommand(s, i)
	case discordgo.InteractionMessageComponent:
		b.handleComponent(s, i)
	case discordgo.InteractionModalSubmit:
		b.handleModal(s, i)
	}
}

func (b *Bot) onReactionAdd(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
	if b.session.State.User != nil && r.UserID == b.session.State.User.ID {
		return
	}
	if r.GuildID != b.cfg.GuildID() {
		return
	}

	reactionStatus := map[string]string{
		"🟢": "interested",
		"🔴": "rejected",
		"🟡": "maybe",
		"⭐": "favorite",
		"📌": "follow_up",
	}
	status, ok := reactionStatus[r.Emoji.Name]
	if !ok {
		return
	}

	msg, err := s.ChannelMessage(r.ChannelID, r.MessageID)
	if err != nil {
		return
	}
	monitorName, listingID := parseEmbedFooter(msg)
	if monitorName == "" || listingID == "" {
		return
	}

	userID, _ := strconv.ParseInt(r.UserID, 10, 64)
	_ = b.monRepo.SetFeedback(monitorName, listingID, status, userID, nil)
	logrus.Debugf("Reaction status=%s monitor=%s listing=%s user=%s", status, monitorName, listingID, r.UserID)
}

// Commands
func (b *Bot) registerCommands() {
	guildID := b.cfg.GuildID()

	kfCmd := &discordgo.ApplicationCommand{
		Name:        "kf",
		Description: "Kleinanzeigen Finder bot controls",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "status",
				Description: "Show bot status",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
		},
	}
	monitorCmd := &discordgo.ApplicationCommand{
		Name:        "monitor",
		Description: "Manage Kleinanzeigen monitors",
		Options: []*discordgo.ApplicationCommandOption{
			{Name: "list", Description: "List configured monitors", Type: discordgo.ApplicationCommandOptionSubCommand},
			{Name: "create", Description: "Create a new monitor (manual or AI wizard)", Type: discordgo.ApplicationCommandOptionSubCommand},
			{
				Name:        "edit",
				Description: "Edit an existing monitor",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{Type: discordgo.ApplicationCommandOptionString, Name: "name", Description: "Monitor name", Required: true},
				},
			},
			{
				Name:        "delete",
				Description: "Delete a monitor",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{Type: discordgo.ApplicationCommandOptionString, Name: "name", Description: "Monitor name", Required: true},
				},
			},
			{
				Name:        "pause",
				Description: "Pause a monitor",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{Type: discordgo.ApplicationCommandOptionString, Name: "name", Description: "Monitor name", Required: true},
				},
			},
			{
				Name:        "resume",
				Description: "Resume a paused monitor",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{Type: discordgo.ApplicationCommandOptionString, Name: "name", Description: "Monitor name", Required: true},
				},
			},
			{Name: "sync-channels", Description: "Create/link Discord channels for all monitors", Type: discordgo.ApplicationCommandOptionSubCommand},
			{
				Name:        "test",
				Description: "Show monitor details and ensure channel exists",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{Type: discordgo.ApplicationCommandOptionString, Name: "name", Description: "Monitor name", Required: true},
				},
			},
		},
	}

	for _, cmd := range []*discordgo.ApplicationCommand{kfCmd, monitorCmd} {
		_, err := b.session.ApplicationCommandCreate(b.session.State.User.ID, guildID, cmd)
		if err != nil {
			logrus.Errorf("Could not create command /%s: %v", cmd.Name, err)
		} else {
			logrus.Infof("Registered command /%s", cmd.Name)
		}
	}
}

// Handlers
func (b *Bot) handleCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !b.isAuthorized(i) {
		b.ephemeralReply(s, i, "You do not have permission to use this command.")
		return
	}

	switch i.ApplicationCommandData().Name {
	case "kf":
		b.handleKFCommand(s, i)
	case "monitor":
		b.handleMonitorCommand(s, i)
	}
}

func (b *Bot) handleKFCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	opts := i.ApplicationCommandData().Options
	if len(opts) == 0 {
		return
	}
	switch opts[0].Name {
	case "status":
		all := b.monRepo.List(true)
		enabled := b.monRepo.List(false)
		b.ephemeralReply(s, i, fmt.Sprintf("✅ Running. **%d** enabled monitor(s), **%d** total.", len(enabled), len(all)))
	}
}

func (b *Bot) handleMonitorCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	opts := i.ApplicationCommandData().Options
	if len(opts) == 0 {
		return
	}
	sub := opts[0]
	switch sub.Name {
	case "list":
		all := b.monRepo.List(true)
		if len(all) == 0 {
			b.ephemeralReply(s, i, "No monitors configured.")
			return
		}
		var lines []string
		for _, m := range all {
			st := "✅"
			if !m.Enabled {
				st = "⏸️"
			}
			lines = append(lines, fmt.Sprintf("%s `%s` — mode=%s — backfill=%ds watch=%ds — <%s>",
				st, m.Name, m.Mode, m.BackfillIntervalSeconds, m.WatchIntervalSeconds, m.SearchURL))
		}
		b.ephemeralReply(s, i, strings.Join(lines, "\n"))

	case "create":
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "How would you like to create the monitor?",
				Flags:   discordgo.MessageFlagsEphemeral,
				Components: []discordgo.MessageComponent{
					discordgo.ActionsRow{Components: []discordgo.MessageComponent{
						discordgo.Button{Label: "✨ AI Wizard", Style: discordgo.PrimaryButton, CustomID: "kf:btn:ai_wizard"},
						discordgo.Button{Label: "📝 Manual Form", Style: discordgo.SecondaryButton, CustomID: "kf:btn:manual_form"},
					}},
				},
			},
		})

	case "edit":
		name := sub.Options[0].StringValue()
		monitor := b.monRepo.Get(name)
		if monitor == nil {
			b.ephemeralReply(s, i, fmt.Sprintf("Monitor `%s` not found.", name))
			return
		}
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseModal,
			Data: manualMonitorModal(monitor),
		})

	case "delete":
		name := sub.Options[0].StringValue()
		if b.monRepo.Delete(name) {
			b.ephemeralReply(s, i, fmt.Sprintf("Deleted `%s`.", name))
		} else {
			b.ephemeralReply(s, i, fmt.Sprintf("Monitor `%s` not found.", name))
		}

	case "pause":
		name := sub.Options[0].StringValue()
		if b.monRepo.SetEnabled(name, false) {
			b.ephemeralReply(s, i, fmt.Sprintf("⏸️ Paused `%s`.", name))
		} else {
			b.ephemeralReply(s, i, fmt.Sprintf("Monitor `%s` not found.", name))
		}

	case "resume":
		name := sub.Options[0].StringValue()
		if b.monRepo.SetEnabled(name, true) {
			b.ephemeralReply(s, i, fmt.Sprintf("▶️ Resumed `%s`.", name))
		} else {
			b.ephemeralReply(s, i, fmt.Sprintf("Monitor `%s` not found.", name))
		}

	case "sync-channels":
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
		})
		for _, m := range b.monRepo.List(true) {
			_, err := b.notifier.ensureMonitorChannel(m)
			if err != nil {
				logrus.Warnf("sync-channels: %v", err)
			}
		}
		_, err := b.notifier.ensureReviewChannel()
		if err != nil {
			logrus.Warnf("sync-channels review: %v", err)
		}
		_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: "Channels synced.",
			Flags:   discordgo.MessageFlagsEphemeral,
		})

	case "test":
		name := sub.Options[0].StringValue()
		monitor := b.monRepo.Get(name)
		if monitor == nil {
			b.ephemeralReply(s, i, fmt.Sprintf("Monitor `%s` not found.", name))
			return
		}
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
		})
		chID, err := b.notifier.ensureMonitorChannel(*monitor)
		var reply string
		if err != nil {
			reply = fmt.Sprintf("Error ensuring channel: %v", err)
		} else {
			reply = fmt.Sprintf("`%s` linked to <#%d>\nMode: %s | Backfill: %ds | Watch: %ds/%d page(s) | Search cap: %s | Detail pages/run: %d | Details: %v\nURL: <%s>",
				monitor.Name, chID, monitor.Mode, monitor.BackfillIntervalSeconds, monitor.WatchIntervalSeconds, monitor.WatchPages,
				searchCapLabel(monitor.MaxResults), monitor.MaxDetailPages, monitor.FetchDetails, monitor.SearchURL)
		}
		_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: reply,
			Flags:   discordgo.MessageFlagsEphemeral,
		})
	}
}

func (b *Bot) handleComponent(s *discordgo.Session, i *discordgo.InteractionCreate) {
	id := i.MessageComponentData().CustomID

	actionID := id
	if strings.HasPrefix(id, "kf:btn:save_draft:") {
		actionID = "kf:btn:save_draft"
	}
	if strings.HasPrefix(id, "kf:btn:edit_draft:") {
		actionID = "kf:btn:edit_draft"
	}
	switch actionID {
	// Wizard actions keep their complete draft in SQLite.
	case "kf:btn:ai_wizard":
		if !b.isAuthorized(i) {
			b.ephemeralReply(s, i, "No permission.")
			return
		}
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseModal,
			Data: aiWizardModal(),
		})
		return
	case "kf:btn:manual_form":
		if !b.isAuthorized(i) {
			b.ephemeralReply(s, i, "No permission.")
			return
		}
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseModal,
			Data: manualMonitorModal(nil),
		})
		return
	case "kf:btn:save_draft":
		if !b.isAuthorized(i) {
			b.ephemeralReply(s, i, "No permission.")
			return
		}
		b.saveDraftFromMessage(s, i)
		return
	case "kf:btn:edit_draft":
		if !b.isAuthorized(i) {
			b.ephemeralReply(s, i, "No permission.")
			return
		}
		b.editDraftFromMessage(s, i)
		return
	}

	parts := strings.SplitN(id, ":", 4)
	if len(parts) != 4 || parts[0] != "kf" {
		return
	}
	if !b.isAuthorized(i) {
		b.ephemeralReply(s, i, "You do not have permission.")
		return
	}

	action, monitorName, listingID := parts[1], parts[2], parts[3]

	switch action {
	case "interested", "reject", "maybe", "favorite", "follow_up":
		userID, _ := strconv.ParseInt(i.Member.User.ID, 10, 64)
		_ = b.monRepo.SetFeedback(monitorName, listingID, action, userID, nil)
		b.handleListingFeedbackAction(s, i, action, monitorName, listingID)

	case "note":
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseModal,
			Data: &discordgo.InteractionResponseData{
				CustomID: "kf:modal:note:" + monitorName + ":" + listingID,
				Title:    "Add Note",
				Components: []discordgo.MessageComponent{
					discordgo.ActionsRow{Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "note_text",
							Label:       "Note",
							Style:       discordgo.TextInputParagraph,
							Placeholder: "Your notes about this listing...",
							Required:    true,
							MaxLength:   1000,
						},
					}},
				},
			},
		})
	}
}

func (b *Bot) handleModal(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !b.isAuthorized(i) {
		b.ephemeralReply(s, i, "No permission.")
		return
	}

	id := i.ModalSubmitData().CustomID
	parts := strings.SplitN(id, ":", 4)

	switch {
	case id == "kf:modal:manual_create" || id == "kf:modal:manual_edit" || strings.HasPrefix(id, "kf:modal:draft_edit:"):
		b.handleManualMonitorModal(s, i)

	case id == "kf:modal:ai_wizard":
		b.handleAIWizardModal(s, i)

	case len(parts) == 4 && parts[0] == "kf" && parts[1] == "modal" && parts[2] == "note":
		sub := strings.SplitN(id, ":", 5)
		if len(sub) < 5 {
			return
		}
		monitorName := sub[3]
		listingID := sub[4]
		noteText := modalTextValue(i, "note_text")
		userID, _ := strconv.ParseInt(i.Member.User.ID, 10, 64)
		_ = b.monRepo.SetFeedback(monitorName, listingID, "note", userID, &noteText)
		if err := b.updateStoredListingNote(monitorName, listingID, noteText); err != nil {
			logrus.Warnf("Could not update Discord note monitor=%s listing=%s: %v", monitorName, listingID, err)
			b.ephemeralReply(s, i, "Note saved, but I could not update the message embed.")
		} else {
			b.ephemeralReply(s, i, "Note saved.")
		}
	}
}

func (b *Bot) handleManualMonitorModal(s *discordgo.Session, i *discordgo.InteractionCreate) {
	name := slugify(modalTextValue(i, "name"))
	searchURL := modalTextValue(i, "search_url")
	criteria := modalTextValue(i, "criteria")
	excludeRaw := modalTextValue(i, "exclude_keywords")
	scanSettings := modalTextValue(i, "scan_settings")
	if scanSettings == "" {
		scanSettings = modalTextValue(i, "interval_seconds")
	}

	provider, err := b.classifier.ProviderForURL(searchURL)
	if err != nil {
		b.ephemeralReply(s, i, err.Error())
		return
	}

	backfillInterval, maxDetailPages, watchInterval, watchPages := parseScanSettings(scanSettings, 300, 8, 1800, 2)
	mode := "backfill"
	if i.ModalSubmitData().CustomID == "kf:modal:manual_edit" {
		if existing := b.monRepo.Get(name); existing != nil && existing.Mode != "" {
			mode = existing.Mode
		}
	}

	monitor := config.MonitorConfig{
		Platform:                provider.ID,
		Name:                    name,
		SearchURL:               searchURL,
		Criteria:                criteria,
		Mode:                    mode,
		IntervalSeconds:         backfillInterval,
		BackfillIntervalSeconds: backfillInterval,
		WatchIntervalSeconds:    watchInterval,
		WatchPages:              watchPages,
		MaxResults:              0,
		FetchDetails:            true,
		MaxDetailPages:          maxDetailPages,
		Enabled:                 true,
		Rules: config.RulesConfig{
			ExcludeKeywords: splitWords(excludeRaw),
		},
	}

	var original *config.MonitorConfig
	if strings.HasPrefix(i.ModalSubmitData().CustomID, "kf:modal:draft_edit:") {
		var err error
		original, err = b.monRepo.GetDraft(strings.TrimPrefix(i.ModalSubmitData().CustomID, "kf:modal:draft_edit:"), i.Member.User.ID)
		if err != nil {
			b.ephemeralReply(s, i, "Draft expired or unavailable.")
			return
		}
	} else if i.ModalSubmitData().CustomID == "kf:modal:manual_edit" {
		original = b.monRepo.Get(name)
	}
	if original != nil {
		monitor.Platform = original.Platform
		monitor.Rules = original.Rules
		monitor.Rules.ExcludeKeywords = splitWords(excludeRaw)
		monitor.Enabled = original.Enabled
		monitor.FetchDetails = original.FetchDetails
		monitor.MaxResults = original.MaxResults
	}
	if err := b.classifier.ValidateDraft(monitor); err != nil {
		b.ephemeralReply(s, i, err.Error())
		return
	}
	if err := b.monRepo.Upsert(monitor, "discord"); err != nil {
		b.ephemeralReply(s, i, "Could not save monitor.")
		return
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})
	chID, err := b.notifier.ensureMonitorChannel(monitor)
	var reply string
	if err != nil {
		reply = fmt.Sprintf("Saved `%s`, but could not create channel: %v", name, err)
	} else {
		reply = fmt.Sprintf("✅ Saved `%s` and linked <#%d>.", name, chID)
	}
	_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: reply,
		Flags:   discordgo.MessageFlagsEphemeral,
	})
}

func (b *Bot) handleAIWizardModal(s *discordgo.Session, i *discordgo.InteractionCreate) {
	description := modalTextValue(i, "description")
	searchURL := modalTextValue(i, "search_url")

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})

	draft, err := b.classifier.DraftMonitor(context.Background(), description, searchURL)
	if err != nil {
		_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: fmt.Sprintf("AI Wizard failed: %v. Use `/monitor create` → Manual Form instead.", err),
			Flags:   discordgo.MessageFlagsEphemeral,
		})
		return
	}

	draftID, err := b.monRepo.SaveDraft(draft, i.Member.User.ID)
	if err != nil {
		b.followupEphemeral(s, i, "Could not save draft. Please try again.")
		return
	}
	preview := formatDraftPreview(draft)
	_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: preview,
		Flags:   discordgo.MessageFlagsEphemeral,
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{
				discordgo.Button{Label: "✅ Save Monitor", Style: discordgo.SuccessButton, CustomID: "kf:btn:save_draft:" + draftID},
				discordgo.Button{Label: "✏️ Edit Manually", Style: discordgo.SecondaryButton, CustomID: "kf:btn:edit_draft:" + draftID},
			}},
		},
	})
}

func (b *Bot) saveDraftFromMessage(s *discordgo.Session, i *discordgo.InteractionCreate) {
	draftID := strings.TrimPrefix(i.MessageComponentData().CustomID, "kf:btn:save_draft:")
	monitor, err := b.monRepo.GetDraft(draftID, i.Member.User.ID)
	if err != nil {
		b.ephemeralReply(s, i, "Draft expired or unavailable. Create a new draft.")
		return
	}
	if err := b.classifier.ValidateDraft(*monitor); err != nil {
		b.ephemeralReply(s, i, err.Error())
		return
	}
	if err := b.monRepo.Upsert(*monitor, "discord-ai"); err != nil {
		b.ephemeralReply(s, i, "Could not save monitor.")
		return
	}
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})
	chID, err := b.notifier.ensureMonitorChannel(*monitor)
	var reply string
	if err != nil {
		reply = fmt.Sprintf("Saved `%s`, channel error: %v", monitor.Name, err)
	} else {
		reply = fmt.Sprintf("✅ Saved `%s` and linked <#%d>.", monitor.Name, chID)
	}
	_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{Content: reply, Flags: discordgo.MessageFlagsEphemeral})
}

func (b *Bot) editDraftFromMessage(s *discordgo.Session, i *discordgo.InteractionCreate) {
	draftID := strings.TrimPrefix(i.MessageComponentData().CustomID, "kf:btn:edit_draft:")
	monitor, err := b.monRepo.GetDraft(draftID, i.Member.User.ID)
	if err != nil {
		b.ephemeralReply(s, i, "Draft expired or unavailable. Create a new draft.")
		return
	}
	modal := manualMonitorModal(monitor)
	modal.CustomID = "kf:modal:draft_edit:" + draftID
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseModal, Data: modal})
}

func (b *Bot) handleListingFeedbackAction(s *discordgo.Session, i *discordgo.InteractionCreate, action, monitorName, listingID string) {
	label := strings.ReplaceAll(action, "_", " ")

	if action == "reject" {
		b.ephemeralReply(s, i, "Rejected and deleted from this channel.")
		if i.Message != nil {
			if err := s.ChannelMessageDelete(i.ChannelID, i.Message.ID); err != nil {
				logrus.Warnf("Could not delete rejected Discord message monitor=%s listing=%s: %v", monitorName, listingID, err)
			}
		}
		return
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})

	if err := b.markInteractionMessage(s, i, action); err != nil {
		logrus.Warnf("Could not mark Discord message monitor=%s listing=%s action=%s: %v", monitorName, listingID, action, err)
	}

	switch action {
	case "interested":
		if err := b.copyInteractionMessageToStatusChannel(s, i, monitorName, "interested", action, false); err != nil {
			logrus.Warnf("Could not copy interested listing monitor=%s listing=%s: %v", monitorName, listingID, err)
			b.followupEphemeral(s, i, "Marked as interested, but I could not copy it to the interested channel.")
			return
		}
	case "favorite":
		if i.Message != nil {
			if err := s.ChannelMessagePin(i.ChannelID, i.Message.ID); err != nil {
				logrus.Warnf("Could not pin favorite in source channel monitor=%s listing=%s: %v", monitorName, listingID, err)
			}
		}
		if err := b.copyInteractionMessageToStatusChannel(s, i, monitorName, "favorites", action, true); err != nil {
			logrus.Warnf("Could not copy favorite listing monitor=%s listing=%s: %v", monitorName, listingID, err)
			b.followupEphemeral(s, i, "Marked as favorite, but I could not copy it to the favorites channel.")
			return
		}
	}

	b.followupEphemeral(s, i, fmt.Sprintf("Marked as **%s**.", label))
}

func (b *Bot) copyInteractionMessageToStatusChannel(s *discordgo.Session, i *discordgo.InteractionCreate, monitorName, suffix, action string, pin bool) error {
	if i.Message == nil || len(i.Message.Embeds) == 0 {
		return fmt.Errorf("source message has no embed")
	}

	channelID, err := b.notifier.ensureMonitorSubChannel(monitorName, suffix)
	if err != nil {
		return err
	}

	embed := cloneEmbed(i.Message.Embeds[0])
	applyStatusToEmbed(embed, action)
	msg, err := s.ChannelMessageSendComplex(strconv.FormatInt(channelID, 10), &discordgo.MessageSend{
		Embed:      embed,
		Components: i.Message.Components,
	})
	if err != nil {
		return err
	}
	if pin {
		if err := s.ChannelMessagePin(strconv.FormatInt(channelID, 10), msg.ID); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bot) markInteractionMessage(s *discordgo.Session, i *discordgo.InteractionCreate, action string) error {
	if i.Message == nil || len(i.Message.Embeds) == 0 {
		return nil
	}
	embed := cloneEmbed(i.Message.Embeds[0])
	applyStatusToEmbed(embed, action)
	_, err := s.ChannelMessageEditEmbed(i.ChannelID, i.Message.ID, embed)
	return err
}

func (b *Bot) updateStoredListingNote(monitorName, listingID, note string) error {
	msg := b.monRepo.GetDiscordMessage(monitorName, listingID)
	if msg == nil {
		return fmt.Errorf("stored Discord message not found")
	}

	channelID := strconv.FormatInt(msg.ChannelID, 10)
	messageID := strconv.FormatInt(msg.MessageID, 10)
	discordMsg, err := b.session.ChannelMessage(channelID, messageID)
	if err != nil {
		return err
	}
	if len(discordMsg.Embeds) == 0 {
		return fmt.Errorf("stored Discord message has no embed")
	}

	embed := cloneEmbed(discordMsg.Embeds[0])
	setEmbedField(embed, "Note", note, false)
	_, err = b.session.ChannelMessageEditEmbed(channelID, messageID, embed)
	return err
}

func cloneEmbed(embed *discordgo.MessageEmbed) *discordgo.MessageEmbed {
	if embed == nil {
		return &discordgo.MessageEmbed{}
	}
	clone := *embed
	if embed.Footer != nil {
		footer := *embed.Footer
		clone.Footer = &footer
	}
	if embed.Thumbnail != nil {
		thumbnail := *embed.Thumbnail
		clone.Thumbnail = &thumbnail
	}
	if embed.Image != nil {
		image := *embed.Image
		clone.Image = &image
	}
	if embed.Author != nil {
		author := *embed.Author
		clone.Author = &author
	}
	if embed.Fields != nil {
		clone.Fields = make([]*discordgo.MessageEmbedField, 0, len(embed.Fields))
		for _, field := range embed.Fields {
			if field == nil {
				continue
			}
			fieldClone := *field
			clone.Fields = append(clone.Fields, &fieldClone)
		}
	}
	return &clone
}

func applyStatusToEmbed(embed *discordgo.MessageEmbed, action string) {
	statusLabel := map[string]string{
		"interested": "Interested",
		"maybe":      "Maybe",
		"favorite":   "Favorite",
		"follow_up":  "Follow-up",
	}[action]
	if statusLabel == "" {
		statusLabel = strings.ReplaceAll(action, "_", " ")
	}

	switch action {
	case "interested":
		embed.Color = 0x2ECC71
	case "maybe":
		embed.Color = 0xF1C40F
	case "favorite":
		embed.Color = 0xF5C542
	case "follow_up":
		embed.Color = 0x3498DB
	}
	setEmbedField(embed, "Status", statusLabel, true)
}

func setEmbedField(embed *discordgo.MessageEmbed, name, value string, inline bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "-"
	}
	if len(value) > 1024 {
		value = value[:1021] + "..."
	}

	for _, field := range embed.Fields {
		if field != nil && strings.EqualFold(field.Name, name) {
			field.Value = value
			field.Inline = inline
			return
		}
	}

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   name,
		Value:  value,
		Inline: inline,
	})
}

func (b *Bot) isAuthorized(i *discordgo.InteractionCreate) bool {
	if i.Member == nil {
		return false
	}
	adminRoles := b.cfg.AdminRoleIDs
	if len(adminRoles) == 0 {
		// Fall back to server admin permission
		perms, err := b.session.UserChannelPermissions(i.Member.User.ID, i.ChannelID)
		if err != nil {
			return false
		}
		return perms&discordgo.PermissionAdministrator != 0
	}
	roleSet := make(map[string]bool)
	for _, r := range adminRoles {
		roleSet[r] = true
	}
	for _, r := range i.Member.Roles {
		if roleSet[r] {
			return true
		}
	}
	return false
}

func (b *Bot) ephemeralReply(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

func (b *Bot) followupEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: content,
		Flags:   discordgo.MessageFlagsEphemeral,
	})
}

func modalTextValue(i *discordgo.InteractionCreate, customID string) string {
	for _, comp := range i.ModalSubmitData().Components {
		row, ok := comp.(*discordgo.ActionsRow)
		if !ok {
			continue
		}
		for _, c := range row.Components {
			if ti, ok := c.(*discordgo.TextInput); ok && ti.CustomID == customID {
				return ti.Value
			}
		}
	}
	return ""
}

func parseEmbedFooter(msg *discordgo.Message) (string, string) {
	if len(msg.Embeds) == 0 || msg.Embeds[0].Footer == nil {
		return "", ""
	}
	re := regexp.MustCompile(`monitor=(\S+)\s+listing=(\S+)`)
	m := re.FindStringSubmatch(msg.Embeds[0].Footer.Text)
	if m == nil {
		return "", ""
	}
	return m[1], m[2]
}

func splitWords(s string) []string {
	var result []string
	for _, part := range regexp.MustCompile(`[,;\n]+`).Split(s, -1) {
		if p := strings.TrimSpace(part); p != "" {
			result = append(result, p)
		}
	}
	return result
}

func parseScanSettings(value string, fallbackInterval, fallbackDetailPages, fallbackWatchInterval, fallbackWatchPages int) (int, int, int, int) {
	interval := fallbackInterval
	detailPages := fallbackDetailPages
	watchInterval := fallbackWatchInterval
	watchPages := fallbackWatchPages

	numbers := regexp.MustCompile(`\d+`).FindAllString(value, 4)
	if len(numbers) > 0 {
		if n, err := strconv.Atoi(numbers[0]); err == nil && n >= 60 {
			interval = n
		}
	}
	if len(numbers) > 1 {
		if n, err := strconv.Atoi(numbers[1]); err == nil && n > 0 {
			detailPages = n
		}
	}
	if len(numbers) > 2 {
		if n, err := strconv.Atoi(numbers[2]); err == nil && n >= 60 {
			watchInterval = n
		}
	}
	if len(numbers) > 3 {
		if n, err := strconv.Atoi(numbers[3]); err == nil && n > 0 {
			watchPages = n
		}
	}

	return interval, detailPages, watchInterval, watchPages
}

func searchCapLabel(maxResults int) string {
	if maxResults <= 0 {
		return "unlimited"
	}
	return strconv.Itoa(maxResults)
}

func manualMonitorModal(existing *config.MonitorConfig) *discordgo.InteractionResponseData {
	name, searchURL, criteria, excludeKW, scanSettings := "", "", "", "", "300 / 8 / 1800 / 2"
	modalID := "kf:modal:manual_create"
	title := "Create Monitor"

	if existing != nil && existing.Name != "" {
		name = existing.Name
		searchURL = existing.SearchURL
		criteria = existing.Criteria
		excludeKW = strings.Join(existing.Rules.ExcludeKeywords, ", ")
		scanSettings = fmt.Sprintf("%d / %d / %d / %d",
			existing.BackfillIntervalSeconds, existing.MaxDetailPages, existing.WatchIntervalSeconds, existing.WatchPages)
		modalID = "kf:modal:manual_edit"
		title = "Edit Monitor: " + existing.Name
	}

	return &discordgo.InteractionResponseData{
		CustomID: modalID,
		Title:    string([]rune(title)[:min(len([]rune(title)), 45)]),
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{
				discordgo.TextInput{CustomID: "name", Label: "Monitor Name", Style: discordgo.TextInputShort, Value: name, Required: true, MaxLength: 80},
			}},
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{
				discordgo.TextInput{CustomID: "search_url", Label: "Marketplace Search URL", Style: discordgo.TextInputParagraph, Value: searchURL, Required: true},
			}},
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{
				discordgo.TextInput{CustomID: "criteria", Label: "LLM Fit Criteria", Style: discordgo.TextInputParagraph, Value: criteria, Required: true, MaxLength: 4000},
			}},
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{
				discordgo.TextInput{CustomID: "exclude_keywords", Label: "Exclude keywords (comma-separated)", Style: discordgo.TextInputShort, Value: excludeKW, Required: false},
			}},
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{
				discordgo.TextInput{CustomID: "scan_settings", Label: "Backfill / details / watch / pages", Style: discordgo.TextInputShort, Value: scanSettings, Required: true, Placeholder: "300 / 8 / 1800 / 2"},
			}},
		},
	}
}

func aiWizardModal() *discordgo.InteractionResponseData {
	return &discordgo.InteractionResponseData{
		CustomID: "kf:modal:ai_wizard",
		Title:    "AI Monitor Wizard",
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{
				discordgo.TextInput{
					CustomID:    "description",
					Label:       "What are you looking for?",
					Style:       discordgo.TextInputParagraph,
					Placeholder: "E.g. 2-room apartment in Munich under 1000€, no shared bathroom",
					Required:    true,
					MaxLength:   1500,
				},
			}},
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{
				discordgo.TextInput{
					CustomID: "search_url",
					Label:    "Search URL (if you already have one)",
					Style:    discordgo.TextInputParagraph,
					Required: false,
				},
			}},
		},
	}
}

func formatDraftPreview(draft config.MonitorConfig) string {
	lines := []string{
		"Draft monitor ready. Review before saving.",
		"",
		fmt.Sprintf("**Name:** `%s`", draft.Name),
		fmt.Sprintf("**URL:** <%s>", draft.SearchURL),
		fmt.Sprintf("**Mode:** %s | **Backfill:** %ds | **Watch:** %ds/%d page(s) | **Search cap:** %s | **Details:** %v/%d",
			draft.Mode, draft.BackfillIntervalSeconds, draft.WatchIntervalSeconds, draft.WatchPages,
			searchCapLabel(draft.MaxResults), draft.FetchDetails, draft.MaxDetailPages),
		"",
		"**Criteria:**",
		draft.Criteria,
	}
	if len(draft.Rules.ExcludeKeywords) > 0 {
		lines = append(lines, "", "**Exclude:** "+strings.Join(draft.Rules.ExcludeKeywords, ", "))
	}
	if len(draft.Rules.IncludeKeywords) > 0 {
		lines = append(lines, "Required keywords: "+strings.Join(draft.Rules.IncludeKeywords, ", "))
	}
	if len(draft.Rules.IncludeAny) > 0 {
		lines = append(lines, "Any keyword: "+strings.Join(draft.Rules.IncludeAny, ", "))
	}
	if draft.Rules.MinPrice != nil {
		lines = append(lines, fmt.Sprintf("Minimum price: %.2f", *draft.Rules.MinPrice))
	}
	if draft.Rules.MaxPrice != nil {
		lines = append(lines, fmt.Sprintf("Maximum price: %.2f", *draft.Rules.MaxPrice))
	}
	preview := strings.Join(lines, "\n")
	if len(preview) > 1900 {
		preview = string([]rune(preview)[:min(1750, len([]rune(preview)))]) + "\nPreview shortened. The complete criteria and rules are saved."
	}
	return preview
}
