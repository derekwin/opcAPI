package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/derekwin/opcAPI/internal/billing"
	"github.com/derekwin/opcAPI/internal/promptapi"
	"github.com/derekwin/opcAPI/internal/provider"
	"github.com/derekwin/opcAPI/internal/upstream"
	"github.com/derekwin/opcAPI/internal/user"
)

const adminSessionCookieName = "opc_admin_session"

type adminLoginView struct {
	Lang  string
	Error string
}

type adminSetupView struct {
	Lang  string
	Error string
}

type adminDashboardView struct {
	Claims     user.SessionClaims
	Me         user.AuthResult
	Users      []user.TenantUser
	PromptAPIs []promptapi.Definition
	Orders     []billing.Order
	Message    string
	Error      string
}

var adminAuthTemplateFuncs = template.FuncMap{
	"ui": adminUI,
	"uiOr": func(lang, key, fallback string) string {
		v := adminUI(lang, key)
		if v == key {
			return fallback
		}
		return v
	},
	"langCode": func(lang string) string {
		if isChineseLang(lang) {
			return languageZhCN
		}
		return languageEN
	},
	"withLang": withLangPath,
}
var adminLoginTemplate = template.Must(template.New("admin-login").Funcs(adminAuthTemplateFuncs).Parse(`<!doctype html>
<html lang="{{langCode .Lang}}">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>opcAPI Admin Login</title>
  <style>
    :root {
      --bg: #eef6f1;
      --panel: #ffffff;
      --ink: #162229;
      --muted: #60717a;
      --line: #d7e3dc;
      --brand: #1e7a63;
      --brand-strong: #185f4d;
      --danger: #b24444;
      --shadow: 0 20px 44px rgba(20, 39, 33, 0.12);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      display: grid;
      place-items: center;
      font-family: "IBM Plex Sans", "Noto Sans SC", "PingFang SC", "Segoe UI", sans-serif;
      color: var(--ink);
      background:
        radial-gradient(860px 430px at 0% 0%, #d5e9e0 0%, transparent 72%),
        radial-gradient(760px 360px at 100% 100%, #dceee5 0%, transparent 70%),
        var(--bg);
      padding: 20px;
    }
    .card {
      width: min(560px, 100%);
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 20px;
      padding: 28px;
      box-shadow: var(--shadow);
      animation: rise .24s ease-out;
      display: grid;
      gap: 14px;
    }
    .eyebrow {
      margin: 0;
      letter-spacing: .11em;
      font-weight: 600;
      font-size: 11px;
      color: var(--muted);
    }
    h1 { margin: 0; font-size: 32px; letter-spacing: -.01em; }
    p.sub { margin: 0; color: var(--muted); }
    label { display: block; font-size: 13px; color: var(--muted); margin-bottom: 12px; }
    input {
      width: 100%; margin-top: 7px; border: 1px solid var(--line); border-radius: 11px;
      padding: 11px 12px; font-size: 14px; color: var(--ink); background: #fff;
    }
    .err {
      margin: 0;
      color: var(--danger);
      border: 1px solid #efcccc;
      background: #fff4f4;
      border-radius: 10px;
      padding: 10px 12px;
      font-size: 13px;
    }
    .actions { display: flex; gap: 10px; margin-top: 8px; }
    button {
      border: none; border-radius: 10px; padding: 10px 14px; cursor: pointer; font-size: 14px; transition: .16s ease-in-out;
    }
    .primary { background: var(--brand); color: #fff; }
    .primary:hover { background: var(--brand-strong); transform: translateY(-1px); }
    .secondary { background: #e8f2ed; color: var(--ink); }
    .secondary:hover { transform: translateY(-1px); }
    .tip { margin: 0; font-size: 12px; color: var(--muted); }
    @keyframes rise {
      from { opacity: 0; transform: translateY(10px); }
      to { opacity: 1; transform: translateY(0); }
    }
    @media (max-width: 640px) {
      .card { padding: 20px; border-radius: 14px; }
      .actions { flex-direction: column; }
    }
  </style>
</head>
<body>
  <main class="card">
    <p class="eyebrow">{{uiOr .Lang "management" "OPCAPI MANAGEMENT"}}</p>
    <h1>{{ui .Lang "sign_in"}}</h1>
    <p class="sub">{{uiOr .Lang "admin_signin_subtitle" "Use your administrator account to enter the management console."}}</p>
    {{if .Error}}<p class="err">{{.Error}}</p>{{end}}
    <form method="post" action="{{withLang "/admin/login" .Lang}}">
      <label>{{ui .Lang "email"}}
        <input type="email" name="email" placeholder="admin@example.com" required />
      </label>
      <label>{{ui .Lang "password"}}
        <input type="password" name="password" placeholder="at least 8 characters" required />
      </label>
      <p class="tip">{{uiOr .Lang "signin_tip" "If this is your first time, initialize admin from setup flow."}}</p>
      <div class="actions">
        <button class="primary" type="submit">{{ui .Lang "sign_in"}}</button>
        <button class="secondary" type="button" onclick="location.href='/healthz'">Healthz</button>
      </div>
    </form>
  </main>
</body>
</html>`))

var adminSetupTemplate = template.Must(template.New("admin-setup").Funcs(adminAuthTemplateFuncs).Parse(`<!doctype html>
<html lang="{{langCode .Lang}}">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>opcAPI First Admin Setup</title>
  <style>
    :root {
      --bg: #eef6f1;
      --panel: #ffffff;
      --ink: #162229;
      --muted: #60717a;
      --line: #d7e3dc;
      --brand: #1e7a63;
      --brand-strong: #185f4d;
      --danger: #b24444;
      --shadow: 0 20px 44px rgba(20, 39, 33, 0.12);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      display: grid;
      place-items: center;
      font-family: "IBM Plex Sans", "Noto Sans SC", "PingFang SC", "Segoe UI", sans-serif;
      color: var(--ink);
      background:
        radial-gradient(860px 430px at 0% 0%, #d5e9e0 0%, transparent 72%),
        radial-gradient(760px 360px at 100% 100%, #dceee5 0%, transparent 70%),
        var(--bg);
      padding: 20px;
    }
    .card {
      width: min(560px, 100%);
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 20px;
      padding: 28px;
      box-shadow: var(--shadow);
      animation: rise .24s ease-out;
      display: grid;
      gap: 14px;
    }
    .eyebrow {
      margin: 0;
      letter-spacing: .11em;
      font-weight: 600;
      font-size: 11px;
      color: var(--muted);
    }
    h1 { margin: 0; font-size: 32px; letter-spacing: -.01em; }
    p.sub { margin: 0; color: var(--muted); }
    label { display: block; font-size: 13px; color: var(--muted); margin-bottom: 12px; }
    input {
      width: 100%; margin-top: 7px; border: 1px solid var(--line); border-radius: 11px;
      padding: 11px 12px; font-size: 14px; color: var(--ink); background: #fff;
    }
    .err {
      margin: 0;
      color: var(--danger);
      border: 1px solid #efcccc;
      background: #fff4f4;
      border-radius: 10px;
      padding: 10px 12px;
      font-size: 13px;
    }
    .actions { display: flex; gap: 10px; margin-top: 8px; }
    button {
      border: none; border-radius: 10px; padding: 10px 14px; cursor: pointer; font-size: 14px; transition: .16s ease-in-out;
    }
    .primary { background: var(--brand); color: #fff; }
    .primary:hover { background: var(--brand-strong); transform: translateY(-1px); }
    .secondary { background: #e8f2ed; color: var(--ink); }
    .secondary:hover { transform: translateY(-1px); }
    .tip { margin: 0; font-size: 12px; color: var(--muted); }
    @keyframes rise {
      from { opacity: 0; transform: translateY(10px); }
      to { opacity: 1; transform: translateY(0); }
    }
    @media (max-width: 640px) {
      .card { padding: 20px; border-radius: 14px; }
      .actions { flex-direction: column; }
    }
  </style>
</head>
<body>
  <main class="card">
    <p class="eyebrow">{{uiOr .Lang "bootstrap" "OPCAPI BOOTSTRAP"}}</p>
    <h1>{{uiOr .Lang "initialize_admin" "Initialize Admin"}}</h1>
    <p class="sub">{{uiOr .Lang "admin_setup_subtitle" "Create the first admin account. This page appears only when no admin exists."}}</p>
    {{if .Error}}<p class="err">{{.Error}}</p>{{end}}
    <form method="post" action="{{withLang "/admin/setup" .Lang}}">
      <label>{{ui .Lang "email"}}
        <input type="email" name="email" placeholder="admin@example.com" required />
      </label>
      <label>{{ui .Lang "password"}}
        <input type="password" name="password" placeholder="at least 8 characters" required />
      </label>
      <label>{{ui .Lang "confirm_password"}}
        <input type="password" name="password_confirm" placeholder="repeat password" required />
      </label>
      <p class="tip">{{uiOr .Lang "admin_setup_tip" "Use a production-grade password and store it securely."}}</p>
      <div class="actions">
        <button class="primary" type="submit">{{uiOr .Lang "create_super_admin" "Create Super Admin"}}</button>
        <button class="secondary" type="button" onclick="location.href='/healthz'">Healthz</button>
      </div>
    </form>
  </main>
</body>
</html>`))

var adminDashboardTemplate = template.Must(template.New("admin-dashboard").Funcs(template.FuncMap{
	"join": func(items []string) string { return strings.Join(items, ",") },
}).Parse(`<!doctype html>
<html>
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>opcAPI Admin Console</title>
  <style>
    :root {
      --bg: #edf3fb;
      --panel: #ffffff;
      --ink: #112236;
      --muted: #607089;
      --line: #d8e3f2;
      --brand: #1769e0;
      --danger: #b42323;
      --ok: #1f7a3a;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0; padding: 18px;
      font-family: "Avenir Next", "Segoe UI", sans-serif;
      color: var(--ink);
      background: radial-gradient(circle at top right, #d8e8fd, transparent 35%), var(--bg);
    }
    .wrap { max-width: 1280px; margin: 0 auto; display: grid; gap: 14px; }
    .top {
      display: flex; justify-content: space-between; align-items: center; gap: 12px;
      background: var(--panel); border: 1px solid var(--line); border-radius: 14px;
      padding: 14px 16px; box-shadow: 0 8px 24px rgba(17,39,70,.08);
    }
    h1 { margin: 0; font-size: 24px; }
    .meta { color: var(--muted); font-size: 13px; margin-top: 4px; }
    .msg { margin: 0; padding: 10px 12px; border-radius: 10px; }
    .msg.ok { color: var(--ok); background: #edf8f1; border: 1px solid #b6dfc2; }
    .msg.err { color: var(--danger); background: #fff1f1; border: 1px solid #efc6c6; }
    .grid { display: grid; grid-template-columns: repeat(12,minmax(0,1fr)); gap: 14px; }
    .card {
      background: var(--panel); border: 1px solid var(--line); border-radius: 14px;
      padding: 14px; box-shadow: 0 8px 20px rgba(17,39,70,.06); overflow: auto;
    }
    .col-12 { grid-column: span 12; }
    .col-6 { grid-column: span 6; }
    .col-4 { grid-column: span 4; }
    .col-3 { grid-column: span 3; }
    h2 { margin: 0 0 10px; font-size: 18px; }
    table { width: 100%; border-collapse: collapse; font-size: 13px; }
    th, td { border-bottom: 1px solid var(--line); text-align: left; padding: 8px 6px; vertical-align: top; }
    tr:last-child td { border-bottom: none; }
    th { color: var(--muted); font-weight: 600; white-space: nowrap; }
    form.stack { display: grid; gap: 8px; }
    .f2 { display: grid; grid-template-columns: repeat(2,minmax(0,1fr)); gap: 8px; }
    .f3 { display: grid; grid-template-columns: repeat(3,minmax(0,1fr)); gap: 8px; }
    label { font-size: 12px; color: var(--muted); display: grid; gap: 4px; }
    input, textarea, select {
      width: 100%; border: 1px solid var(--line); border-radius: 8px;
      padding: 8px 10px; font-size: 13px; background: #fff; color: var(--ink);
    }
    textarea { min-height: 90px; resize: vertical; }
    .btn { width: fit-content; border: none; border-radius: 8px; padding: 8px 12px; cursor: pointer; color: #fff; background: var(--brand); }
    .btn.gray { background: #6f8098; }
    @media (max-width: 980px) { .col-6,.col-4,.col-3 { grid-column: span 12; } }
  </style>
</head>
<body>
  <main class="wrap">
    <section class="top">
      <div>
        <h1>opcAPI Admin Console</h1>
        <div class="meta">user={{.Claims.UserID}} role={{.Claims.Role}} email={{.Me.User.Email}}</div>
      </div>
      <form method="post" action="/admin/logout"><button class="btn gray" type="submit">Logout</button></form>
    </section>

    {{if .Message}}<p class="msg ok">{{.Message}}</p>{{end}}
    {{if .Error}}<p class="msg err">{{.Error}}</p>{{end}}

    <section class="grid">
      <article class="card col-6">
        <h2>Users</h2>
        <table>
          <thead><tr><th>ID</th><th>Email</th><th>Role</th><th>Status</th></tr></thead>
          <tbody>
            {{range .Users}}
            <tr><td>{{.User.ID}}</td><td>{{.User.Email}}</td><td>{{.Role}}</td><td>{{.User.Status}}</td></tr>
            {{end}}
          </tbody>
        </table>
      </article>

      <article class="card col-6">
        <h2>Create User</h2>
        <form class="stack" method="post" action="/admin/users/new">
          <div class="f2">
            <label>Email<input name="email" type="email" required /></label>
            <label>Password<input name="password" type="password" required /></label>
          </div>
          <label>Role
            <select name="role"><option value="member">member</option><option value="admin">admin</option></select>
          </label>
          <button class="btn" type="submit">Create User</button>
        </form>
        <hr />
        <form class="stack" method="post" action="/admin/users/role">
          <div class="f2">
            <label>User ID<input name="user_id" required /></label>
            <label>Role<select name="role"><option value="member">member</option><option value="admin">admin</option></select></label>
          </div>
          <button class="btn" type="submit">Update Role</button>
        </form>
        <hr />
        <form class="stack" method="post" action="/admin/users/status">
          <div class="f2">
            <label>User ID<input name="user_id" required /></label>
            <label>Status<select name="status"><option value="active">active</option><option value="disabled">disabled</option></select></label>
          </div>
          <button class="btn" type="submit">Update Status</button>
        </form>
      </article>

      <article class="card col-6">
        <h2>Prompt APIs</h2>
        <table>
          <thead><tr><th>Slug</th><th>Name</th><th>Status</th><th>Model</th><th>Version</th></tr></thead>
          <tbody>
            {{range .PromptAPIs}}
            <tr><td>{{.Slug}}</td><td>{{.Name}}</td><td>{{.Status}}</td><td>{{.Model}}</td><td>{{.Version}}</td></tr>
            {{end}}
          </tbody>
        </table>
        <hr />
        <form class="stack" method="post" action="/admin/prompt-apis/new">
          <div class="f3">
            <label>Slug<input name="slug" required /></label>
            <label>Name<input name="name" required /></label>
            <label>Model<input name="model" required /></label>
          </div>
          <label>Description<input name="description" /></label>
          <label>Variables (comma separated)<input name="variables" placeholder="name,topic" /></label>
          <label>System Prompt<textarea name="system_prompt"></textarea></label>
          <label>User Template<textarea name="user_template" placeholder="Summarize {{.topic}} for {{.name}}"></textarea></label>
          <label><input type="checkbox" name="publish" checked /> publish</label>
          <button class="btn" type="submit">Save Prompt API</button>
        </form>
      </article>

      <article class="card col-12">
        <h2>Billing</h2>
        <table>
          <thead><tr><th>ID</th><th>User</th><th>Type</th><th>Amount</th><th>Currency</th><th>Status</th></tr></thead>
          <tbody>
            {{range .Orders}}
            <tr><td>{{.ID}}</td><td>{{.UserID}}</td><td>{{.Type}}</td><td>{{.Amount}}</td><td>{{.Currency}}</td><td>{{.Status}}</td></tr>
            {{end}}
          </tbody>
        </table>
      </article>
    </section>
  </main>
</body>
</html>`))

func (h *handler) adminLoginPage(w http.ResponseWriter, r *http.Request) {
	lang := detectLanguage(r)
	if claims, ok := h.adminClaimsFromRequest(r); ok && claims.Role == "admin" {
		http.Redirect(w, r, withLangPath("/admin/users", lang), http.StatusSeeOther)
		return
	}
	hasAdmin, err := h.hasAdminAccount(r.Context())
	if err != nil {
		writeLocalizedPlainError(w, r, http.StatusServiceUnavailable, "user_service_unavailable", "user service unavailable")
		return
	}
	if !hasAdmin {
		http.Redirect(w, r, withLangPath("/admin/setup", lang), http.StatusSeeOther)
		return
	}
	_ = adminLoginTemplate.Execute(w, adminLoginView{
		Lang:  lang,
		Error: adminErrorMessage(r, strings.TrimSpace(r.URL.Query().Get("error"))),
	})
}

func (h *handler) adminLoginSubmit(w http.ResponseWriter, r *http.Request) {
	lang := detectLanguage(r)
	if h.userService == nil {
		writeLocalizedPlainError(w, r, http.StatusServiceUnavailable, "user_service_unavailable", "user service unavailable")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeLocalizedPlainError(w, r, http.StatusBadRequest, "invalid_auth_request", "invalid form data")
		return
	}

	hasAdmin, err := h.hasAdminAccount(r.Context())
	if err != nil {
		writeLocalizedPlainError(w, r, http.StatusServiceUnavailable, "user_service_unavailable", "user service unavailable")
		return
	}
	if !hasAdmin {
		http.Redirect(w, r, withLangPath("/admin/setup", lang), http.StatusSeeOther)
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	password := strings.TrimSpace(r.FormValue("password"))

	res, err := h.userService.Signin(r.Context(), user.SigninRequest{Email: email, Password: password})
	if err != nil {
		http.Redirect(w, r, withQuery(withLangPath("/admin/login", lang), "error", err.Error()), http.StatusSeeOther)
		return
	}
	if res.Role != "admin" {
		http.Redirect(w, r, withQuery(withLangPath("/admin/login", lang), "error", "permission denied: admin only"), http.StatusSeeOther)
		return
	}

	h.setAdminSessionCookie(w, res.Token)
	http.Redirect(w, r, withQuery(withLangPath("/admin/users", lang), "message", "sign_in_success"), http.StatusSeeOther)
}

func (h *handler) adminSetupPage(w http.ResponseWriter, r *http.Request) {
	lang := detectLanguage(r)
	if claims, ok := h.adminClaimsFromRequest(r); ok && claims.Role == "admin" {
		http.Redirect(w, r, withLangPath("/admin/users", lang), http.StatusSeeOther)
		return
	}
	hasAdmin, err := h.hasAdminAccount(r.Context())
	if err != nil {
		writeLocalizedPlainError(w, r, http.StatusServiceUnavailable, "user_service_unavailable", "user service unavailable")
		return
	}
	if hasAdmin {
		http.Redirect(w, r, withLangPath("/admin/login", lang), http.StatusSeeOther)
		return
	}
	_ = adminSetupTemplate.Execute(w, adminSetupView{
		Lang:  lang,
		Error: adminErrorMessage(r, strings.TrimSpace(r.URL.Query().Get("error"))),
	})
}

func (h *handler) adminSetupSubmit(w http.ResponseWriter, r *http.Request) {
	lang := detectLanguage(r)
	if h.userService == nil {
		writeLocalizedPlainError(w, r, http.StatusServiceUnavailable, "user_service_unavailable", "user service unavailable")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeLocalizedPlainError(w, r, http.StatusBadRequest, "invalid_auth_request", "invalid form data")
		return
	}
	hasAdmin, err := h.hasAdminAccount(r.Context())
	if err != nil {
		writeLocalizedPlainError(w, r, http.StatusServiceUnavailable, "user_service_unavailable", "user service unavailable")
		return
	}
	if hasAdmin {
		http.Redirect(w, r, withLangPath("/admin/login", lang), http.StatusSeeOther)
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	password := strings.TrimSpace(r.FormValue("password"))
	passwordConfirm := strings.TrimSpace(r.FormValue("password_confirm"))
	if password != passwordConfirm {
		http.Redirect(w, r, withQuery(withLangPath("/admin/setup", lang), "error", "password confirmation mismatch"), http.StatusSeeOther)
		return
	}

	res, err := h.userService.BootstrapAdmin(r.Context(), user.SignupRequest{Email: email, Password: password})
	if err != nil {
		http.Redirect(w, r, withQuery(withLangPath("/admin/setup", lang), "error", err.Error()), http.StatusSeeOther)
		return
	}
	h.setAdminSessionCookie(w, res.Token)
	http.Redirect(w, r, withQuery(withLangPath("/admin/users", lang), "message", "admin_setup_complete"), http.StatusSeeOther)
}

func (h *handler) adminLogout(w http.ResponseWriter, r *http.Request) {
	lang := detectLanguage(r)
	if cookie, err := r.Cookie(adminSessionCookieName); err == nil {
		_ = h.userService.Logout(r.Context(), cookie.Value)
	}
	h.clearAdminSessionCookie(w)
	http.Redirect(w, r, withLangPath("/admin/login", lang), http.StatusSeeOther)
}

func (h *handler) adminDashboard(w http.ResponseWriter, r *http.Request) {
	lang := detectLanguage(r)
	claims := SessionClaimsFromContext(r.Context())
	if claims.UserID == "" {
		http.Redirect(w, r, withQuery(withLangPath("/admin/login", lang), "error", "invalid_session"), http.StatusSeeOther)
		return
	}
	if h.userService == nil {
		writeLocalizedPlainError(w, r, http.StatusServiceUnavailable, "user_service_unavailable", "user service unavailable")
		return
	}

	me, err := h.userService.GetMe(r.Context(), claims)
	if err != nil {
		http.Redirect(w, r, withQuery(withLangPath("/admin/login", lang), "error", "invalid_session"), http.StatusSeeOther)
		return
	}

	view := adminDashboardView{Claims: claims, Me: me, Message: strings.TrimSpace(r.URL.Query().Get("message")), Error: strings.TrimSpace(r.URL.Query().Get("error"))}
	if users, err := h.userService.ListUsers(r.Context(), claims); err == nil {
		view.Users = users
	}
	if h.promptAPI != nil {
		if page, err := h.promptAPI.List(r.Context(), promptapi.ListQuery{Limit: 200}); err == nil {
			view.PromptAPIs = page.Data
		}
	}
	if h.billing != nil {
		if page, err := h.billing.ListOrdersAdmin(r.Context(), billing.OrderQuery{Limit: 20}); err == nil {
			view.Orders = page.Data
		}
	}

	_ = adminDashboardTemplate.Execute(w, view)
}

func (h *handler) adminPromptCreate(w http.ResponseWriter, r *http.Request) {
	if h.promptAPI == nil {
		h.redirectAdminWithError(w, r, "save_prompt_api_failed: prompt api unavailable")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.redirectAdminWithError(w, r, "invalid form data")
		return
	}

	req := buildPromptUpsertRequestFromForm(r)
	def, err := h.promptAPI.Upsert(r.Context(), req)
	if err != nil {
		h.redirectAdminWithError(w, r, "save_prompt_api_failed: "+err.Error())
		return
	}
	h.redirectAdminWithMessage(w, r, "prompt_api_saved:"+def.Slug)
}

func (h *handler) adminPromptPreview(w http.ResponseWriter, r *http.Request) {
	if h.promptAPI == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "prompt api unavailable"})
		return
	}
	if h.gateway == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "gateway unavailable"})
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid form data"})
		return
	}

	req := buildPromptUpsertRequestFromForm(r)
	if strings.TrimSpace(req.Slug) == "" {
		req.Slug = "preview"
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = "Prompt Preview"
	}

	previewInput, err := parsePromptPreviewInput(r.FormValue("preview_input"), req.Variables)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid preview input json: " + err.Error()})
		return
	}

	def, providerReq, renderedUserPrompt, err := h.promptAPI.BuildPreviewChatRequest(req, previewInput)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, promptapi.ErrStoreUnavailable) {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}

	result, err := h.gateway.Chat(r.Context(), providerReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "upstream provider request failed: " + err.Error()})
		return
	}

	systemPrompt := ""
	if len(providerReq.Messages) > 0 && providerReq.Messages[0].Role == "system" {
		systemPrompt = providerReq.Messages[0].Content
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"definition": map[string]any{
			"slug":          def.Slug,
			"name":          def.Name,
			"model":         def.Model,
			"variables":     def.Variables,
			"input_schema":  def.InputSchema,
			"output_schema": def.OutputSchema,
		},
		"preview": map[string]any{
			"input":             previewInput,
			"rendered_system":   systemPrompt,
			"rendered_user":     renderedUserPrompt,
			"assistant_reply":   strings.TrimSpace(result.Content),
			"provider_model":    result.Model,
			"prompt_tokens":     result.PromptTokens,
			"completion_tokens": result.CompletionTokens,
			"total_tokens":      result.PromptTokens + result.CompletionTokens,
		},
	})
}

func (h *handler) adminUpstreamDiagnose(w http.ResponseWriter, r *http.Request) {
	if h.upstream == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "upstream service unavailable"})
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid form data"})
		return
	}

	payload, err := h.performUpstreamDiagnose(
		r.Context(),
		r.FormValue("id"),
		r.FormValue("model"),
		r.FormValue("system_prompt"),
		r.FormValue("user_prompt"),
	)
	if err != nil {
		switch {
		case errors.Is(err, errMissingUpstreamID):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		case errors.Is(err, upstream.ErrUpstreamNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (h *handler) performUpstreamDiagnose(ctx context.Context, upstreamID, model, systemPrompt, userPrompt string) (upstreamDiagnosePayload, error) {
	targetID := strings.TrimSpace(upstreamID)
	if targetID == "" {
		return upstreamDiagnosePayload{}, errMissingUpstreamID
	}
	items, err := h.upstream.ListUpstreams()
	if err != nil {
		return upstreamDiagnosePayload{}, fmt.Errorf("list upstreams failed: %w", err)
	}
	item, ok := findUpstreamByID(items, targetID)
	if !ok {
		return upstreamDiagnosePayload{}, upstream.ErrUpstreamNotFound
	}

	healthResults, err := h.upstream.CheckHealth(ctx, targetID)
	if err != nil {
		return upstreamDiagnosePayload{}, fmt.Errorf("check upstream failed: %w", err)
	}

	payload := upstreamDiagnosePayload{
		Health: map[string]any{
			"upstream_id":   item.ID,
			"upstream_name": item.Name,
			"provider_type": item.Type,
			"base_url":      item.BaseURL,
			"enabled":       item.Enabled,
		},
	}
	if len(healthResults) > 0 {
		healthResult := healthResults[0]
		payload.Health["healthy"] = healthResult.Healthy
		payload.Health["latency_ms"] = healthResult.LatencyMS
		payload.Health["http_status"] = healthResult.HTTPCode
		payload.Health["endpoint"] = healthResult.Endpoint
		payload.Health["error"] = healthResult.Error
		payload.Health["checked_at"] = healthResult.CheckedAt
		payload.Health["raw_response"] = healthResult.Response
	}

	targetModel := strings.TrimSpace(model)
	if targetModel == "" && len(item.Models) > 0 {
		targetModel = strings.TrimSpace(item.Models[0])
	}
	if targetModel == "" {
		payload.ProbeError = "model is required"
		return payload, nil
	}

	system := strings.TrimSpace(systemPrompt)
	userContent := strings.TrimSpace(userPrompt)
	if userContent == "" {
		userContent = "请回复“pong”并简短说明当前模型名。"
	}

	messages := make([]provider.Message, 0, 2)
	if system != "" {
		messages = append(messages, provider.Message{Role: "system", Content: system})
	}
	messages = append(messages, provider.Message{Role: "user", Content: userContent})

	start := time.Now()
	result, err := runUpstreamProbe(ctx, item, provider.ChatRequest{
		Model:    targetModel,
		Messages: messages,
	})
	latency := time.Since(start).Milliseconds()
	if err != nil {
		payload.ProbeError = err.Error()
		payload.Probe = map[string]any{
			"upstream_id":         item.ID,
			"upstream_name":       item.Name,
			"provider_type":       item.Type,
			"base_url":            item.BaseURL,
			"requested_model":     targetModel,
			"latency_ms":          latency,
			"http_status":         result.HTTPStatus,
			"endpoint":            result.Endpoint,
			"raw_response":        result.RawResponse,
			"upstream_is_enabled": item.Enabled,
		}
		return payload, nil
	}

	payload.Probe = map[string]any{
		"upstream_id":         item.ID,
		"upstream_name":       item.Name,
		"provider_type":       item.Type,
		"base_url":            item.BaseURL,
		"requested_model":     targetModel,
		"provider_model":      result.Chat.Model,
		"assistant_reply":     strings.TrimSpace(result.Chat.Content),
		"latency_ms":          latency,
		"prompt_tokens":       result.Chat.PromptTokens,
		"completion_tokens":   result.Chat.CompletionTokens,
		"total_tokens":        result.Chat.PromptTokens + result.Chat.CompletionTokens,
		"http_status":         result.HTTPStatus,
		"endpoint":            result.Endpoint,
		"raw_response":        result.RawResponse,
		"upstream_is_enabled": item.Enabled,
	}
	return payload, nil
}

func findUpstreamByID(items []upstream.Upstream, id string) (upstream.Upstream, bool) {
	target := strings.TrimSpace(id)
	for _, item := range items {
		if item.ID == target {
			return item, true
		}
	}
	return upstream.Upstream{}, false
}

type upstreamProbeResult struct {
	Chat        provider.ChatResult
	Endpoint    string
	HTTPStatus  int
	RawResponse string
}

type upstreamDiagnosePayload struct {
	Health     map[string]any `json:"health"`
	Probe      map[string]any `json:"probe,omitempty"`
	ProbeError string         `json:"probe_error,omitempty"`
}

var errMissingUpstreamID = errors.New("missing upstream id")

func runUpstreamProbe(ctx context.Context, item upstream.Upstream, req provider.ChatRequest) (upstreamProbeResult, error) {
	switch item.Type {
	case upstream.ProviderOpenAICompat:
		return runOpenAICompatProbe(ctx, item, req)
	case upstream.ProviderAnthropic:
		return runAnthropicProbe(ctx, item, req)
	case upstream.ProviderGemini:
		return runGeminiProbe(ctx, item, req)
	case upstream.ProviderOllama:
		return runOllamaProbe(ctx, item, req)
	default:
		return upstreamProbeResult{}, fmt.Errorf("unsupported upstream provider type: %s", item.Type)
	}
}

func runOpenAICompatProbe(ctx context.Context, item upstream.Upstream, req provider.ChatRequest) (upstreamProbeResult, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = "gpt-4o-mini"
	}
	messages := make([]map[string]string, 0, len(req.Messages))
	for _, m := range req.Messages {
		messages = append(messages, map[string]string{
			"role":    strings.TrimSpace(m.Role),
			"content": m.Content,
		})
	}
	payload := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   false,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return upstreamProbeResult{}, err
	}
	base := strings.TrimRight(strings.TrimSpace(item.BaseURL), "/")
	route := "/v1/chat/completions"
	if strings.HasSuffix(base, "/v1") {
		route = "/chat/completions"
	}
	endpoint := base + route
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return upstreamProbeResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(item.APIKey) != "" {
		httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(item.APIKey))
	}

	result, raw, status, err := executeProbeRequest(httpReq)
	if err != nil {
		return upstreamProbeResult{Endpoint: endpoint, HTTPStatus: status, RawResponse: raw}, err
	}
	var parsed struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		return upstreamProbeResult{Endpoint: endpoint, HTTPStatus: status, RawResponse: raw}, err
	}
	if len(parsed.Choices) == 0 {
		return upstreamProbeResult{Endpoint: endpoint, HTTPStatus: status, RawResponse: raw}, provider.ErrUpstreamResponse
	}
	outModel := strings.TrimSpace(parsed.Model)
	if outModel == "" {
		outModel = model
	}
	return upstreamProbeResult{
		Endpoint:    endpoint,
		HTTPStatus:  status,
		RawResponse: raw,
		Chat: provider.ChatResult{
			Model:            "openai/" + outModel,
			Content:          parsed.Choices[0].Message.Content,
			PromptTokens:     parsed.Usage.PromptTokens,
			CompletionTokens: parsed.Usage.CompletionTokens,
		},
	}, nil
}

func runAnthropicProbe(ctx context.Context, item upstream.Upstream, req provider.ChatRequest) (upstreamProbeResult, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = "claude-3-5-haiku-latest"
	}
	systemPrompt := make([]string, 0, 2)
	type anthropicMessage struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	messages := make([]anthropicMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		content := strings.TrimSpace(m.Content)
		if role == "system" {
			if content != "" {
				systemPrompt = append(systemPrompt, content)
			}
			continue
		}
		targetRole := "user"
		if role == "assistant" {
			targetRole = "assistant"
		}
		block := struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{Type: "text", Text: content}
		messages = append(messages, anthropicMessage{
			Role: targetRole,
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{block},
		})
	}
	if len(messages) == 0 {
		return upstreamProbeResult{}, provider.ErrUpstreamResponse
	}
	payload := map[string]any{
		"model":      model,
		"messages":   messages,
		"max_tokens": 1024,
	}
	if len(systemPrompt) > 0 {
		payload["system"] = strings.Join(systemPrompt, "\n\n")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return upstreamProbeResult{}, err
	}
	base := strings.TrimRight(strings.TrimSpace(item.BaseURL), "/")
	route := "/v1/messages"
	if strings.HasSuffix(base, "/v1") {
		route = "/messages"
	}
	endpoint := base + route
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return upstreamProbeResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	if strings.TrimSpace(item.APIKey) != "" {
		httpReq.Header.Set("x-api-key", strings.TrimSpace(item.APIKey))
	}

	result, raw, status, err := executeProbeRequest(httpReq)
	if err != nil {
		return upstreamProbeResult{Endpoint: endpoint, HTTPStatus: status, RawResponse: raw}, err
	}
	var parsed struct {
		Model   string `json:"model"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		return upstreamProbeResult{Endpoint: endpoint, HTTPStatus: status, RawResponse: raw}, err
	}
	if len(parsed.Content) == 0 {
		return upstreamProbeResult{Endpoint: endpoint, HTTPStatus: status, RawResponse: raw}, provider.ErrUpstreamResponse
	}
	out := make([]string, 0, len(parsed.Content))
	for _, c := range parsed.Content {
		if strings.TrimSpace(c.Text) != "" {
			out = append(out, c.Text)
		}
	}
	if len(out) == 0 {
		return upstreamProbeResult{Endpoint: endpoint, HTTPStatus: status, RawResponse: raw}, provider.ErrUpstreamResponse
	}
	outModel := strings.TrimSpace(parsed.Model)
	if outModel == "" {
		outModel = model
	}
	return upstreamProbeResult{
		Endpoint:    endpoint,
		HTTPStatus:  status,
		RawResponse: raw,
		Chat: provider.ChatResult{
			Model:            "anthropic/" + outModel,
			Content:          strings.Join(out, "\n"),
			PromptTokens:     parsed.Usage.InputTokens,
			CompletionTokens: parsed.Usage.OutputTokens,
		},
	}, nil
}

func runGeminiProbe(ctx context.Context, item upstream.Upstream, req provider.ChatRequest) (upstreamProbeResult, error) {
	model := strings.TrimPrefix(strings.TrimSpace(req.Model), "models/")
	if model == "" {
		model = "gemini-2.0-flash"
	}
	systemPrompt := make([]string, 0, 2)
	type geminiPart struct {
		Text string `json:"text"`
	}
	type geminiContent struct {
		Role  string       `json:"role"`
		Parts []geminiPart `json:"parts"`
	}
	contents := make([]geminiContent, 0, len(req.Messages))
	for _, m := range req.Messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		text := strings.TrimSpace(m.Content)
		if role == "system" {
			if text != "" {
				systemPrompt = append(systemPrompt, text)
			}
			continue
		}
		targetRole := "user"
		if role == "assistant" {
			targetRole = "model"
		}
		contents = append(contents, geminiContent{
			Role:  targetRole,
			Parts: []geminiPart{{Text: text}},
		})
	}
	if len(contents) == 0 {
		return upstreamProbeResult{}, provider.ErrUpstreamResponse
	}
	payload := map[string]any{"contents": contents}
	if len(systemPrompt) > 0 {
		payload["system_instruction"] = map[string]any{
			"parts": []map[string]string{{"text": strings.Join(systemPrompt, "\n\n")}},
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return upstreamProbeResult{}, err
	}
	base := strings.TrimRight(strings.TrimSpace(item.BaseURL), "/")
	route := "/v1beta/models/" + url.PathEscape(model) + ":generateContent"
	if strings.HasSuffix(base, "/v1beta") || strings.HasSuffix(base, "/v1") {
		route = "/models/" + url.PathEscape(model) + ":generateContent"
	}
	endpoint := base + route
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return upstreamProbeResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(item.APIKey) != "" {
		httpReq.Header.Set("x-goog-api-key", strings.TrimSpace(item.APIKey))
	}

	result, raw, status, err := executeProbeRequest(httpReq)
	if err != nil {
		return upstreamProbeResult{Endpoint: endpoint, HTTPStatus: status, RawResponse: raw}, err
	}
	var parsed struct {
		ModelVersion string `json:"modelVersion"`
		Candidates   []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		return upstreamProbeResult{Endpoint: endpoint, HTTPStatus: status, RawResponse: raw}, err
	}
	if len(parsed.Candidates) == 0 || len(parsed.Candidates[0].Content.Parts) == 0 {
		return upstreamProbeResult{Endpoint: endpoint, HTTPStatus: status, RawResponse: raw}, provider.ErrUpstreamResponse
	}
	out := make([]string, 0, len(parsed.Candidates[0].Content.Parts))
	for _, p := range parsed.Candidates[0].Content.Parts {
		if strings.TrimSpace(p.Text) != "" {
			out = append(out, p.Text)
		}
	}
	if len(out) == 0 {
		return upstreamProbeResult{Endpoint: endpoint, HTTPStatus: status, RawResponse: raw}, provider.ErrUpstreamResponse
	}
	outModel := strings.TrimSpace(parsed.ModelVersion)
	if outModel == "" {
		outModel = model
	}
	return upstreamProbeResult{
		Endpoint:    endpoint,
		HTTPStatus:  status,
		RawResponse: raw,
		Chat: provider.ChatResult{
			Model:            "gemini/" + outModel,
			Content:          strings.Join(out, "\n"),
			PromptTokens:     parsed.UsageMetadata.PromptTokenCount,
			CompletionTokens: parsed.UsageMetadata.CandidatesTokenCount,
		},
	}, nil
}

func runOllamaProbe(ctx context.Context, item upstream.Upstream, req provider.ChatRequest) (upstreamProbeResult, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = "llama3.1"
	}
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	messages := make([]msg, 0, len(req.Messages))
	for _, m := range req.Messages {
		messages = append(messages, msg{Role: strings.TrimSpace(m.Role), Content: m.Content})
	}
	payload := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   false,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return upstreamProbeResult{}, err
	}
	base := strings.TrimRight(strings.TrimSpace(item.BaseURL), "/")
	endpoint := base + "/api/chat"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return upstreamProbeResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	result, raw, status, err := executeProbeRequest(httpReq)
	if err != nil {
		return upstreamProbeResult{Endpoint: endpoint, HTTPStatus: status, RawResponse: raw}, err
	}
	var parsed struct {
		Model   string `json:"model"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		PromptEvalCount int `json:"prompt_eval_count"`
		EvalCount       int `json:"eval_count"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		return upstreamProbeResult{Endpoint: endpoint, HTTPStatus: status, RawResponse: raw}, err
	}
	if strings.TrimSpace(parsed.Message.Content) == "" {
		return upstreamProbeResult{Endpoint: endpoint, HTTPStatus: status, RawResponse: raw}, provider.ErrUpstreamResponse
	}
	outModel := strings.TrimSpace(parsed.Model)
	if outModel == "" {
		outModel = model
	}
	return upstreamProbeResult{
		Endpoint:    endpoint,
		HTTPStatus:  status,
		RawResponse: raw,
		Chat: provider.ChatResult{
			Model:            "ollama/" + outModel,
			Content:          parsed.Message.Content,
			PromptTokens:     parsed.PromptEvalCount,
			CompletionTokens: parsed.EvalCount,
		},
	}, nil
}

func executeProbeRequest(httpReq *http.Request) ([]byte, string, int, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, "", 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", resp.StatusCode, err
	}
	text := strings.TrimSpace(string(raw))
	if resp.StatusCode >= http.StatusBadRequest {
		if text == "" {
			return raw, text, resp.StatusCode, fmt.Errorf("upstream status %d", resp.StatusCode)
		}
		return raw, text, resp.StatusCode, fmt.Errorf("upstream status %d: %s", resp.StatusCode, text)
	}
	return raw, text, resp.StatusCode, nil
}

func (h *handler) adminBillingPlansUpdate(w http.ResponseWriter, r *http.Request) {
	if h.billing == nil {
		h.redirectAdminWithError(w, r, "update_plan_configs_failed: billing unavailable")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.redirectAdminWithError(w, r, "invalid form data")
		return
	}

	updates := make([]billing.PlanConfig, 0, len(billing.SupportedPlans()))
	for _, planID := range billing.SupportedPlans() {
		price, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue("price_"+planID)), 64)
		if err != nil || price < 0 {
			h.redirectAdminWithError(w, r, "update_plan_configs_failed: invalid price for "+planID)
			return
		}
		rpm, err := parseNonNegativeIntForm(r.FormValue("rpm_" + planID))
		if err != nil {
			h.redirectAdminWithError(w, r, "update_plan_configs_failed: invalid rpm for "+planID)
			return
		}
		fiveHour, err := parseNonNegativeIntForm(r.FormValue("limit5h_" + planID))
		if err != nil {
			h.redirectAdminWithError(w, r, "update_plan_configs_failed: invalid 5h limit for "+planID)
			return
		}
		monthly, err := parseNonNegativeIntForm(r.FormValue("limitm_" + planID))
		if err != nil {
			h.redirectAdminWithError(w, r, "update_plan_configs_failed: invalid monthly limit for "+planID)
			return
		}
		unlimited := r.FormValue("unlimited_"+planID) == "on" || strings.EqualFold(strings.TrimSpace(r.FormValue("unlimited_"+planID)), "true")
		updates = append(updates, billing.PlanConfig{
			PlanID:             planID,
			PriceCNY:           price,
			RPMLimit:           rpm,
			FiveHourTokenLimit: fiveHour,
			MonthlyTokenLimit:  monthly,
			Unlimited:          unlimited,
		})
	}
	if _, err := h.billing.SetPlanConfigs(r.Context(), updates); err != nil {
		h.redirectAdminWithError(w, r, "update_plan_configs_failed: "+err.Error())
		return
	}
	h.redirectAdminWithMessage(w, r, "plan_configs_updated")
}

func parseNonNegativeIntForm(raw string) (int, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return 0, nil
	}
	v, err := strconv.Atoi(text)
	if err != nil || v < 0 {
		return 0, errors.New("invalid non-negative integer")
	}
	return v, nil
}

func (h *handler) adminUserCreate(w http.ResponseWriter, r *http.Request) {
	if h.userService == nil {
		h.redirectAdminWithError(w, r, "create_user_failed: user service unavailable")
		return
	}
	claims := SessionClaimsFromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		h.redirectAdminWithError(w, r, "invalid form data")
		return
	}
	created, err := h.userService.CreateUser(r.Context(), claims, user.CreateUserRequest{
		Email:    strings.TrimSpace(r.FormValue("email")),
		Password: strings.TrimSpace(r.FormValue("password")),
		Role:     strings.TrimSpace(r.FormValue("role")),
	})
	if err != nil {
		h.redirectAdminWithError(w, r, "create_user_failed: "+err.Error())
		return
	}
	h.redirectAdminWithMessage(w, r, "user_created:"+created.User.ID)
}

func (h *handler) adminUserRoleUpdate(w http.ResponseWriter, r *http.Request) {
	if h.userService == nil {
		h.redirectAdminWithError(w, r, "update_user_role_failed: user service unavailable")
		return
	}
	claims := SessionClaimsFromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		h.redirectAdminWithError(w, r, "invalid form data")
		return
	}
	userIDs := parseBatchValues(r.FormValue("user_id"))
	if len(userIDs) == 0 {
		h.redirectAdminWithError(w, r, "missing user_id")
		return
	}
	if containsBatchValue(userIDs, claims.UserID) {
		h.redirectAdminWithError(w, r, "forbidden_self_sensitive_operation")
		return
	}
	role := strings.TrimSpace(r.FormValue("role"))
	if role == "" {
		if r.FormValue("is_admin") == "on" || strings.EqualFold(strings.TrimSpace(r.FormValue("is_admin")), "true") {
			role = "admin"
		} else {
			role = "member"
		}
	}
	lastID := ""
	for _, userID := range userIDs {
		updated, err := h.userService.UpdateUserRole(r.Context(), claims, userID, role)
		if err != nil {
			h.redirectAdminWithError(w, r, "update_user_role_failed: "+userID+": "+err.Error())
			return
		}
		lastID = updated.User.ID
	}
	if len(userIDs) == 1 {
		h.redirectAdminWithMessage(w, r, "user_role_updated:"+lastID)
		return
	}
	h.redirectAdminWithMessage(w, r, "user_role_updated:"+strconv.Itoa(len(userIDs)))
}

func (h *handler) adminUserStatusUpdate(w http.ResponseWriter, r *http.Request) {
	if h.userService == nil {
		h.redirectAdminWithError(w, r, "update_user_status_failed: user service unavailable")
		return
	}
	claims := SessionClaimsFromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		h.redirectAdminWithError(w, r, "invalid form data")
		return
	}
	userIDs := parseBatchValues(r.FormValue("user_id"))
	if len(userIDs) == 0 {
		h.redirectAdminWithError(w, r, "missing user_id")
		return
	}
	if containsBatchValue(userIDs, claims.UserID) {
		h.redirectAdminWithError(w, r, "forbidden_self_sensitive_operation")
		return
	}
	status := strings.TrimSpace(r.FormValue("status"))
	if status == "" {
		if r.FormValue("is_active") == "on" || strings.EqualFold(strings.TrimSpace(r.FormValue("is_active")), "true") {
			status = "active"
		} else {
			status = "disabled"
		}
	}
	lastID := ""
	for _, userID := range userIDs {
		updated, err := h.userService.UpdateUserStatus(r.Context(), claims, userID, status)
		if err != nil {
			h.redirectAdminWithError(w, r, "update_user_status_failed: "+userID+": "+err.Error())
			return
		}
		lastID = updated.User.ID
	}
	if len(userIDs) == 1 {
		h.redirectAdminWithMessage(w, r, "user_status_updated:"+lastID)
		return
	}
	h.redirectAdminWithMessage(w, r, "user_status_updated:"+strconv.Itoa(len(userIDs)))
}

func (h *handler) adminUserMembershipUpdate(w http.ResponseWriter, r *http.Request) {
	if h.billing == nil {
		h.redirectAdminWithError(w, r, "update_user_membership_failed: billing unavailable")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.redirectAdminWithError(w, r, "invalid form data")
		return
	}
	userIDs := parseBatchValues(r.FormValue("user_id"))
	if len(userIDs) == 0 {
		h.redirectAdminWithError(w, r, "missing user_id")
		return
	}
	planID := strings.ToLower(strings.TrimSpace(r.FormValue("plan_id")))
	if planID == "" {
		h.redirectAdminWithError(w, r, "update_user_membership_failed: plan is required")
		return
	}
	periodEndInput := strings.TrimSpace(r.FormValue("period_end"))
	description := strings.TrimSpace(r.FormValue("description"))
	var periodEnd time.Time
	if planID != "free" {
		if periodEndInput == "" {
			h.redirectAdminWithError(w, r, "update_user_membership_failed: period_end is required")
			return
		}
		parsed, err := time.ParseInLocation("2006-01-02T15:04", periodEndInput, time.Local)
		if err != nil {
			h.redirectAdminWithError(w, r, "update_user_membership_failed: invalid period_end")
			return
		}
		periodEnd = parsed.UTC()
	}

	lastID := ""
	for _, userID := range userIDs {
		if _, err := h.billing.SetMembershipAdmin(r.Context(), userID, billing.AdminSetMembershipRequest{
			PlanID:      planID,
			PeriodEnd:   periodEnd,
			Description: description,
		}); err != nil {
			h.redirectAdminWithError(w, r, "update_user_membership_failed: "+userID+": "+err.Error())
			return
		}
		lastID = userID
	}
	if len(userIDs) == 1 {
		h.redirectAdminWithMessage(w, r, "user_membership_updated:"+lastID)
		return
	}
	h.redirectAdminWithMessage(w, r, "user_membership_updated:"+strconv.Itoa(len(userIDs)))
}

func (h *handler) adminUserDelete(w http.ResponseWriter, r *http.Request) {
	if h.userService == nil {
		h.redirectAdminWithError(w, r, "delete_user_failed: user service unavailable")
		return
	}
	claims := SessionClaimsFromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		h.redirectAdminWithError(w, r, "invalid form data")
		return
	}

	userIDs := parseBatchValues(r.FormValue("user_id"))
	if len(userIDs) == 0 {
		h.redirectAdminWithError(w, r, "missing user_id")
		return
	}
	if containsBatchValue(userIDs, claims.UserID) {
		h.redirectAdminWithError(w, r, "forbidden_delete_self")
		return
	}
	lastID := ""
	for _, userID := range userIDs {
		deleted, err := h.userService.DeleteUser(r.Context(), claims, userID)
		if err != nil {
			h.redirectAdminWithError(w, r, "delete_user_failed: "+userID+": "+err.Error())
			return
		}
		lastID = deleted.ID
	}
	if len(userIDs) == 1 {
		h.redirectAdminWithMessage(w, r, "user_deleted:"+lastID)
		return
	}
	h.redirectAdminWithMessage(w, r, "user_deleted:"+strconv.Itoa(len(userIDs)))
}

func (h *handler) adminPromptDelete(w http.ResponseWriter, r *http.Request) {
	if h.promptAPI == nil {
		h.redirectAdminWithError(w, r, "delete_prompt_api_failed: prompt api unavailable")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.redirectAdminWithError(w, r, "invalid form data")
		return
	}

	slugs := parseBatchValues(r.FormValue("slug"))
	if len(slugs) == 0 {
		h.redirectAdminWithError(w, r, "missing slug")
		return
	}
	lastSlug := ""
	for _, slug := range slugs {
		if err := h.promptAPI.Delete(r.Context(), slug); err != nil {
			h.redirectAdminWithError(w, r, "delete_prompt_api_failed: "+slug+": "+err.Error())
			return
		}
		lastSlug = slug
	}
	if len(slugs) == 1 {
		h.redirectAdminWithMessage(w, r, "prompt_api_deleted:"+lastSlug)
		return
	}
	h.redirectAdminWithMessage(w, r, "prompt_api_deleted:"+strconv.Itoa(len(slugs)))
}

func (h *handler) adminDefaultAPIUpdate(w http.ResponseWriter, r *http.Request) {
	if h.promptAPI == nil {
		h.redirectAdminWithError(w, r, "save_default_api_failed: prompt api unavailable")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.redirectAdminWithError(w, r, "invalid form data")
		return
	}

	mappings, err := parseDefaultAPIModelMappings(r.FormValue("model_mappings"))
	if err != nil {
		h.redirectAdminWithError(w, r, "save_default_api_failed: "+err.Error())
		return
	}
	enabled := r.FormValue("enabled") == "on" || strings.EqualFold(strings.TrimSpace(r.FormValue("enabled")), "true")
	defaultModel := strings.TrimSpace(r.FormValue("default_model"))
	allowedModels := uniqueModelTargetsFromMappings(mappings)
	if defaultModel != "" && !containsText(allowedModels, defaultModel) {
		allowedModels = append(allowedModels, defaultModel)
	}
	updated, err := h.promptAPI.UpsertDefaultAPI(r.Context(), promptapi.UpsertDefaultAPIRequest{
		APIKey:          strings.TrimSpace(r.FormValue("api_key")),
		Enabled:         enabled,
		DefaultModel:    defaultModel,
		AllowedModels:   allowedModels,
		ModelIDMappings: mappings,
	})
	if err != nil {
		h.redirectAdminWithError(w, r, "save_default_api_failed: "+err.Error())
		return
	}
	h.redirectAdminWithMessage(w, r, "default_api_saved:"+updated.APIPath)
}

func (h *handler) adminOrderDelete(w http.ResponseWriter, r *http.Request) {
	if h.billing == nil {
		h.redirectAdminWithError(w, r, "delete_order_failed: billing unavailable")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.redirectAdminWithError(w, r, "invalid form data")
		return
	}
	orderIDs := parseBatchValues(r.FormValue("order_id"))
	if len(orderIDs) == 0 {
		h.redirectAdminWithError(w, r, "missing order_id")
		return
	}
	lastID := ""
	for _, orderID := range orderIDs {
		deleted, err := h.billing.DeleteOrderAdmin(r.Context(), orderID)
		if err != nil {
			h.redirectAdminWithError(w, r, "delete_order_failed: "+orderID+": "+err.Error())
			return
		}
		lastID = deleted.ID
	}
	if len(orderIDs) == 1 {
		h.redirectAdminWithMessage(w, r, "order_deleted:"+lastID)
		return
	}
	h.redirectAdminWithMessage(w, r, "order_deleted:"+strconv.Itoa(len(orderIDs)))
}

func (h *handler) redirectAdminWithError(w http.ResponseWriter, r *http.Request, message string) {
	target := adminReturnTo(r, "/admin/users")
	sep := "?"
	if strings.Contains(target, "?") {
		sep = "&"
	}
	http.Redirect(w, r, target+sep+"error="+url.QueryEscape(message), http.StatusSeeOther)
}

func (h *handler) redirectAdminWithMessage(w http.ResponseWriter, r *http.Request, message string) {
	target := adminReturnTo(r, "/admin/users")
	sep := "?"
	if strings.Contains(target, "?") {
		sep = "&"
	}
	http.Redirect(w, r, target+sep+"message="+url.QueryEscape(message), http.StatusSeeOther)
}

func adminReturnTo(r *http.Request, fallback string) string {
	if r == nil {
		return fallback
	}
	candidates := []string{
		strings.TrimSpace(r.FormValue("return_to")),
		strings.TrimSpace(r.URL.Query().Get("return_to")),
	}
	for _, c := range candidates {
		if strings.HasPrefix(c, "/admin") {
			return c
		}
	}
	return fallback
}

func (h *handler) hasAdminAccount(ctx context.Context) (bool, error) {
	if h == nil || h.userService == nil {
		return false, user.ErrStoreUnavailable
	}
	return h.userService.HasAdmin(ctx)
}

func (h *handler) adminClaimsFromRequest(r *http.Request) (user.SessionClaims, bool) {
	if h == nil || h.userService == nil || r == nil {
		return user.SessionClaims{}, false
	}
	cookie, err := r.Cookie(adminSessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return user.SessionClaims{}, false
	}
	claims, err := h.userService.ValidateSessionToken(cookie.Value)
	if err != nil {
		return user.SessionClaims{}, false
	}
	return claims, true
}

func (h *handler) setAdminSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *handler) clearAdminSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func adminSessionAuth(userService *user.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if userService == nil {
				writeLocalizedPlainError(w, r, http.StatusServiceUnavailable, "user_service_unavailable", "user service unavailable")
				return
			}
			cookie, err := r.Cookie(adminSessionCookieName)
			if err != nil || strings.TrimSpace(cookie.Value) == "" {
				http.Redirect(w, r, withQuery(withLangPath("/admin/login", detectLanguage(r)), "error", "sign_in_required"), http.StatusSeeOther)
				return
			}
			claims, err := userService.ValidateSessionToken(cookie.Value)
			if err != nil {
				http.Redirect(w, r, withQuery(withLangPath("/admin/login", detectLanguage(r)), "error", "invalid_session"), http.StatusSeeOther)
				return
			}
			ctx := context.WithValue(r.Context(), sessionKey, claims)
			ctx = context.WithValue(ctx, sessionToken, cookie.Value)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func adminRequireAdminRole() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := SessionClaimsFromContext(r.Context())
			if claims.Role != "admin" {
				writeLocalizedPlainError(w, r, http.StatusForbidden, "forbidden", "forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func adminRequireOwnerRole() func(http.Handler) http.Handler {
	return adminRequireAdminRole()
}

func splitCommaSeparated(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	return out
}

func parseDefaultAPIModelMappings(raw string) ([]promptapi.DefaultAPIModelMapping, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ';'
	})
	out := make([]promptapi.DefaultAPIModelMapping, 0, len(parts))
	for _, part := range parts {
		line := strings.TrimSpace(part)
		if line == "" {
			continue
		}
		pair := strings.SplitN(line, "=>", 2)
		if len(pair) != 2 {
			pair = strings.SplitN(line, "=", 2)
		}
		if len(pair) != 2 {
			return nil, errors.New("invalid model mapping line")
		}
		modelID := strings.TrimSpace(pair[0])
		model := strings.TrimSpace(pair[1])
		if modelID == "" || model == "" {
			return nil, errors.New("invalid model mapping line")
		}
		out = append(out, promptapi.DefaultAPIModelMapping{ModelID: modelID, Model: model})
	}
	return out, nil
}

func uniqueModelTargetsFromMappings(items []promptapi.DefaultAPIModelMapping) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		target := strings.TrimSpace(item.Model)
		if target == "" {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		out = append(out, target)
	}
	return out
}

func containsText(items []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, item := range items {
		if strings.TrimSpace(item) == target {
			return true
		}
	}
	return false
}

func containsBatchValue(items []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, item := range items {
		if strings.TrimSpace(item) == target {
			return true
		}
	}
	return false
}

func buildPromptUpsertRequestFromForm(r *http.Request) promptapi.UpsertRequest {
	variables := splitCommaSeparated(r.FormValue("variables"))
	inputKey := sanitizePromptVarName(r.FormValue("input_key"))
	if inputKey == "" {
		inputKey = "input"
	}
	if len(variables) == 0 {
		variables = []string{inputKey}
	}
	systemPrompt := strings.TrimSpace(r.FormValue("system_prompt"))
	if systemPrompt == "" {
		systemPrompt = strings.TrimSpace(r.FormValue("task_instruction"))
	}
	userTemplate := strings.TrimSpace(r.FormValue("user_template"))
	if userTemplate == "" {
		userTemplate = "{{." + inputKey + "}}"
	}
	return promptapi.UpsertRequest{
		Slug:          strings.TrimSpace(r.FormValue("slug")),
		Name:          strings.TrimSpace(r.FormValue("name")),
		Description:   strings.TrimSpace(r.FormValue("description")),
		Model:         strings.TrimSpace(r.FormValue("model")),
		InputSchema:   strings.TrimSpace(r.FormValue("input_schema")),
		OutputSchema:  strings.TrimSpace(r.FormValue("output_schema")),
		SystemPrompt:  systemPrompt,
		UserTemplate:  userTemplate,
		Variables:     variables,
		ExampleInput:  strings.TrimSpace(r.FormValue("example_input")),
		ExampleOutput: strings.TrimSpace(r.FormValue("example_output")),
		Publish:       r.FormValue("publish") == "on",
	}
}

func sanitizePromptVarName(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var b strings.Builder
	for _, ch := range raw {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			b.WriteRune(ch)
			continue
		}
		if ch == '-' || ch == ' ' {
			b.WriteRune('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func parsePromptPreviewInput(raw string, variables []string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		fallback := make(map[string]any, len(variables))
		for _, v := range variables {
			key := strings.TrimSpace(v)
			if key == "" {
				continue
			}
			fallback[key] = "demo_" + key
		}
		return fallback, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	if payload == nil {
		payload = map[string]any{}
	}
	return payload, nil
}

func adminErrorCode(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("error: %s", err.Error())
}
