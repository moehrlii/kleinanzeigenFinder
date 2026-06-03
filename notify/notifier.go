package notify

import (
	"context"

	"github.com/Kamuyin/kleinanzeigenFinder/config"
	"github.com/Kamuyin/kleinanzeigenFinder/model"
	"github.com/sirupsen/logrus"
)

type ListingNotifier interface {
	NotifyAccepted(ctx context.Context, monitor config.MonitorConfig, listing model.Listing, classification model.Classification) (bool, error)
	NotifyRejected(ctx context.Context, monitor config.MonitorConfig, listing model.Listing, classification model.Classification) (bool, error)
}

type Router struct {
	Telegram ListingNotifier
	Discord  ListingNotifier
	WhatsApp ListingNotifier
}

func (r *Router) Accepted(ctx context.Context, monitor config.MonitorConfig, listing model.Listing, classification model.Classification) bool {
	delivered := false

	if r.Telegram != nil {
		if ok, err := r.Telegram.NotifyAccepted(ctx, monitor, listing, classification); err == nil && ok {
			delivered = true
		} else if err != nil {
			logrus.Warnf("Telegram Accepted notify error: %v", err)
		}
	}

	if r.Discord != nil {
		if ok, err := r.Discord.NotifyAccepted(ctx, monitor, listing, classification); err == nil && ok {
			delivered = true
		} else if err != nil {
			logrus.Warnf("Discord Accepted notify error: %v", err)
		}
	}

	if r.WhatsApp != nil {
		if ok, err := r.WhatsApp.NotifyAccepted(ctx, monitor, listing, classification); err == nil && ok {
			delivered = true
		} else if err != nil {
			logrus.Warnf("WhatsApp Accepted notify error: %v", err)
		}
	}

	logrus.Debugf("Accepted notification delivered=%v listing_id=%s", delivered, listing.Identity())
	return delivered
}

func (r *Router) Rejected(ctx context.Context, monitor config.MonitorConfig, listing model.Listing, classification model.Classification) bool {
	delivered := false

	if r.Discord != nil {
		if ok, err := r.Discord.NotifyRejected(ctx, monitor, listing, classification); err == nil && ok {
			delivered = true
		} else if err != nil {
			logrus.Warnf("Discord Rejected notify error: %v", err)
		}
	}

	if r.WhatsApp != nil {
		if ok, err := r.WhatsApp.NotifyRejected(ctx, monitor, listing, classification); err == nil && ok {
			delivered = true
		} else if err != nil {
			logrus.Warnf("WhatsApp Rejected notify error: %v", err)
		}
	}

	logrus.Debugf("Rejected notification delivered=%v listing_id=%s", delivered, listing.Identity())
	return delivered
}
