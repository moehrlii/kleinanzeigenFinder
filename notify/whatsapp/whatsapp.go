package whatsapp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kamuyin/kleinanzeigenFinder/config"
	"github.com/Kamuyin/kleinanzeigenFinder/model"
	_ "github.com/glebarez/go-sqlite"
	"github.com/mdp/qrterminal/v3"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

type WhatsAppNotifier struct {
	settings config.WhatsAppSettings
	client   *whatsmeow.Client
}

// logAdapter wraps logrus to satisfy whatsmeow's waLog.Logger interface...
type logAdapter struct {
	module string
}

func (l *logAdapter) Errorf(msg string, args ...interface{}) {
	logrus.Errorf("["+l.module+"] "+msg, args...)
}
func (l *logAdapter) Warnf(msg string, args ...interface{}) {
	logrus.Warnf("["+l.module+"] "+msg, args...)
}
func (l *logAdapter) Infof(msg string, args ...interface{}) {
	logrus.Infof("["+l.module+"] "+msg, args...)
}
func (l *logAdapter) Debugf(msg string, args ...interface{}) {
	logrus.Debugf("["+l.module+"] "+msg, args...)
}
func (l *logAdapter) Sub(module string) waLog.Logger {
	return &logAdapter{module: l.module + "." + module}
}

func NewWhatsAppNotifier(settings config.WhatsAppSettings) (*WhatsAppNotifier, error) {
	if !settings.Enabled {
		return &WhatsAppNotifier{settings: settings}, nil
	}

	sessionDB := settings.SessionDB
	if sessionDB == "" {
		sessionDB = "data/whatsapp_session.sqlite3"
	}
	if err := os.MkdirAll(filepath.Dir(sessionDB), 0755); err != nil {
		return nil, fmt.Errorf("could not create whatsapp session dir: %w", err)
	}

	dbLog := &logAdapter{module: "wa-db"}
	container, err := sqlstore.New(context.Background(), "sqlite", "file:"+sessionDB+"?_pragma=foreign_keys(1)", dbLog)
	if err != nil {
		return nil, fmt.Errorf("whatsapp db error: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return nil, fmt.Errorf("whatsapp device error: %w", err)
	}

	clientLog := &logAdapter{module: "wa-client"}
	client := whatsmeow.NewClient(deviceStore, clientLog)

	if client.Store.ID == nil {
		qrChan, _ := client.GetQRChannel(context.Background())
		err = client.Connect()
		if err != nil {
			return nil, fmt.Errorf("whatsapp connect error: %w", err)
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
				fmt.Println()
				fmt.Println("Waiting for scan...")
			} else {
				logrus.Infof("WhatsApp login event: %s", evt.Event)
			}
		}
		logrus.Infof("WhatsApp linked successfully!")
	} else {
		err = client.Connect()
		if err != nil {
			return nil, fmt.Errorf("whatsapp reconnect error: %w", err)
		}
		logrus.Infof("WhatsApp connected (already linked).")
	}

	return &WhatsAppNotifier{
		settings: settings,
		client:   client,
	}, nil
}

func (w *WhatsAppNotifier) NotifyAccepted(ctx context.Context, monitor config.MonitorConfig, listing model.Listing, classification model.Classification) (bool, error) {
	if !w.settings.Enabled || w.client == nil {
		return false, nil
	}
	if w.client.Store.ID == nil {
		return false, fmt.Errorf("whatsapp not linked")
	}

	msgText := fmt.Sprintf("✅ *%s*\n\n", escape(listing.Title))
	if listing.Price != "" {
		msgText += fmt.Sprintf("💰 %s\n", escape(listing.Price))
	}
	if listing.Location != "" {
		msgText += fmt.Sprintf("📍 %s\n", escape(listing.Location))
	}

	msgText += fmt.Sprintf("\n%s\n\n", escape(listing.URL))

	if classification.AssessmentText() != "" {
		msgText += fmt.Sprintf("🤖 *Score %d/100*\n_%s_\n", classification.Score, escape(classification.AssessmentText()))
	}

	var targetJID types.JID
	if w.settings.TargetPhone != "" {
		targetJID = types.NewJID(w.settings.TargetPhone, types.DefaultUserServer)
	} else {
		targetJID = w.client.Store.ID.ToNonAD()
	}

	msg := &waE2E.Message{
		Conversation: proto.String(msgText),
	}

	_, err := w.client.SendMessage(ctx, targetJID, msg)
	if err != nil {
		return false, fmt.Errorf("whatsapp send error: %w", err)
	}

	return true, nil
}

func (w *WhatsAppNotifier) NotifyRejected(ctx context.Context, monitor config.MonitorConfig, listing model.Listing, classification model.Classification) (bool, error) {
	// Let's not spam ourselves with rejections via WhatsApp
	return false, nil
}

func escape(text string) string {
	// WhatsApp markdown doesn't strictly need escaping except avoiding unintended formatting
	// but trimming length is good
	if len(text) > 1000 {
		return text[:997] + "..."
	}
	return strings.TrimSpace(text)
}
