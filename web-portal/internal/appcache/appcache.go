// Package appcache caches a hostable Android APK for the portal to serve.
package appcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	settingsFile = "settings.json"
	metaFile     = "meta.json"
	apkFile      = "latest.apk"
	tmpSuffix    = ".tmp"

	defaultPackageID     = "org.snikket.android"
	defaultDownloadLimit = 6
	defaultRefreshHours  = 12
	defaultMaxAPKBytes   = 200 << 20
	fetchTimeout         = 10 * time.Minute
)

// Settings controls whether the portal hosts an Android APK and where it comes from.
type Settings struct {
	Enabled              bool   `json:"enabled"`
	SourceURL            string `json:"source_url"`
	PackageID            string `json:"package_id"`
	PlayStoreURL         string `json:"play_store_url"`
	FDroidURL            string `json:"fdroid_url"`
	DownloadLimitPerHour int    `json:"download_limit_per_hour"`
	RefreshIntervalHours int    `json:"refresh_interval_hours"`
}

// Meta describes the currently cached APK.
type Meta struct {
	Version   string    `json:"version,omitempty"`
	Filename  string    `json:"filename,omitempty"`
	Size      int64     `json:"size"`
	SHA256    string    `json:"sha256,omitempty"`
	SourceURL string    `json:"source_url,omitempty"`
	FetchedAt time.Time `json:"fetched_at,omitempty"`
	ETag      string    `json:"etag,omitempty"`
	LastError string    `json:"last_error,omitempty"`
}

// Defaults seeds settings from environment style values at process start.
type Defaults struct {
	Enabled              bool
	SourceURL            string
	PackageID            string
	PlayStoreURL         string
	FDroidURL            string
	DownloadLimitPerHour int
	RefreshIntervalHours int
}

// Cache stores settings and a single latest APK under a state directory.
type Cache struct {
	dir    string
	client *http.Client

	mu       sync.RWMutex
	settings Settings
	meta     Meta
}

// Open loads or creates cache state under dir.
func Open(dir string, defaults Defaults) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	c := &Cache{
		dir: dir,
		client: &http.Client{
			Timeout: fetchTimeout,
		},
		settings: normalizeSettings(Settings{
			Enabled:              defaults.Enabled,
			SourceURL:            strings.TrimSpace(defaults.SourceURL),
			PackageID:            strings.TrimSpace(defaults.PackageID),
			PlayStoreURL:         strings.TrimSpace(defaults.PlayStoreURL),
			FDroidURL:            strings.TrimSpace(defaults.FDroidURL),
			DownloadLimitPerHour: defaults.DownloadLimitPerHour,
			RefreshIntervalHours: defaults.RefreshIntervalHours,
		}),
	}
	if err := c.loadSettings(); err != nil {
		return nil, err
	}
	_ = c.loadMeta()
	return c, nil
}

// Settings returns a copy of the current settings.
func (c *Cache) Settings() Settings {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.settings
}

// Meta returns a copy of the current cache metadata.
func (c *Cache) Meta() Meta {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.meta
}

// Ready reports whether a downloadable APK is available to the public.
func (c *Cache) Ready() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.settings.Enabled {
		return false
	}
	if c.meta.Size <= 0 || c.meta.SHA256 == "" {
		return false
	}
	_, err := os.Stat(c.apkPath())
	return err == nil
}

// SaveSettings persists operator settings and normalizes fields.
func (c *Cache) SaveSettings(next Settings) error {
	next = normalizeSettings(next)
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(c.dir, settingsFile)
	tmp := path + tmpSuffix
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	c.mu.Lock()
	c.settings = next
	c.mu.Unlock()
	return nil
}

// Clear removes the cached APK and metadata while keeping settings.
func (c *Cache) Clear() error {
	_ = os.Remove(c.apkPath())
	_ = os.Remove(filepath.Join(c.dir, metaFile))
	c.mu.Lock()
	c.meta = Meta{}
	c.mu.Unlock()
	return nil
}

// StartLoop refreshes the APK on an interval while enabled.
func (c *Cache) StartLoop(ctx context.Context) {
	go func() {
		c.maybeRefresh(ctx)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.maybeRefresh(ctx)
			}
		}
	}()
}

func (c *Cache) maybeRefresh(ctx context.Context) {
	c.mu.RLock()
	enabled := c.settings.Enabled
	source := c.settings.SourceURL
	interval := time.Duration(c.settings.RefreshIntervalHours) * time.Hour
	fetched := c.meta.FetchedAt
	c.mu.RUnlock()
	if !enabled || source == "" {
		return
	}
	if !fetched.IsZero() && time.Since(fetched) < interval {
		return
	}
	if err := c.Refresh(ctx); err != nil {
		slog.Warn("android apk refresh failed", slog.String("error", err.Error()))
	}
}

// Refresh downloads the configured source into the local cache.
func (c *Cache) Refresh(ctx context.Context) error {
	c.mu.RLock()
	source := strings.TrimSpace(c.settings.SourceURL)
	c.mu.RUnlock()
	if source == "" {
		return errors.New("android apk source url is empty")
	}
	downloadURL, version, err := c.resolveSource(ctx, source)
	if err != nil {
		c.setLastError(err.Error())
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		c.setLastError(err.Error())
		return err
	}
	req.Header.Set("User-Agent", "snikketx-portal-appcache/1.0")
	c.mu.RLock()
	etag := c.meta.ETag
	c.mu.RUnlock()
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		c.setLastError(err.Error())
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		c.mu.Lock()
		c.meta.FetchedAt = time.Now().UTC()
		c.meta.LastError = ""
		meta := c.meta
		c.mu.Unlock()
		_ = c.writeMeta(meta)
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("download returned HTTP %d", resp.StatusCode)
		c.setLastError(err.Error())
		return err
	}
	if resp.ContentLength > defaultMaxAPKBytes {
		err := fmt.Errorf("apk larger than %d bytes", defaultMaxAPKBytes)
		c.setLastError(err.Error())
		return err
	}
	filename := filepath.Base(strings.Split(downloadURL, "?")[0])
	if filename == "" || filename == "." || filename == "/" {
		filename = apkFile
	}
	if !strings.HasSuffix(strings.ToLower(filename), ".apk") {
		filename += ".apk"
	}
	meta, err := c.storeBody(resp.Body, Meta{
		Version:   version,
		Filename:  filename,
		SourceURL: downloadURL,
		ETag:      strings.TrimSpace(resp.Header.Get("ETag")),
		FetchedAt: time.Now().UTC(),
	})
	if err != nil {
		c.setLastError(err.Error())
		return err
	}
	c.mu.Lock()
	c.meta = meta
	c.mu.Unlock()
	return nil
}

// StoreUpload writes an operator uploaded APK into the cache.
func (c *Cache) StoreUpload(r io.Reader, filename string) (Meta, error) {
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "" || filename == "." {
		filename = "upload.apk"
	}
	if !strings.HasSuffix(strings.ToLower(filename), ".apk") {
		filename += ".apk"
	}
	meta, err := c.storeBody(r, Meta{
		Filename:  filename,
		SourceURL: "upload",
		FetchedAt: time.Now().UTC(),
	})
	if err != nil {
		c.setLastError(err.Error())
		return Meta{}, err
	}
	c.mu.Lock()
	c.meta = meta
	c.mu.Unlock()
	return meta, nil
}

// OpenAPK opens the cached APK for serving. Caller must close the file.
func (c *Cache) OpenAPK() (*os.File, Meta, error) {
	c.mu.RLock()
	meta := c.meta
	enabled := c.settings.Enabled
	c.mu.RUnlock()
	if !enabled {
		return nil, Meta{}, errors.New("android apk hosting is disabled")
	}
	// #nosec G304 -- path is under the controlled state directory
	f, err := os.Open(c.apkPath())
	if err != nil {
		return nil, Meta{}, err
	}
	return f, meta, nil
}

// PlayURL returns the Play Store listing URL for the configured package.
func (c *Cache) PlayURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if u := strings.TrimSpace(c.settings.PlayStoreURL); u != "" {
		return u
	}
	pkg := c.settings.PackageID
	if pkg == "" {
		pkg = defaultPackageID
	}
	return "https://play.google.com/store/apps/details?id=" + url.QueryEscape(pkg)
}

// FDroidMarketURL returns a market:// URL for F-Droid clients.
func (c *Cache) FDroidMarketURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if u := strings.TrimSpace(c.settings.FDroidURL); u != "" {
		return u
	}
	pkg := c.settings.PackageID
	if pkg == "" {
		pkg = defaultPackageID
	}
	return "market://details?id=" + pkg
}

// FDroidWebURL returns the https F-Droid package page.
func (c *Cache) FDroidWebURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if u := strings.TrimSpace(c.settings.FDroidURL); u != "" && strings.HasPrefix(u, "http") {
		return u
	}
	pkg := c.settings.PackageID
	if pkg == "" {
		pkg = defaultPackageID
	}
	return "https://f-droid.org/packages/" + pkg + "/"
}

// PackageID returns the configured Android application id.
func (c *Cache) PackageID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.settings.PackageID == "" {
		return defaultPackageID
	}
	return c.settings.PackageID
}

// DownloadLimit returns the per IP hourly download cap.
func (c *Cache) DownloadLimit() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.settings.DownloadLimitPerHour < 1 {
		return defaultDownloadLimit
	}
	return c.settings.DownloadLimitPerHour
}

func (c *Cache) apkPath() string {
	return filepath.Join(c.dir, apkFile)
}

func (c *Cache) loadSettings() error {
	path := filepath.Join(c.dir, settingsFile)
	// #nosec G304 -- path is under the controlled state directory
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c.SaveSettings(c.settings)
		}
		return err
	}
	var next Settings
	if err := json.Unmarshal(data, &next); err != nil {
		return err
	}
	c.mu.Lock()
	c.settings = normalizeSettings(next)
	c.mu.Unlock()
	return nil
}

func (c *Cache) loadMeta() error {
	path := filepath.Join(c.dir, metaFile)
	// #nosec G304 -- path is under the controlled state directory
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		return err
	}
	c.mu.Lock()
	c.meta = meta
	c.mu.Unlock()
	return nil
}

func (c *Cache) writeMeta(meta Meta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(c.dir, metaFile)
	tmp := path + tmpSuffix
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c *Cache) setLastError(msg string) {
	c.mu.Lock()
	c.meta.LastError = msg
	meta := c.meta
	c.mu.Unlock()
	_ = c.writeMeta(meta)
}

func (c *Cache) storeBody(r io.Reader, seed Meta) (Meta, error) {
	tmp := c.apkPath() + tmpSuffix
	// #nosec G304 -- path is under the controlled state directory
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return Meta{}, err
	}
	hash := sha256.New()
	limited := io.LimitReader(r, defaultMaxAPKBytes+1)
	n, err := io.Copy(io.MultiWriter(f, hash), limited)
	closeErr := f.Close()
	if err != nil {
		_ = os.Remove(tmp)
		return Meta{}, err
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return Meta{}, closeErr
	}
	if n > defaultMaxAPKBytes {
		_ = os.Remove(tmp)
		return Meta{}, fmt.Errorf("apk larger than %d bytes", defaultMaxAPKBytes)
	}
	if n < 1024 {
		_ = os.Remove(tmp)
		return Meta{}, errors.New("apk payload too small")
	}
	if err := os.Rename(tmp, c.apkPath()); err != nil {
		_ = os.Remove(tmp)
		return Meta{}, err
	}
	seed.Size = n
	seed.SHA256 = hex.EncodeToString(hash.Sum(nil))
	seed.LastError = ""
	if seed.FetchedAt.IsZero() {
		seed.FetchedAt = time.Now().UTC()
	}
	if err := c.writeMeta(seed); err != nil {
		return Meta{}, err
	}
	return seed, nil
}

func normalizeSettings(s Settings) Settings {
	s.SourceURL = strings.TrimSpace(s.SourceURL)
	s.PackageID = strings.TrimSpace(s.PackageID)
	s.PlayStoreURL = strings.TrimSpace(s.PlayStoreURL)
	s.FDroidURL = strings.TrimSpace(s.FDroidURL)
	if s.PackageID == "" {
		s.PackageID = defaultPackageID
	}
	if s.DownloadLimitPerHour < 1 {
		s.DownloadLimitPerHour = defaultDownloadLimit
	}
	if s.DownloadLimitPerHour > 100 {
		s.DownloadLimitPerHour = 100
	}
	if s.RefreshIntervalHours < 1 {
		s.RefreshIntervalHours = defaultRefreshHours
	}
	if s.RefreshIntervalHours > 168 {
		s.RefreshIntervalHours = 168
	}
	return s
}

func (c *Cache) resolveSource(ctx context.Context, source string) (downloadURL, version string, err error) {
	owner, repo, ok := parseGitHubSource(source)
	if !ok {
		if err := validateHTTPURL(source); err != nil {
			return "", "", err
		}
		return source, "", nil
	}
	api := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "snikketx-portal-appcache/1.0")
	resp, err := c.client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("github releases returned HTTP %d", resp.StatusCode)
	}
	var body struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", "", err
	}
	var chosen string
	for _, asset := range body.Assets {
		name := strings.ToLower(asset.Name)
		if !strings.HasSuffix(name, ".apk") {
			continue
		}
		if strings.Contains(name, "arm64") || strings.Contains(name, "aarch64") {
			return asset.BrowserDownloadURL, body.TagName, nil
		}
		if chosen == "" {
			chosen = asset.BrowserDownloadURL
		}
	}
	if chosen == "" {
		return "", "", errors.New("github release has no apk assets")
	}
	return chosen, body.TagName, nil
}

func parseGitHubSource(source string) (owner, repo string, ok bool) {
	source = strings.TrimSpace(source)
	if strings.HasPrefix(source, "github:") {
		parts := strings.Split(strings.TrimPrefix(source, "github:"), "/")
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			return parts[0], parts[1], true
		}
		return "", "", false
	}
	u, err := url.Parse(source)
	if err != nil {
		return "", "", false
	}
	if u.Host != "github.com" && u.Host != "www.github.com" {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", false
	}
	if len(parts) >= 4 && parts[2] == "releases" {
		return parts[0], parts[1], true
	}
	if len(parts) == 2 {
		return parts[0], parts[1], true
	}
	return "", "", false
}

func validateHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("source url must be http or https")
	}
	if u.Host == "" {
		return errors.New("source url host is required")
	}
	return nil
}
