package ebay

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// Start a real browser process with an isolated profile, then attach via loopback CDP.
// This avoids Playwright launch flags and UA overrides. No user profile is imported.
func (s *Scraper) startBrowser() (err error) {
	if s.closed {
		return fmt.Errorf("eBay provider is closed")
	}
	if s.page != nil {
		return nil
	}
	defer func() {
		if err != nil {
			s.closeBrowser()
		}
	}()
	if err = playwright.Install(&playwright.RunOptions{SkipInstallBrowsers: true}); err != nil {
		return fmt.Errorf("install eBay browser driver: %w", err)
	}
	s.pw, err = playwright.Run()
	if err != nil {
		return err
	}
	executable := s.settings.BrowserExecutable
	if executable == "" {
		executable = installedChrome()
	}
	if executable == "" {
		if err = playwright.Install(&playwright.RunOptions{Browsers: []string{"chromium"}}); err != nil {
			return err
		}
		executable = s.pw.Chromium.ExecutablePath()
	}
	profile := s.settings.ProfileDir
	if profile == "" {
		profile, err = os.MkdirTemp("", "kf-ebay-")
		if err != nil {
			return err
		}
		s.temporaryProfile = profile
	}
	profile, err = filepath.Abs(profile)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(profile, 0700); err != nil {
		return err
	}
	// Claim the dedicated profile before starting Chrome: another process must not
	// silently hand this session to an existing Chrome process using the same profile.
	lock, err := os.OpenFile(filepath.Join(profile, "kf-provider.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("eBay profile is in use or unavailable (%s): %w", profile, err)
	}
	_ = lock.Close()
	s.profileLock = filepath.Join(profile, "kf-provider.lock")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	args := []string{
		"--user-data-dir=" + profile, fmt.Sprintf("--remote-debugging-port=%d", port),
		"--remote-debugging-address=127.0.0.1", "--no-first-run", "--no-default-browser-check",
	}
	if s.settings.Headless {
		args = append(args, "--headless=new")
	}
	// Chromium cannot start as root with its sandbox. This is only for root-run
	// containers, matching Playwright's existing container behavior.
	if runtime.GOOS == "linux" && os.Geteuid() == 0 {
		args = append(args, "--no-sandbox")
	}
	s.process = exec.Command(executable, append(args, "about:blank")...)
	if err = s.process.Start(); err != nil {
		return fmt.Errorf("launch eBay browser: %w", err)
	}
	s.processDone = make(chan struct{})
	go func() { _ = s.process.Wait(); close(s.processDone) }()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for {
		s.browser, err = s.pw.Chromium.ConnectOverCDP(fmt.Sprintf("http://127.0.0.1:%d", port), playwright.BrowserTypeConnectOverCDPOptions{Timeout: new(1000.0)})
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("eBay browser did not start; headed Linux requires DISPLAY (use xvfb-run): %w", err)
		case <-s.processDone:
			return fmt.Errorf("eBay browser exited before CDP became available; check executable, profile, and DISPLAY")
		case <-time.After(200 * time.Millisecond):
		}
	}
	contexts := s.browser.Contexts()
	if len(contexts) != 1 {
		return fmt.Errorf("expected one isolated eBay browser context")
	}
	s.browserContext = contexts[0]
	s.page, err = s.browserContext.NewPage()
	if err == nil {
		s.page.SetDefaultTimeout(float64(s.config.TimeoutMS))
	}
	return err
}

func installedChrome() string {
	var candidates []string
	switch runtime.GOOS {
	case "windows":
		for _, root := range []string{os.Getenv("PROGRAMFILES"), os.Getenv("PROGRAMFILES(X86)"), os.Getenv("LOCALAPPDATA")} {
			if root != "" {
				candidates = append(candidates, filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe"))
			}
		}
	case "darwin":
		candidates = []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"}
	default:
		for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
			if path, err := exec.LookPath(name); err == nil {
				return path
			}
		}
	}
	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func (s *Scraper) closeBrowser() {
	if s.browser != nil {
		// Closing a CDP connection alone only detaches. Ask our owned process to
		// exit cleanly so a configured profile can flush its session to disk.
		if s.process != nil {
			if session, err := s.browser.NewBrowserCDPSession(); err == nil {
				_, _ = session.Send("Browser.close", nil)
			}
		}
		_ = s.browser.Close()
	}
	if s.process != nil && s.process.Process != nil {
		select {
		case <-s.processDone:
		case <-time.After(2 * time.Second):
			_ = s.process.Process.Kill()
			<-s.processDone
		}
	}
	if s.pw != nil {
		_ = s.pw.Stop()
	}
	if s.profileLock != "" {
		_ = os.Remove(s.profileLock)
	}
	if s.temporaryProfile != "" {
		_ = os.RemoveAll(s.temporaryProfile)
	}
	s.page, s.browser, s.browserContext, s.pw = nil, nil, nil, nil
	s.process, s.processDone, s.profileLock, s.temporaryProfile = nil, nil, "", ""
}
