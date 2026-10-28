package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-reader/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-reader/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-reader/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-reader/internal/store"
)

// ---- settings ----

func mask(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "••••"
	}
	return s[:4] + "••••" + s[len(s)-4:]
}

// publicSettings is what one person sees: their settings with the secrets
// masked and, per Pocket app, the mode in effect (<app>_mode), whether it
// works and how (<app>_via: core | direct | "") and why not (<app>_detail).
func (s *Server) publicSettings(ctx context.Context, u *store.User, st store.Settings) map[string]any {
	out := map[string]any{
		"core_configured": s.core.Configured(), "core_registered": s.coreStatus()["registered"],
		"title": st.Title, "date_format": st.DateFormat, "time_format": st.TimeFormat, "week_start": st.WeekStart,
		"font": st.Font, "font_size": st.FontSize, "line_width": st.LineWidth, "line_height": st.LineHeight, "reader_theme": st.ReaderTheme, "justify": st.Justify,
		"mark_on_open": st.MarkOnOpen, "mark_on_scroll": st.MarkOnScroll, "sort_oldest": st.SortOldest, "list_style": st.ListStyle, "show_images": st.ShowImages,
		"open_original": st.OpenOriginal, "hide_read": st.HideRead, "confirm_all": st.ConfirmAll, "auto_extract": st.AutoExtract,
		"refresh_min": st.RefreshMin, "keep_read_days": st.KeepReadDays, "keep_unread_days": st.KeepUnreadDays,
		"notify_url": st.NotifyURL, "notify_format": st.NotifyFormat, "notify_configured": st.NotifyURL != "",
		"notify_new_posts": st.NotifyNewPosts, "digest_enabled": st.DigestEnabled, "digest_hour": st.DigestHour, "digest_days": st.DigestDays, "notify_feed_errors": st.NotifyFeedError,
		"notes_url": st.NotesURL, "notes_token": mask(st.NotesToken), "notes_public_url": st.NotesPublicURL, "notes_folder": st.NotesFolder, "notes_auto": st.NotesAuto,
		"ai_provider": st.AIProvider, "ai_pocket_model": st.AIPocketModel, "openai_api_key": mask(st.OpenAIKey), "openai_model": st.OpenAIModel, "openai_base_url": st.OpenAIBaseURL,
		"ai_auto_tag": st.AIAutoTag, "ai_language": st.AILanguage,
		"setup_complete": st.SetupComplete, "base_url": s.cfg.BaseURL, "fetch_private": s.cfg.FetchPrivate,
	}
	for _, a := range pocketApps {
		mode, via, detail := s.connStatus(ctx, u, st, a)
		out[a.slug+"_mode"], out[a.slug+"_via"], out[a.slug+"_detail"] = mode, via, detail
		out[a.slug+"_configured"] = via != ""
		_, _, _, public := connSettings(st, a)
		if public == "" && via != "" {
			public = s.resolveSibling(ctx, a.slug)
		}
		out[a.slug+"_open_url"] = public
	}
	return out
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), userFrom(r.Context()), st))
}

// handlePutSettings saves the caller's personal settings. The box-wide title
// is only taken from an admin.
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	me := userFrom(r.Context())
	isAdmin := me.Role == "admin"
	cur, err := s.store.GetSettings(r.Context(), me.ID)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	var in store.Settings
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	bad := func(msg string) { writeErr(w, http.StatusBadRequest, msg) }
	if keys["title"] && isAdmin {
		cur.Title = strings.TrimSpace(in.Title)
		if cur.Title == "" {
			cur.Title = store.DefaultTitle
		}
	}
	if keys["date_format"] {
		cur.DateFormat = in.DateFormat
	}
	if keys["time_format"] {
		cur.TimeFormat = in.TimeFormat
	}
	if keys["week_start"] && (in.WeekStart == 1 || in.WeekStart == 7 || in.WeekStart == 6) {
		cur.WeekStart = in.WeekStart
	}
	intIn := func(k string, dst *int, v, lo, hi int) bool {
		if !keys[k] {
			return true
		}
		if v < lo || v > hi {
			bad(fmt.Sprintf("%s is %d to %d", k, lo, hi))
			return false
		}
		*dst = v
		return true
	}
	if !intIn("font_size", &cur.FontSize, in.FontSize, 14, 28) || !intIn("refresh_min", &cur.RefreshMin, in.RefreshMin, 10, 1440) ||
		!intIn("keep_read_days", &cur.KeepReadDays, in.KeepReadDays, 0, 3650) || !intIn("keep_unread_days", &cur.KeepUnreadDays, in.KeepUnreadDays, 0, 3650) ||
		!intIn("digest_hour", &cur.DigestHour, in.DigestHour, 0, 23) {
		return
	}
	pick := func(k string, dst *string, v string, allowed []string) bool {
		if !keys[k] {
			return true
		}
		for _, a := range allowed {
			if v == a {
				*dst = v
				return true
			}
		}
		bad(fmt.Sprintf("%s must be one of %s", k, strings.Join(allowed, ", ")))
		return false
	}
	if !pick("font", &cur.Font, in.Font, []string{"serif", "sans", "mono"}) || !pick("line_width", &cur.LineWidth, in.LineWidth, []string{"narrow", "medium", "wide"}) ||
		!pick("line_height", &cur.LineHeight, in.LineHeight, []string{"tight", "normal", "loose"}) ||
		!pick("reader_theme", &cur.ReaderTheme, in.ReaderTheme, []string{"app", "paper", "sepia", "night"}) ||
		!pick("list_style", &cur.ListStyle, in.ListStyle, []string{"cards", "compact", "magazine"}) {
		return
	}
	if keys["digest_days"] {
		d := ""
		for _, c := range "1234567" {
			if strings.ContainsRune(in.DigestDays, c) {
				d += string(c)
			}
		}
		cur.DigestDays = d
	}
	secret := func(k string, dst *string, v string) {
		if keys[k] && !strings.Contains(v, "••") {
			*dst = strings.TrimSpace(v)
		}
	}
	url := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimRight(strings.TrimSpace(v), "/")
		}
	}
	text := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimSpace(v)
		}
	}
	boolIn := func(k string, dst *bool, v bool) {
		if keys[k] {
			*dst = v
		}
	}
	boolIn("justify", &cur.Justify, in.Justify)
	boolIn("mark_on_open", &cur.MarkOnOpen, in.MarkOnOpen)
	boolIn("mark_on_scroll", &cur.MarkOnScroll, in.MarkOnScroll)
	boolIn("sort_oldest", &cur.SortOldest, in.SortOldest)
	boolIn("show_images", &cur.ShowImages, in.ShowImages)
	boolIn("open_original", &cur.OpenOriginal, in.OpenOriginal)
	boolIn("hide_read", &cur.HideRead, in.HideRead)
	boolIn("confirm_all", &cur.ConfirmAll, in.ConfirmAll)
	boolIn("auto_extract", &cur.AutoExtract, in.AutoExtract)
	boolIn("notify_new_posts", &cur.NotifyNewPosts, in.NotifyNewPosts)
	boolIn("digest_enabled", &cur.DigestEnabled, in.DigestEnabled)
	boolIn("notify_feed_errors", &cur.NotifyFeedError, in.NotifyFeedError)
	boolIn("notes_auto", &cur.NotesAuto, in.NotesAuto)
	boolIn("ai_auto_tag", &cur.AIAutoTag, in.AIAutoTag)
	text("notify_url", &cur.NotifyURL, in.NotifyURL)
	if keys["notify_format"] && in.NotifyFormat != "" {
		cur.NotifyFormat = in.NotifyFormat
	}
	url("notes_url", &cur.NotesURL, in.NotesURL)
	secret("notes_token", &cur.NotesToken, in.NotesToken)
	url("notes_public_url", &cur.NotesPublicURL, in.NotesPublicURL)
	if keys["notes_folder"] {
		f := strings.Trim(strings.TrimSpace(in.NotesFolder), "/")
		if strings.Contains(f, "..") || len(f) > 120 {
			bad("that folder name will not do")
			return
		}
		cur.NotesFolder = f
	}
	text("ai_language", &cur.AILanguage, in.AILanguage)
	if keys["ai_provider"] {
		if !validAIProvider(in.AIProvider) {
			bad("AI is automatic, Pocket AI or OpenAI")
			return
		}
		cur.AIProvider = in.AIProvider
	}
	if keys["ai_pocket_model"] {
		if !validPocketModel(in.AIPocketModel) {
			bad("unknown model")
			return
		}
		cur.AIPocketModel = in.AIPocketModel
	}
	secret("openai_api_key", &cur.OpenAIKey, in.OpenAIKey)
	text("openai_model", &cur.OpenAIModel, in.OpenAIModel)
	url("openai_base_url", &cur.OpenAIBaseURL, in.OpenAIBaseURL)
	if keys["notes_mode"] && (in.NotesMode == modeCore || in.NotesMode == modeDirect || in.NotesMode == modeOff) {
		cur.NotesMode = in.NotesMode
	}
	if err := s.store.SaveUserSettings(r.Context(), me.ID, cur); err != nil {
		s.fail(w, err, "save settings")
		return
	}
	if isAdmin && keys["title"] {
		if err := s.store.SaveBoxSettings(r.Context(), cur); err != nil {
			s.fail(w, err, "save settings")
			return
		}
	}
	for _, k := range modeKeys {
		if keys[k] {
			s.reportModesSoon()
			break
		}
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), me, cur))
}

// handleIntegrationTest checks a connection with the caller's own saved
// settings (or the URL/key typed into the form).
func (s *Server) handleIntegrationTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Which  string `json:"which"`
		URL    string `json:"url"`
		Key    string `json:"key"`
		Format string `json:"format"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	uid := actorID(r.Context())
	st, _ := s.store.GetSettings(r.Context(), uid)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	keyOr := func(saved string) string {
		if strings.Contains(in.Key, "••") || in.Key == "" {
			return saved
		}
		return in.Key
	}
	urlOr := func(saved string) string {
		if strings.TrimSpace(in.URL) == "" {
			return saved
		}
		return strings.TrimRight(strings.TrimSpace(in.URL), "/")
	}
	var msg string
	var err error
	if a, ok := appBySlug(in.Which); ok {
		mode, savedURL, savedTok, _ := connSettings(st, a)
		typed := in.Key != "" && !strings.Contains(in.Key, "••")
		var conn *siblingConn
		if tok := keyOr(savedTok); tok != "" && (typed || s.connMode(mode, savedTok) == modeDirect) {
			base := urlOr(savedURL)
			if base == "" {
				base = s.resolveSibling(ctx, a.slug)
			}
			conn = &siblingConn{client: pocket.New(base, tok, 15*time.Second), via: modeDirect}
		} else {
			conn, err = s.clients(ctx, uid).conn(ctx, a, a.scopes...)
		}
		if err == nil {
			how := "direct connection"
			if conn.via == modeCore {
				how = "through Pocket Core"
			}
			var v string
			if v, err = conn.client.Health(ctx); err == nil {
				msg = fmt.Sprintf("%s %s, %s", a.name, v, how)
			}
		}
	} else if in.Which == "openai" {
		c := ai.New(urlOr(st.OpenAIBaseURL), keyOr(st.OpenAIKey), st.OpenAIModel, 20*time.Second)
		var ids []string
		if ids, err = c.Models(ctx); err == nil {
			msg = fmt.Sprintf("The key works (%d models)", len(ids))
		}
	} else if in.Which == "notify" {
		f := in.Format
		if f == "" {
			f = st.NotifyFormat
		}
		n := notify.New(urlOr(st.NotifyURL), f)
		err = n.Send(ctx, notify.Message{Title: "Pocket Reader", Body: "Notifications work. New posts you asked for and the morning digest will arrive here.", Priority: "info", Tags: []string{"newspaper"}})
		msg = "Test message sent via " + n.Format
	} else {
		writeErr(w, http.StatusBadRequest, "unknown integration")
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

func (s *Server) handleNotifyTest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	if err := n.Send(ctx, notify.Message{Title: "Pocket Reader", Body: "Notifications work.", Priority: "info", Tags: []string{"newspaper"}}); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- system / backup / export ----

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	var size int64
	if fi, err := os.Stat(s.cfg.DBPath); err == nil {
		size = fi.Size()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version": Version, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
		"core":      s.coreStatus(),
		"auth_mode": s.cfg.AuthMode, "data_dir": s.cfg.DataDir, "db_path": s.cfg.DBPath, "db_bytes": size,
		"uptime": time.Since(s.started).Round(time.Second).String(), "timezone": time.Now().Location().String(),
		"counts": s.store.CountsFor(r.Context(), actorID(r.Context())),
	})
}

// handleBackup downloads the whole database, an admin-only box operation.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "backups")
	name := fmt.Sprintf("pocket-reader-%s.db", time.Now().Format("20060102-150405"))
	dst := filepath.Join(dir, name)
	if err := s.store.Backup(r.Context(), dst); err != nil {
		s.fail(w, err, "backup")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, dst)
}
