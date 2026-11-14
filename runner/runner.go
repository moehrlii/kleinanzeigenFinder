package runner

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Kamuyin/kleinanzeigenFinder/config"
	"github.com/Kamuyin/kleinanzeigenFinder/llm"
	"github.com/Kamuyin/kleinanzeigenFinder/model"
	"github.com/Kamuyin/kleinanzeigenFinder/notify"
	"github.com/Kamuyin/kleinanzeigenFinder/providers"
	"github.com/Kamuyin/kleinanzeigenFinder/rules"
	"github.com/Kamuyin/kleinanzeigenFinder/storage"
	"github.com/sirupsen/logrus"
)

const (
	monitorModeBackfill = "backfill"
	monitorModeWatch    = "watch"
)

type RunStats struct {
	Scanned  int
	New      int
	Rejected int
	Notified int
	Deferred int
	Errors   int
}

func (s *RunStats) Add(other RunStats) {
	s.Scanned += other.Scanned
	s.New += other.New
	s.Rejected += other.Rejected
	s.Notified += other.Notified
	s.Deferred += other.Deferred
	s.Errors += other.Errors
}

type Runner struct {
	cfg        *config.AppConfig
	seenStore  *storage.SeenStore
	monRepo    *storage.MonitorRepository
	scrapers   map[string]providers.Provider
	classifier *llm.Classifier
	router     *notify.Router
}

type listingWithClassification struct {
	listing        model.Listing
	classification model.Classification
}

func New(cfg *config.AppConfig, seenStore *storage.SeenStore, monRepo *storage.MonitorRepository, activeProviders map[string]providers.Provider, cls *llm.Classifier, router *notify.Router) *Runner {
	return &Runner{
		cfg:        cfg,
		seenStore:  seenStore,
		monRepo:    monRepo,
		scrapers:   activeProviders,
		classifier: cls,
		router:     router,
	}
}

func (r *Runner) RunOnce(ctx context.Context, notify bool) (RunStats, error) {
	var totalStats RunStats
	monitors := r.monRepo.List(false)

	for _, m := range monitors {
		stats, err := r.processMonitor(ctx, m, notify)
		if err != nil {
			logrus.Errorf("Error processing monitor %s: %v", m.Name, err)
			totalStats.Errors++
		}
		totalStats.Add(stats)
	}

	return totalStats, nil
}

func (r *Runner) RunForever(ctx context.Context, notify bool) {
	activeMonitors := make(map[string]context.CancelFunc)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	r.syncMonitors(ctx, activeMonitors, notify)

	for {
		select {
		case <-ctx.Done():
			logrus.Info("Stopping all monitors")
			for _, cancel := range activeMonitors {
				cancel()
			}
			return
		case <-ticker.C:
			r.syncMonitors(ctx, activeMonitors, notify)
		}
	}
}

func (r *Runner) syncMonitors(baseCtx context.Context, active map[string]context.CancelFunc, notify bool) {
	monitors := r.monRepo.List(false)
	currentNames := make(map[string]bool)

	for _, m := range monitors {
		currentNames[m.Name] = true
		if _, exists := active[m.Name]; !exists {
			logrus.Infof("Starting monitor: %s", m.Name)
			mCtx, cancel := context.WithCancel(baseCtx)
			active[m.Name] = cancel
			go r.runMonitorLoop(mCtx, m, notify)
		}
	}

	for name, cancel := range active {
		if !currentNames[name] {
			logrus.Infof("Stopping monitor (disabled or deleted): %s", name)
			cancel()
			delete(active, name)
		}
	}
}

func (r *Runner) runMonitorLoop(ctx context.Context, monitor config.MonitorConfig, notify bool) {
	logrus.Infof("[%s] Monitor started (mode=%s, backfill_interval=%v, watch_interval=%v, watch_pages=%d, max_detail_pages=%d, url=%s)",
		monitor.Name,
		normalizeMode(monitor.Mode),
		intervalDuration(monitor.BackfillIntervalSeconds),
		intervalDuration(monitor.WatchIntervalSeconds),
		monitor.WatchPages,
		monitor.MaxDetailPages,
		monitor.SearchURL,
	)

	for {
		currentCfg := r.monRepo.Get(monitor.Name)
		if currentCfg == nil {
			logrus.Infof("[%s] Monitor no longer exists; stopping loop", monitor.Name)
			return
		}

		if _, err := r.processMonitor(ctx, *currentCfg, notify); err != nil {
			logrus.Errorf("Run error for monitor %s: %v", monitor.Name, err)
		}

		currentCfg = r.monRepo.Get(monitor.Name)
		if currentCfg == nil {
			return
		}
		delay := r.nextDelay(*currentCfg)
		logrus.Debugf("[%s] Next %s run in %v", currentCfg.Name, normalizeMode(currentCfg.Mode), delay)

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (r *Runner) processMonitor(ctx context.Context, monitor config.MonitorConfig, sendNotify bool) (RunStats, error) {
	var stats RunStats
	platform := providers.MonitorPlatform(monitor.Platform, monitor.SearchURL)
	provider, ok := r.scrapers[platform]
	if !ok {
		return stats, fmt.Errorf("unsupported platform %q", platform)
	}
	mode := normalizeMode(monitor.Mode)
	watchPages := monitor.WatchPages
	if watchPages <= 0 {
		watchPages = 2
	}

	maxPages := 0
	if mode == monitorModeWatch {
		maxPages = watchPages
	}

	logrus.Infof("[%s] Discovering listings (mode=%s, max_pages=%s)...", monitor.Name, mode, maxPagesLabel(maxPages))
	listings, err := provider.FetchListingsPages(monitor.SearchURL, monitor.MaxResults, maxPages)
	if err != nil {
		return stats, fmt.Errorf("scraper error: %w", err)
	}
	stats.Scanned = len(listings)

	enqueued := r.monRepo.EnqueueListings(monitor.Name, listings)
	stats.New = enqueued
	logrus.Infof("[%s] Discovered %d listing card(s), queued %d new item(s)", monitor.Name, len(listings), enqueued)

	if err := r.processQueuedListings(ctx, monitor, sendNotify, &stats); err != nil {
		return stats, err
	}

	openQueue := r.monRepo.OpenQueueCount(monitor.Name)
	stats.Deferred = int(openQueue)

	if mode == monitorModeBackfill && openQueue == 0 {
		if err := r.monRepo.SetMode(monitor.Name, monitorModeWatch); err != nil {
			logrus.Warnf("[%s] Could not switch monitor to watch mode: %v", monitor.Name, err)
		} else {
			logrus.Infof("[%s] Backfill completed; switching to watch mode", monitor.Name)
		}
	}

	logrus.Infof("[%s] Done - scanned=%d queued=%d rejected=%d notified=%d backlog=%d",
		monitor.Name, stats.Scanned, stats.New, stats.Rejected, stats.Notified, stats.Deferred)
	return stats, nil
}

func (r *Runner) processQueuedListings(ctx context.Context, monitor config.MonitorConfig, sendNotify bool, stats *RunStats) error {
	queued := r.monRepo.ListQueued(monitor.Name, 0)
	if len(queued) == 0 {
		return nil
	}

	detailBudget := monitor.MaxDetailPages
	if detailBudget <= 0 || detailBudget > len(queued) {
		detailBudget = len(queued)
	}

	var detailCandidates []model.Listing
	for _, listing := range queued {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if r.seenStore.HasSeen(monitor.Name, listing.Identity()) {
			r.monRepo.MarkQueuedProcessed(monitor.Name, listing.Identity())
			continue
		}

		preRules := monitor.Rules
		if monitor.FetchDetails {
			preRules = config.RulesConfig{}
		}
		decision := rules.ApplyRules(listing, preRules)
		if !decision.Accepted {
			logrus.Infof("[%s] Rules rejected id=%s: %s", monitor.Name, listing.Identity(), decision.Reasons[0])
			stats.Rejected++
			_ = r.seenStore.Save(monitor.Name, listing, model.Classification{Suitable: false, Reason: decision.Reasons[0]}, false)
			r.monRepo.MarkQueuedProcessed(monitor.Name, listing.Identity())
			continue
		}

		if len(detailCandidates) < detailBudget {
			detailCandidates = append(detailCandidates, listing)
		}
	}

	if len(detailCandidates) == 0 {
		return nil
	}

	classificationCandidates := detailCandidates
	if monitor.FetchDetails {
		classificationCandidates = r.fetchDetails(ctx, monitor, detailCandidates)
		var ready []model.Listing
		for _, listing := range classificationCandidates {
			if !listing.DetailLoaded {
				logrus.Infof("[%s] Detail not loaded for id=%s; keeping item queued for retry", monitor.Name, listing.Identity())
				r.monRepo.MarkQueuedFailed(monitor.Name, listing.Identity())
				continue
			}

			decision := rules.ApplyRules(listing, monitor.Rules)
			if !decision.Accepted {
				logrus.Infof("[%s] Rules rejected id=%s after detail fetch: %s", monitor.Name, listing.Identity(), decision.Reasons[0])
				stats.Rejected++
				_ = r.seenStore.Save(monitor.Name, listing, model.Classification{Suitable: false, Reason: decision.Reasons[0]}, false)
				r.monRepo.MarkQueuedProcessed(monitor.Name, listing.Identity())
				continue
			}
			ready = append(ready, listing)
		}
		classificationCandidates = ready
	}

	for result := range r.classifyListings(ctx, monitor, classificationCandidates) {
		listing := result.listing
		classification := result.classification
		if listing.Identity() == "" {
			continue
		}

		logrus.Infof("[%s] LLM result id=%s: suitable=%v score=%d reason=%q",
			monitor.Name, listing.Identity(), classification.Suitable, classification.Score, classification.Reason)

		if classification.Error != "" {
			logrus.Warnf("[%s] Assessment pending for %s: %s", monitor.Name, listing.Identity(), classification.Error)
			r.monRepo.MarkQueuedFailed(monitor.Name, listing.Identity())
			stats.Errors++
			continue
		}
		if !classification.Suitable {
			logrus.Infof("[%s] LLM rejected id=%s (score=%d)", monitor.Name, listing.Identity(), classification.Score)
			stats.Rejected++
			_ = r.seenStore.Save(monitor.Name, listing, classification, false)
			r.monRepo.MarkQueuedProcessed(monitor.Name, listing.Identity())
			continue
		}

		notified := false
		if sendNotify {
			logrus.Infof("[%s] Notifying for id=%s (score=%d)", monitor.Name, listing.Identity(), classification.Score)
			notified = r.router.Accepted(ctx, monitor, listing, classification)
			if notified {
				stats.Notified++
				logrus.Infof("[%s] Notification delivered for id=%s", monitor.Name, listing.Identity())
			} else {
				logrus.Warnf("[%s] Notification NOT delivered for id=%s (no notifiers responded)", monitor.Name, listing.Identity())
			}
		}

		_ = r.seenStore.Save(monitor.Name, listing, classification, notified)
		r.monRepo.MarkQueuedProcessed(monitor.Name, listing.Identity())
	}

	return nil
}

func (r *Runner) fetchDetails(ctx context.Context, monitor config.MonitorConfig, listings []model.Listing) []model.Listing {
	platform := providers.MonitorPlatform(monitor.Platform, monitor.SearchURL)
	provider := r.scrapers[platform]
	if !monitor.FetchDetails || len(listings) == 0 {
		return listings
	}

	limit := len(listings)
	concurrency := boundedConcurrency(r.cfg.Scraper.DetailConcurrency, limit, 3)
	if concurrency <= 1 {
		for i := 0; i < limit; i++ {
			select {
			case <-ctx.Done():
				return listings
			default:
			}

			listing := listings[i]
			logrus.Debugf("[%s] Fetching detail page for id=%s url=%s", monitor.Name, listing.Identity(), listing.URL)
			detail, err := provider.FetchDetail(listing)
			if err == nil {
				listings[i] = detail
				logrus.Debugf("[%s] Detail loaded for id=%s", monitor.Name, detail.Identity())
			} else {
				logrus.Warnf("[%s] Could not fetch detail for id=%s: %v", monitor.Name, listing.Identity(), err)
			}
		}
		return listings
	}

	logrus.Infof("[%s] Fetching %d detail page(s) with concurrency=%d and delay=%.2fs",
		monitor.Name, limit, concurrency, r.cfg.Scraper.DetailDelaySeconds)

	jobs := make(chan int)
	var wg sync.WaitGroup

	for range concurrency {
		wg.Go(func() {
			for index := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}

				listing := listings[index]
				logrus.Debugf("[%s] Fetching detail page for id=%s url=%s", monitor.Name, listing.Identity(), listing.URL)
				detail, err := provider.FetchDetail(listing)
				if err == nil {
					listings[index] = detail
					logrus.Debugf("[%s] Detail loaded for id=%s", monitor.Name, detail.Identity())
				} else {
					logrus.Warnf("[%s] Could not fetch detail for id=%s: %v", monitor.Name, listing.Identity(), err)
				}
			}
		})
	}

	for i := 0; i < limit; i++ {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return listings
		case jobs <- i:
		}
	}
	close(jobs)
	wg.Wait()

	return listings
}

func (r *Runner) classifyListings(ctx context.Context, monitor config.MonitorConfig, listings []model.Listing) <-chan listingWithClassification {
	results := make(chan listingWithClassification)

	go func() {
		defer close(results)
		if len(listings) == 0 {
			return
		}

		concurrency := boundedConcurrency(r.cfg.LLM.ClassificationConcurrency, len(listings), 8)
		if concurrency <= 1 {
			for _, listing := range listings {
				select {
				case <-ctx.Done():
					return
				default:
				}

				logrus.Infof("[%s] Sending id=%s to LLM for classification...", monitor.Name, listing.Identity())
				result := listingWithClassification{
					listing:        listing,
					classification: r.classifier.Classify(ctx, listing, monitor),
				}

				select {
				case <-ctx.Done():
					return
				case results <- result:
				}
			}
			return
		}

		logrus.Infof("[%s] Classifying %d listing(s) with LLM concurrency=%d", monitor.Name, len(listings), concurrency)

		jobs := make(chan model.Listing)
		var wg sync.WaitGroup

		for range concurrency {
			wg.Go(func() {
				for listing := range jobs {
					select {
					case <-ctx.Done():
						return
					default:
					}

					logrus.Infof("[%s] Sending id=%s to LLM for classification...", monitor.Name, listing.Identity())
					result := listingWithClassification{
						listing:        listing,
						classification: r.classifier.Classify(ctx, listing, monitor),
					}

					select {
					case <-ctx.Done():
						return
					case results <- result:
					}
				}
			})
		}

		for _, listing := range listings {
			select {
			case <-ctx.Done():
				close(jobs)
				wg.Wait()
				return
			case jobs <- listing:
			}
		}
		close(jobs)
		wg.Wait()
	}()

	return results
}

func (r *Runner) nextDelay(monitor config.MonitorConfig) time.Duration {
	if normalizeMode(monitor.Mode) == monitorModeWatch {
		return intervalDuration(monitor.WatchIntervalSeconds)
	}
	return intervalDuration(monitor.BackfillIntervalSeconds)
}

func normalizeMode(mode string) string {
	if mode == monitorModeWatch {
		return monitorModeWatch
	}
	return monitorModeBackfill
}

func intervalDuration(seconds int) time.Duration {
	if seconds < 60 {
		seconds = 60
	}
	return time.Duration(seconds) * time.Second
}

func maxPagesLabel(maxPages int) string {
	if maxPages <= 0 {
		return "all"
	}
	return fmt.Sprintf("%d", maxPages)
}

func boundedConcurrency(configured, itemCount, hardMax int) int {
	if itemCount <= 0 {
		return 0
	}
	if configured <= 0 {
		configured = 1
	}
	if hardMax > 0 && configured > hardMax {
		configured = hardMax
	}
	if configured > itemCount {
		configured = itemCount
	}
	return configured
}
