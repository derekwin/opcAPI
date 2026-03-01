package http

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/derekwin/opcAPI/internal/audit"
	"github.com/derekwin/opcAPI/internal/billing"
	"github.com/derekwin/opcAPI/internal/keymgmt"
	"github.com/derekwin/opcAPI/internal/promptapi"
	"github.com/derekwin/opcAPI/internal/user"
)

const adminSessionCookieName = "opc_admin_session"

type adminLoginView struct {
	Lang             string
	Message          string
	Error            string
	Channel          string
	Identifier       string
	VerificationCode string
	TenantName       string
}

type adminDashboardView struct {
	Lang       string
	Message    string
	Error      string
	Issues     []string
	Claims     user.SessionClaims
	Me         user.AuthResult
	Users      []user.TenantUser
	Keys       []keymgmt.PolicyRecord
	PromptAPIs []promptapi.Definition
	Balance    billing.Balance
	Orders     []billing.Order
	Audits     []audit.Event
}

var adminLoginTemplate = template.Must(template.New("admin-login").Funcs(template.FuncMap{
	"ui":   adminUI,
	"enum": adminEnum,
}).Parse(`<!doctype html>
<html lang="{{.Lang}}">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>{{ui $.Lang "login_title"}}</title>
  <style>
    :root {
      --bg: #f3f8ff;
      --panel: #ffffff;
      --line: #d8e4f4;
      --ink: #17283f;
      --muted: #5d6f86;
      --primary: #0f5cc0;
      --danger: #b52929;
    }
    body {
      margin: 0;
      padding: 24px;
      font-family: "Avenir Next", "Segoe UI", sans-serif;
      background: radial-gradient(circle at top right, #d9ecff 0, #f3f8ff 42%, #eef5ff 100%);
      color: var(--ink);
    }
    .shell {
      max-width: 620px;
      margin: 24px auto;
      padding: 28px;
      border: 1px solid var(--line);
      border-radius: 16px;
      background: var(--panel);
      box-shadow: 0 14px 32px rgba(8, 29, 61, 0.08);
    }
    h1 {
      margin: 0 0 8px;
      font-size: 28px;
    }
    p {
      margin: 0 0 20px;
      color: var(--muted);
    }
    .msg {
      margin-bottom: 14px;
      border-radius: 10px;
      padding: 10px 12px;
      font-size: 14px;
    }
    .msg.ok {
      border: 1px solid #a8d4af;
      background: #effaf0;
      color: #265f2d;
    }
    .msg.err {
      border: 1px solid #edc0c0;
      background: #fff1f1;
      color: var(--danger);
    }
    form {
      display: grid;
      gap: 12px;
    }
    label {
      font-size: 14px;
      color: var(--muted);
    }
    input, select {
      width: 100%;
      box-sizing: border-box;
      margin-top: 6px;
      padding: 9px 10px;
      border: 1px solid var(--line);
      border-radius: 8px;
      font-size: 14px;
      color: var(--ink);
      background: #fff;
    }
    .actions {
      display: flex;
      gap: 10px;
      margin-top: 6px;
    }
    button {
      border: none;
      border-radius: 8px;
      padding: 10px 14px;
      font-size: 14px;
      cursor: pointer;
    }
    .primary {
      background: var(--primary);
      color: #fff;
    }
    .secondary {
      background: #edf3fb;
      color: var(--ink);
    }
  </style>
</head>
<body>
  <main class="shell">
    <h1>{{ui $.Lang "login_heading"}}</h1>
    <p>{{ui $.Lang "login_subtitle"}}</p>
    {{if .Message}}<div class="msg ok">{{.Message}}</div>{{end}}
    {{if .Error}}<div class="msg err">{{.Error}}</div>{{end}}

    <form method="post" action="/admin/login">
      <label>{{ui $.Lang "channel"}}
        <select name="channel">
          <option value="phone" {{if eq .Channel "phone"}}selected{{end}}>{{enum $.Lang "channel" "phone"}}</option>
          <option value="wechat" {{if eq .Channel "wechat"}}selected{{end}}>{{enum $.Lang "channel" "wechat"}}</option>
          <option value="feishu" {{if eq .Channel "feishu"}}selected{{end}}>{{enum $.Lang "channel" "feishu"}}</option>
        </select>
      </label>
      <label>{{ui $.Lang "identifier"}}
        <input name="identifier" placeholder="{{ui $.Lang "placeholder_phone_openid"}}" value="{{.Identifier}}" required />
      </label>
      <label>{{ui $.Lang "verification_code_phone"}}
        <input name="verification_code" placeholder="000000" value="{{.VerificationCode}}" />
      </label>
      <label>{{ui $.Lang "tenant_name_signup"}}
        <input name="tenant_name" placeholder="demo" value="{{.TenantName}}" />
      </label>
      <div class="actions">
        <button class="primary" type="submit" name="action" value="signin">{{ui $.Lang "sign_in"}}</button>
        <button class="secondary" type="submit" name="action" value="signup">{{ui $.Lang "sign_up"}}</button>
      </div>
    </form>
  </main>
</body>
</html>
`))

var adminDashboardTemplate = template.Must(template.New("admin-dashboard").Funcs(template.FuncMap{
	"ui":   adminUI,
	"enum": adminEnum,
	"fmtTime": func(t time.Time) string {
		if t.IsZero() {
			return "-"
		}
		return t.Local().Format("2006-01-02 15:04:05")
	},
	"fmtMoney": func(v float64) string {
		return fmt.Sprintf("%.2f", v)
	},
	"join": func(items []string) string {
		return strings.Join(items, ",")
	},
	"short": func(v string) string {
		if len(v) <= 16 {
			return v
		}
		return v[:16] + "..."
	},
	"metaText": func(m map[string]string) string {
		if len(m) == 0 {
			return "-"
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, k+"="+m[k])
		}
		return strings.Join(parts, "; ")
	},
}).Parse(`<!doctype html>
<html lang="{{.Lang}}">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>{{ui $.Lang "console_title"}}</title>
  <style>
    :root {
      --bg: #eef4fb;
      --panel: #ffffff;
      --ink: #16273d;
      --muted: #5f6f83;
      --line: #d4dfef;
      --brand: #0f5cc0;
      --danger: #ab2a2a;
      --ok: #247649;
    }
    body {
      margin: 0;
      padding: 22px;
      color: var(--ink);
      font-family: "Avenir Next", "Segoe UI", sans-serif;
      background:
        radial-gradient(circle at 0 0, rgba(187, 223, 255, 0.7), transparent 46%),
        radial-gradient(circle at 100% 0, rgba(221, 236, 255, 0.9), transparent 40%),
        var(--bg);
    }
    .wrap {
      max-width: 1200px;
      margin: 0 auto;
      display: grid;
      gap: 16px;
    }
    .top {
      display: flex;
      gap: 12px;
      align-items: center;
      justify-content: space-between;
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 14px;
      padding: 16px 18px;
      box-shadow: 0 10px 24px rgba(9, 30, 57, 0.08);
    }
    .top h1 {
      margin: 0;
      font-size: 24px;
    }
    .meta {
      color: var(--muted);
      font-size: 13px;
      margin-top: 6px;
    }
    .logout button {
      border: none;
      border-radius: 8px;
      padding: 8px 12px;
      cursor: pointer;
      background: #e8eef8;
      color: var(--ink);
    }
    .msg {
      border-radius: 10px;
      padding: 10px 12px;
      font-size: 14px;
    }
    .msg.ok {
      border: 1px solid #b8e0be;
      color: var(--ok);
      background: #edf8ef;
    }
    .msg.err {
      border: 1px solid #efc2c2;
      color: var(--danger);
      background: #fff0f0;
    }
    .issues {
      margin: 0;
      padding: 0 0 0 18px;
      color: var(--danger);
    }
    .grid {
      display: grid;
      gap: 16px;
      grid-template-columns: repeat(12, minmax(0, 1fr));
    }
    .card {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 14px;
      box-shadow: 0 8px 20px rgba(9, 30, 57, 0.07);
      padding: 14px;
      overflow: auto;
    }
    .col-12 { grid-column: span 12; }
    .col-8 { grid-column: span 8; }
    .col-6 { grid-column: span 6; }
    .col-4 { grid-column: span 4; }
    .col-3 { grid-column: span 3; }
    h2 {
      margin: 0 0 12px;
      font-size: 18px;
    }
    table {
      width: 100%;
      border-collapse: collapse;
      font-size: 13px;
    }
    th, td {
      border-bottom: 1px solid var(--line);
      text-align: left;
      padding: 8px 6px;
      vertical-align: top;
    }
    th {
      color: var(--muted);
      font-weight: 600;
      white-space: nowrap;
    }
    tr:last-child td {
      border-bottom: none;
    }
    form.inline {
      display: inline;
    }
    form.stack {
      display: grid;
      gap: 8px;
    }
    .fields-2 {
      display: grid;
      gap: 8px;
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    .fields-3 {
      display: grid;
      gap: 8px;
      grid-template-columns: repeat(3, minmax(0, 1fr));
    }
    label {
      font-size: 12px;
      color: var(--muted);
      display: grid;
      gap: 5px;
    }
    input, textarea, select {
      box-sizing: border-box;
      width: 100%;
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 8px 10px;
      font-size: 13px;
      color: var(--ink);
      background: #fff;
    }
    textarea { min-height: 80px; resize: vertical; }
    .btn {
      width: fit-content;
      border: none;
      border-radius: 8px;
      padding: 8px 12px;
      cursor: pointer;
      background: var(--brand);
      color: #fff;
      font-size: 13px;
    }
    .btn.danger {
      background: var(--danger);
    }
    .mono {
      font-family: Menlo, Consolas, monospace;
      font-size: 12px;
    }
    @media (max-width: 950px) {
      .col-8, .col-6, .col-4, .col-3 { grid-column: span 12; }
    }
  </style>
</head>
<body>
  <main class="wrap">
    <section class="top">
      <div>
        <h1>{{ui $.Lang "console_title"}}</h1>
        <div class="meta">{{ui $.Lang "tenant"}}={{.Claims.TenantID}} {{ui $.Lang "user"}}={{.Claims.UserID}} {{ui $.Lang "role"}}={{enum $.Lang "role" .Claims.Role}}</div>
      </div>
      <form class="logout" method="post" action="/admin/logout">
        <button type="submit">{{ui $.Lang "sign_out"}}</button>
      </form>
    </section>

    {{if .Message}}<div class="msg ok">{{.Message}}</div>{{end}}
    {{if .Error}}<div class="msg err">{{.Error}}</div>{{end}}
    {{if .Issues}}
      <div class="msg err">
        <strong>{{ui $.Lang "partial_data_errors"}}</strong>
        <ul class="issues">
          {{range .Issues}}<li>{{.}}</li>{{end}}
        </ul>
      </div>
    {{end}}

    <section class="grid">
      <article class="card col-3">
        <h2>{{ui $.Lang "tenant"}}</h2>
        <table>
          <tr><th>{{ui $.Lang "id"}}</th><td class="mono">{{.Me.Tenant.ID}}</td></tr>
          <tr><th>{{ui $.Lang "name"}}</th><td>{{.Me.Tenant.Name}}</td></tr>
          <tr><th>{{ui $.Lang "plan"}}</th><td>{{.Me.Tenant.PlanID}}</td></tr>
          <tr><th>{{ui $.Lang "status"}}</th><td>{{.Me.Tenant.Status}}</td></tr>
          <tr><th>{{ui $.Lang "created"}}</th><td>{{fmtTime .Me.Tenant.CreatedAt}}</td></tr>
        </table>
      </article>

      <article class="card col-3">
        <h2>{{ui $.Lang "current_user"}}</h2>
        <table>
          <tr><th>{{ui $.Lang "id"}}</th><td class="mono">{{.Me.User.ID}}</td></tr>
          <tr><th>{{ui $.Lang "phone"}}</th><td>{{.Me.User.Phone}}</td></tr>
          <tr><th>{{ui $.Lang "wechat"}}</th><td>{{.Me.User.WeChatOpenID}}</td></tr>
          <tr><th>{{ui $.Lang "feishu"}}</th><td>{{.Me.User.FeishuOpenID}}</td></tr>
          <tr><th>{{ui $.Lang "status"}}</th><td>{{.Me.User.Status}}</td></tr>
        </table>
      </article>

      <article class="card col-3">
        <h2>{{ui $.Lang "balance"}}</h2>
        <p><strong>{{fmtMoney .Balance.Amount}} {{.Balance.Currency}}</strong></p>
        <p class="meta">{{ui $.Lang "updated"}}: {{fmtTime .Balance.UpdatedAt}}</p>
        <form class="stack" method="post" action="/admin/billing/topup">
          <div class="fields-3">
            <label>{{ui $.Lang "amount"}}<input name="amount" value="100" required /></label>
            <label>{{ui $.Lang "currency"}}<input name="currency" value="CNY" required /></label>
            <label>{{ui $.Lang "channel"}}<input name="channel" value="manual" required /></label>
          </div>
          <label>{{ui $.Lang "description"}}<input name="description" placeholder="{{ui $.Lang "placeholder_manual_topup"}}" /></label>
          <button class="btn" type="submit">{{ui $.Lang "top_up"}}</button>
        </form>
      </article>

      <article class="card col-3">
        <h2>{{ui $.Lang "quick_stats"}}</h2>
        <table>
          <tr><th>{{ui $.Lang "users"}}</th><td>{{len .Users}}</td></tr>
          <tr><th>{{ui $.Lang "api_keys"}}</th><td>{{len .Keys}}</td></tr>
          <tr><th>{{ui $.Lang "prompt_apis"}}</th><td>{{len .PromptAPIs}}</td></tr>
          <tr><th>{{ui $.Lang "recent_orders"}}</th><td>{{len .Orders}}</td></tr>
          <tr><th>{{ui $.Lang "audit_logs"}}</th><td>{{len .Audits}}</td></tr>
        </table>
      </article>

      <article class="card col-6">
        <h2>{{ui $.Lang "api_keys"}}</h2>
        <form class="stack" method="post" action="/admin/keys/new">
          <div class="fields-3">
            <label>{{ui $.Lang "api_key"}}<input name="api_key" placeholder="sk-opcapi-..." required /></label>
            <label>{{ui $.Lang "rpm_limit"}}<input name="rpm_limit" value="120" /></label>
            <label>{{ui $.Lang "daily_token_limit"}}<input name="daily_token_limit" value="1000000" /></label>
          </div>
          <label><input type="checkbox" name="enabled" checked /> {{ui $.Lang "enabled"}}</label>
          <button class="btn" type="submit">{{ui $.Lang "save_api_key_policy"}}</button>
        </form>
        <table>
          <thead>
            <tr>
              <th>{{ui $.Lang "key_hash"}}</th>
              <th>{{ui $.Lang "tenant"}}</th>
              <th>{{ui $.Lang "enabled"}}</th>
              <th>{{ui $.Lang "rpm"}}</th>
              <th>{{ui $.Lang "daily"}}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {{range .Keys}}
              <tr>
                <td class="mono" title="{{.KeyHash}}">{{short .KeyHash}}</td>
                <td class="mono">{{.TenantID}}</td>
                <td>{{.Enabled}}</td>
                <td>{{.RPMLimit}}</td>
                <td>{{.DailyTokenLimit}}</td>
                <td>
                  <form class="inline" method="post" action="/admin/keys/delete">
                    <input type="hidden" name="key_hash" value="{{.KeyHash}}" />
                    <button class="btn danger" type="submit">{{ui $.Lang "delete"}}</button>
                  </form>
                </td>
              </tr>
            {{end}}
          </tbody>
        </table>
      </article>

      <article class="card col-6">
        <h2>{{ui $.Lang "prompt_apis"}}</h2>
        <form class="stack" method="post" action="/admin/prompt-apis/new">
          <div class="fields-3">
            <label>{{ui $.Lang "slug"}}<input name="slug" placeholder="{{ui $.Lang "placeholder_email_summary"}}" required /></label>
            <label>{{ui $.Lang "name"}}<input name="name" placeholder="{{ui $.Lang "placeholder_email_summary_name"}}" required /></label>
            <label>{{ui $.Lang "model"}}<input name="model" value="stub/test" required /></label>
          </div>
          <label>{{ui $.Lang "description"}}<input name="description" /></label>
          <label>{{ui $.Lang "system_prompt"}}<textarea name="system_prompt"></textarea></label>
          <label>{{ui $.Lang "user_template"}}<textarea name="user_template" required>{{ui $.Lang "default_user_template"}}</textarea></label>
          <label>{{ui $.Lang "variables_comma"}}<input name="variables" value="content" /></label>
          <label><input type="checkbox" name="publish" checked /> {{ui $.Lang "publish"}}</label>
          <button class="btn" type="submit">{{ui $.Lang "save_prompt_api"}}</button>
        </form>
        <table>
          <thead>
            <tr>
              <th>{{ui $.Lang "slug"}}</th>
              <th>{{ui $.Lang "status"}}</th>
              <th>{{ui $.Lang "version"}}</th>
              <th>{{ui $.Lang "model"}}</th>
              <th>{{ui $.Lang "variables"}}</th>
              <th>{{ui $.Lang "updated"}}</th>
            </tr>
          </thead>
          <tbody>
            {{range .PromptAPIs}}
              <tr>
                <td class="mono">{{.Slug}}</td>
                <td>{{enum $.Lang "status" .Status}}</td>
                <td>{{.Version}}</td>
                <td class="mono">{{.Model}}</td>
                <td class="mono">{{join .Variables}}</td>
                <td>{{fmtTime .UpdatedAt}}</td>
              </tr>
            {{end}}
          </tbody>
        </table>
      </article>

      <article class="card col-4">
        <h2>{{ui $.Lang "users"}}</h2>
        {{if eq .Claims.Role "owner"}}
        <form class="stack" method="post" action="/admin/users/new">
          <div class="fields-2">
            <label>{{ui $.Lang "channel"}}
              <select name="channel">
                <option value="phone">{{enum $.Lang "channel" "phone"}}</option>
                <option value="wechat">{{enum $.Lang "channel" "wechat"}}</option>
                <option value="feishu">{{enum $.Lang "channel" "feishu"}}</option>
              </select>
            </label>
            <label>{{ui $.Lang "role"}}
              <select name="role">
                <option value="admin">{{enum $.Lang "role" "admin"}}</option>
                <option value="member">{{enum $.Lang "role" "member"}}</option>
              </select>
            </label>
          </div>
          <label>{{ui $.Lang "identifier"}}<input name="identifier" placeholder="{{ui $.Lang "placeholder_phone_openid"}}" required /></label>
          <label>{{ui $.Lang "verification_code_phone_only"}}<input name="verification_code" placeholder="000000" /></label>
          <button class="btn" type="submit">{{ui $.Lang "create_user"}}</button>
        </form>
        {{end}}
        <table>
          <thead>
            <tr><th>{{ui $.Lang "user_id"}}</th><th>{{ui $.Lang "role"}}</th><th>{{ui $.Lang "status"}}</th></tr>
          </thead>
          <tbody>
            {{range .Users}}
              <tr>
                <td class="mono">{{.User.ID}}</td>
                <td>
                  {{if and (eq $.Claims.Role "owner") (ne .Role "owner")}}
                    <form class="inline" method="post" action="/admin/users/role">
                      <input type="hidden" name="user_id" value="{{.User.ID}}" />
                      <select name="role">
                        <option value="admin" {{if eq .Role "admin"}}selected{{end}}>{{enum $.Lang "role" "admin"}}</option>
                        <option value="member" {{if eq .Role "member"}}selected{{end}}>{{enum $.Lang "role" "member"}}</option>
                      </select>
                      <button class="btn" type="submit">{{ui $.Lang "save"}}</button>
                    </form>
                  {{else}}
                    {{enum $.Lang "role" .Role}}
                  {{end}}
                </td>
                <td>
                  {{if and (eq $.Claims.Role "owner") (ne .Role "owner")}}
                    <form class="inline" method="post" action="/admin/users/status">
                      <input type="hidden" name="user_id" value="{{.User.ID}}" />
                      <select name="status">
                        <option value="active" {{if eq .User.Status "active"}}selected{{end}}>{{enum $.Lang "status" "active"}}</option>
                        <option value="disabled" {{if eq .User.Status "disabled"}}selected{{end}}>{{enum $.Lang "status" "disabled"}}</option>
                      </select>
                      <button class="btn" type="submit">{{ui $.Lang "save"}}</button>
                    </form>
                  {{else}}
                    {{enum $.Lang "status" .User.Status}}
                  {{end}}
                </td>
              </tr>
            {{end}}
          </tbody>
        </table>
      </article>

      <article class="card col-8">
        <h2>{{ui $.Lang "recent_orders"}}</h2>
        <table>
          <thead>
            <tr>
              <th>{{ui $.Lang "id"}}</th>
              <th>{{ui $.Lang "type"}}</th>
              <th>{{ui $.Lang "channel"}}</th>
              <th>{{ui $.Lang "amount"}}</th>
              <th>{{ui $.Lang "status"}}</th>
              <th>{{ui $.Lang "created"}}</th>
            </tr>
          </thead>
          <tbody>
            {{range .Orders}}
              <tr>
                <td class="mono">{{.ID}}</td>
                <td>{{enum $.Lang "order_type" .Type}}</td>
                <td>{{enum $.Lang "channel" .Channel}}</td>
                <td>{{fmtMoney .Amount}} {{.Currency}}</td>
                <td>{{enum $.Lang "order_status" .Status}}</td>
                <td>{{fmtTime .CreatedAt}}</td>
              </tr>
            {{end}}
          </tbody>
        </table>
      </article>

      <article class="card col-12">
        <h2>{{ui $.Lang "audit_logs"}}</h2>
        <table>
          <thead>
            <tr>
              <th>{{ui $.Lang "id"}}</th>
              <th>{{ui $.Lang "resource"}}</th>
              <th>{{ui $.Lang "action"}}</th>
              <th>{{ui $.Lang "actor"}}</th>
              <th>{{ui $.Lang "target"}}</th>
              <th>{{ui $.Lang "metadata"}}</th>
              <th>{{ui $.Lang "created"}}</th>
            </tr>
          </thead>
          <tbody>
            {{range .Audits}}
              <tr>
                <td class="mono">{{.ID}}</td>
                <td>{{enum $.Lang "audit_resource" .Resource}}</td>
                <td>{{enum $.Lang "audit_action" .Action}}</td>
                <td class="mono">{{.ActorUserID}}</td>
                <td class="mono">{{.TargetID}}</td>
                <td class="mono">{{metaText .Metadata}}</td>
                <td>{{fmtTime .CreatedAt}}</td>
              </tr>
            {{end}}
          </tbody>
        </table>
      </article>
    </section>
  </main>
</body>
</html>
`))

func (h *handler) adminLoginPage(w http.ResponseWriter, r *http.Request) {
	if claims, ok := h.adminClaimsFromRequest(r); ok && (claims.Role == "owner" || claims.Role == "admin") {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	view := adminLoginView{
		Lang:    detectLanguage(r),
		Message: adminFlashMessage(r, r.URL.Query().Get("msg")),
		Error:   adminErrorMessage(r, r.URL.Query().Get("error")),
		Channel: "phone",
	}
	w.Header().Set("Content-Language", view.Lang)
	_ = adminLoginTemplate.Execute(w, view)
}

func (h *handler) adminLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if h.userService == nil {
		writeLocalizedPlainError(w, r, http.StatusServiceUnavailable, "user_service_unavailable", "user service unavailable")
		return
	}
	if err := r.ParseForm(); err != nil {
		adminRenderLogin(w, r, adminLoginView{Error: "invalid_form_data", Channel: "phone"})
		return
	}

	channel := strings.TrimSpace(r.FormValue("channel"))
	identifier := strings.TrimSpace(r.FormValue("identifier"))
	code := strings.TrimSpace(r.FormValue("verification_code"))
	tenantName := strings.TrimSpace(r.FormValue("tenant_name"))
	action := strings.ToLower(strings.TrimSpace(r.FormValue("action")))

	view := adminLoginView{
		Channel:          channel,
		Identifier:       identifier,
		VerificationCode: code,
		TenantName:       tenantName,
	}

	var (
		res user.AuthResult
		err error
	)
	if action == "signup" {
		res, err = h.userService.Signup(r.Context(), user.SignupRequest{
			Channel:          channel,
			Identifier:       identifier,
			VerificationCode: code,
			TenantName:       tenantName,
		})
	} else {
		res, err = h.userService.Signin(r.Context(), user.SigninRequest{
			Channel:          channel,
			Identifier:       identifier,
			VerificationCode: code,
		})
	}
	if err != nil {
		view.Error = adminUserErrorCode(err)
		adminRenderLogin(w, r, view)
		return
	}

	setAdminSessionCookie(w, res.Token, res.Claims.Exp)
	http.Redirect(w, r, "/admin?msg=sign_in_success", http.StatusSeeOther)
}

func (h *handler) adminLogout(w http.ResponseWriter, r *http.Request) {
	clearAdminSessionCookie(w)
	http.Redirect(w, r, "/admin/login?msg=signed_out", http.StatusSeeOther)
}

func (h *handler) adminDashboard(w http.ResponseWriter, r *http.Request) {
	claims := SessionClaimsFromContext(r.Context())
	view := adminDashboardView{
		Lang:    detectLanguage(r),
		Message: adminFlashMessage(r, r.URL.Query().Get("msg")),
		Error:   adminErrorMessage(r, r.URL.Query().Get("error")),
		Claims:  claims,
	}

	if h.userService == nil {
		view.Error = adminErrorMessage(r, "user_service_unavailable")
		w.Header().Set("Content-Language", view.Lang)
		_ = adminDashboardTemplate.Execute(w, view)
		return
	}

	me, err := h.userService.GetMe(r.Context(), claims)
	if err != nil {
		view.Error = adminErrorMessage(r, adminUserErrorCode(err))
		w.Header().Set("Content-Language", view.Lang)
		_ = adminDashboardTemplate.Execute(w, view)
		return
	}
	view.Me = me

	if users, listErr := h.userService.ListTenantUsers(r.Context(), claims); listErr == nil {
		view.Users = users
	} else {
		view.Issues = append(view.Issues, adminIssueMessage(r, "list_users", listErr))
	}

	if h.policySync != nil {
		if keys, _, listErr := h.policySync.List(r.Context(), keymgmt.ListQuery{TenantID: claims.TenantID, Limit: 200}); listErr == nil {
			view.Keys = keys
		} else {
			view.Issues = append(view.Issues, adminIssueMessage(r, "list_api_keys", listErr))
		}
	}

	if h.promptAPI != nil {
		if page, listErr := h.promptAPI.List(r.Context(), promptapi.ListQuery{TenantID: claims.TenantID, Limit: 200}); listErr == nil {
			view.PromptAPIs = page.Data
		} else {
			view.Issues = append(view.Issues, adminIssueMessage(r, "list_prompt_apis", listErr))
		}
	}

	if h.billing != nil {
		if balance, balErr := h.billing.GetBalance(r.Context(), claims.TenantID); balErr == nil {
			view.Balance = balance
		} else {
			view.Issues = append(view.Issues, adminIssueMessage(r, "load_balance", balErr))
		}
		if orders, orderErr := h.billing.ListOrdersAdmin(r.Context(), billing.OrderQuery{TenantID: claims.TenantID, Limit: 20}); orderErr == nil {
			view.Orders = orders.Data
		} else {
			view.Issues = append(view.Issues, adminIssueMessage(r, "list_orders", orderErr))
		}
	}

	if h.audit != nil {
		if page, auditErr := h.audit.List(r.Context(), audit.Query{TargetTenantID: claims.TenantID, Limit: 30}); auditErr == nil {
			view.Audits = page.Data
		} else {
			view.Issues = append(view.Issues, adminIssueMessage(r, "list_audits", auditErr))
		}
	}

	w.Header().Set("Content-Language", view.Lang)
	_ = adminDashboardTemplate.Execute(w, view)
}

func (h *handler) adminKeyCreate(w http.ResponseWriter, r *http.Request) {
	claims := SessionClaimsFromContext(r.Context())
	if h.policySync == nil {
		adminRedirect(w, r, "", "policy_sync_unavailable")
		return
	}
	if err := r.ParseForm(); err != nil {
		adminRedirect(w, r, "", "invalid_form_data")
		return
	}

	apiKey := strings.TrimSpace(r.FormValue("api_key"))
	if apiKey == "" {
		adminRedirect(w, r, "", "api_key_required")
		return
	}
	enabled := parseCheckbox(r.FormValue("enabled"))
	rpm, err := parseOptionalPositiveInt(r.FormValue("rpm_limit"))
	if err != nil {
		adminRedirect(w, r, "", "rpm_limit_invalid")
		return
	}
	daily, err := parseOptionalPositiveInt(r.FormValue("daily_token_limit"))
	if err != nil {
		adminRedirect(w, r, "", "daily_token_limit_invalid")
		return
	}

	tenantID := claims.TenantID
	record, err := h.policySync.Upsert(r.Context(), keymgmt.UpsertPolicyInput{
		APIKey:          apiKey,
		TenantID:        &tenantID,
		Enabled:         &enabled,
		RPMLimit:        rpm,
		DailyTokenLimit: daily,
	})
	if err != nil {
		adminRedirect(w, r, "", "save_api_key_failed:"+err.Error())
		return
	}
	if h.audit != nil {
		_, _ = h.audit.Record(r.Context(), audit.RecordInput{
			Resource:       "api_key",
			Action:         "upsert",
			ActorUserID:    claims.UserID,
			ActorTenantID:  claims.TenantID,
			TargetID:       record.KeyHash,
			TargetTenantID: record.TenantID,
			Metadata: map[string]string{
				"enabled":           strconv.FormatBool(record.Enabled),
				"rpm_limit":         strconv.Itoa(record.RPMLimit),
				"daily_token_limit": strconv.Itoa(record.DailyTokenLimit),
			},
		})
	}
	adminRedirect(w, r, "api_key_saved", "")
}

func (h *handler) adminKeyDelete(w http.ResponseWriter, r *http.Request) {
	claims := SessionClaimsFromContext(r.Context())
	if h.policySync == nil {
		adminRedirect(w, r, "", "policy_sync_unavailable")
		return
	}
	if err := r.ParseForm(); err != nil {
		adminRedirect(w, r, "", "invalid_form_data")
		return
	}

	keyHash := strings.TrimSpace(r.FormValue("key_hash"))
	if keyHash == "" {
		adminRedirect(w, r, "", "key_hash_required")
		return
	}
	if err := h.policySync.DeleteForTenant(r.Context(), keyHash, claims.TenantID); err != nil {
		adminRedirect(w, r, "", "delete_api_key_failed:"+err.Error())
		return
	}
	if h.audit != nil {
		_, _ = h.audit.Record(r.Context(), audit.RecordInput{
			Resource:      "api_key",
			Action:        "delete",
			ActorUserID:   claims.UserID,
			ActorTenantID: claims.TenantID,
			TargetID:      keyHash,
		})
	}
	adminRedirect(w, r, "api_key_deleted", "")
}

func (h *handler) adminPromptCreate(w http.ResponseWriter, r *http.Request) {
	claims := SessionClaimsFromContext(r.Context())
	if h.promptAPI == nil {
		adminRedirect(w, r, "", "prompt_api_unavailable")
		return
	}
	if err := r.ParseForm(); err != nil {
		adminRedirect(w, r, "", "invalid_form_data")
		return
	}

	variables := splitVariables(r.FormValue("variables"))
	def, err := h.promptAPI.Upsert(r.Context(), claims.TenantID, promptapi.UpsertRequest{
		Slug:         strings.TrimSpace(r.FormValue("slug")),
		Name:         strings.TrimSpace(r.FormValue("name")),
		Description:  strings.TrimSpace(r.FormValue("description")),
		Model:        strings.TrimSpace(r.FormValue("model")),
		SystemPrompt: strings.TrimSpace(r.FormValue("system_prompt")),
		UserTemplate: strings.TrimSpace(r.FormValue("user_template")),
		Variables:    variables,
		Publish:      parseCheckbox(r.FormValue("publish")),
	})
	if err != nil {
		adminRedirect(w, r, "", "save_prompt_api_failed:"+err.Error())
		return
	}
	adminRedirect(w, r, "prompt_api_saved:"+def.Slug, "")
}

func (h *handler) adminBillingTopup(w http.ResponseWriter, r *http.Request) {
	claims := SessionClaimsFromContext(r.Context())
	if h.billing == nil {
		adminRedirect(w, r, "", "billing_unavailable")
		return
	}
	if err := r.ParseForm(); err != nil {
		adminRedirect(w, r, "", "invalid_form_data")
		return
	}

	amount, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue("amount")), 64)
	if err != nil || amount <= 0 {
		adminRedirect(w, r, "", "amount_invalid")
		return
	}

	_, _, err = h.billing.Topup(r.Context(), claims.TenantID, billing.TopupRequest{
		Channel:     strings.TrimSpace(r.FormValue("channel")),
		Amount:      amount,
		Currency:    strings.TrimSpace(r.FormValue("currency")),
		Description: strings.TrimSpace(r.FormValue("description")),
	})
	if err != nil {
		adminRedirect(w, r, "", "topup_failed:"+err.Error())
		return
	}
	adminRedirect(w, r, "topup_success", "")
}

func (h *handler) adminUserCreate(w http.ResponseWriter, r *http.Request) {
	claims := SessionClaimsFromContext(r.Context())
	if h.userService == nil {
		adminRedirect(w, r, "", "user_service_unavailable")
		return
	}
	if err := r.ParseForm(); err != nil {
		adminRedirect(w, r, "", "invalid_form_data")
		return
	}

	created, err := h.userService.CreateTenantUser(r.Context(), claims, user.CreateTenantUserRequest{
		Channel:          strings.TrimSpace(r.FormValue("channel")),
		Identifier:       strings.TrimSpace(r.FormValue("identifier")),
		VerificationCode: strings.TrimSpace(r.FormValue("verification_code")),
		Role:             strings.TrimSpace(r.FormValue("role")),
	})
	if err != nil {
		adminRedirect(w, r, "", "create_user_failed:"+adminUserErrorCode(err))
		return
	}
	if h.audit != nil {
		_, _ = h.audit.Record(r.Context(), audit.RecordInput{
			Resource:       "user",
			Action:         "create",
			ActorUserID:    claims.UserID,
			ActorTenantID:  claims.TenantID,
			TargetID:       created.User.ID,
			TargetTenantID: claims.TenantID,
			Metadata: map[string]string{
				"role": created.Role,
			},
		})
	}
	adminRedirect(w, r, "user_created:"+created.User.ID, "")
}

func (h *handler) adminUserRoleUpdate(w http.ResponseWriter, r *http.Request) {
	claims := SessionClaimsFromContext(r.Context())
	if h.userService == nil {
		adminRedirect(w, r, "", "user_service_unavailable")
		return
	}
	if err := r.ParseForm(); err != nil {
		adminRedirect(w, r, "", "invalid_form_data")
		return
	}

	userID := strings.TrimSpace(r.FormValue("user_id"))
	role := strings.TrimSpace(r.FormValue("role"))
	updated, err := h.userService.UpdateTenantUserRole(r.Context(), claims, userID, role)
	if err != nil {
		adminRedirect(w, r, "", "update_user_role_failed:"+adminUserErrorCode(err))
		return
	}
	if h.audit != nil {
		_, _ = h.audit.Record(r.Context(), audit.RecordInput{
			Resource:       "user",
			Action:         "update_role",
			ActorUserID:    claims.UserID,
			ActorTenantID:  claims.TenantID,
			TargetID:       updated.User.ID,
			TargetTenantID: claims.TenantID,
			Metadata: map[string]string{
				"role": updated.Role,
			},
		})
	}
	adminRedirect(w, r, "user_role_updated:"+updated.User.ID, "")
}

func (h *handler) adminUserStatusUpdate(w http.ResponseWriter, r *http.Request) {
	claims := SessionClaimsFromContext(r.Context())
	if h.userService == nil {
		adminRedirect(w, r, "", "user_service_unavailable")
		return
	}
	if err := r.ParseForm(); err != nil {
		adminRedirect(w, r, "", "invalid_form_data")
		return
	}

	userID := strings.TrimSpace(r.FormValue("user_id"))
	status := strings.TrimSpace(r.FormValue("status"))
	updated, err := h.userService.UpdateTenantUserStatus(r.Context(), claims, userID, status)
	if err != nil {
		adminRedirect(w, r, "", "update_user_status_failed:"+adminUserErrorCode(err))
		return
	}
	if h.audit != nil {
		_, _ = h.audit.Record(r.Context(), audit.RecordInput{
			Resource:       "user",
			Action:         "update_status",
			ActorUserID:    claims.UserID,
			ActorTenantID:  claims.TenantID,
			TargetID:       updated.User.ID,
			TargetTenantID: claims.TenantID,
			Metadata: map[string]string{
				"status": updated.User.Status,
			},
		})
	}
	adminRedirect(w, r, "user_status_updated:"+updated.User.ID, "")
}

func adminSessionAuth(userService *user.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if userService == nil {
				writeLocalizedPlainError(w, r, http.StatusServiceUnavailable, "user_service_unavailable", "user service unavailable")
				return
			}

			token, ok := adminSessionTokenFromRequest(r)
			if !ok {
				clearAdminSessionCookie(w)
				http.Redirect(w, r, "/admin/login?error=sign_in_required", http.StatusSeeOther)
				return
			}
			claims, err := userService.ParseSessionToken(token)
			if err != nil {
				clearAdminSessionCookie(w)
				http.Redirect(w, r, "/admin/login?error=invalid_session", http.StatusSeeOther)
				return
			}
			if _, err := userService.GetMe(r.Context(), claims); err != nil {
				clearAdminSessionCookie(w)
				if errors.Is(err, user.ErrUserDisabled) {
					http.Redirect(w, r, "/admin/login?error=user_disabled", http.StatusSeeOther)
					return
				}
				http.Redirect(w, r, "/admin/login?error=invalid_session", http.StatusSeeOther)
				return
			}

			ctx := context.WithValue(r.Context(), sessionKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func adminRequireAdminRole() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := SessionClaimsFromContext(r.Context())
			if claims.Role != "owner" && claims.Role != "admin" {
				writeLocalizedPlainError(w, r, http.StatusForbidden, "forbidden", "forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func adminRequireOwnerRole() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := SessionClaimsFromContext(r.Context())
			if claims.Role != "owner" {
				writeLocalizedPlainError(w, r, http.StatusForbidden, "owner_required", "forbidden: owner required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (h *handler) adminClaimsFromRequest(r *http.Request) (user.SessionClaims, bool) {
	if h == nil || h.userService == nil {
		return user.SessionClaims{}, false
	}
	token, ok := adminSessionTokenFromRequest(r)
	if !ok {
		return user.SessionClaims{}, false
	}
	claims, err := h.userService.ParseSessionToken(token)
	if err != nil {
		return user.SessionClaims{}, false
	}
	return claims, true
}

func adminSessionTokenFromRequest(r *http.Request) (string, bool) {
	if cookie, err := r.Cookie(adminSessionCookieName); err == nil {
		if token := strings.TrimSpace(cookie.Value); token != "" {
			return token, true
		}
	}
	if token, err := bearerToken(r.Header.Get("Authorization")); err == nil {
		if strings.TrimSpace(token) != "" {
			return token, true
		}
	}
	return "", false
}

func setAdminSessionCookie(w http.ResponseWriter, token string, exp int64) {
	cookie := &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	if exp > 0 {
		expires := time.Unix(exp, 0)
		cookie.Expires = expires
		if maxAge := int(time.Until(expires).Seconds()); maxAge > 0 {
			cookie.MaxAge = maxAge
		}
	}
	http.SetCookie(w, cookie)
}

func clearAdminSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func adminRenderLogin(w http.ResponseWriter, r *http.Request, view adminLoginView) {
	if strings.TrimSpace(view.Channel) == "" {
		view.Channel = "phone"
	}
	if strings.TrimSpace(view.Lang) == "" {
		view.Lang = detectLanguage(r)
	}
	view.Message = adminFlashMessage(r, view.Message)
	view.Error = adminErrorMessage(r, view.Error)
	w.Header().Set("Content-Language", view.Lang)
	_ = adminLoginTemplate.Execute(w, view)
}

func adminRedirect(w http.ResponseWriter, r *http.Request, message, errMsg string) {
	values := make(url.Values)
	if strings.TrimSpace(message) != "" {
		values.Set("msg", message)
	}
	if strings.TrimSpace(errMsg) != "" {
		values.Set("error", errMsg)
	}
	target := "/admin"
	if encoded := values.Encode(); encoded != "" {
		target += "?" + encoded
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func parseCheckbox(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	switch v {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func parseOptionalPositiveInt(raw string) (*int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return nil, errors.New("invalid int")
	}
	return &v, nil
}

func splitVariables(raw string) []string {
	raw = strings.ReplaceAll(raw, "\n", ",")
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		v := strings.TrimSpace(part)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func adminUserErrorCode(err error) string {
	switch {
	case errors.Is(err, user.ErrInvalidChannel), errors.Is(err, user.ErrInvalidIdentifier), errors.Is(err, user.ErrInvalidVerifyCode), errors.Is(err, user.ErrInvalidRole), errors.Is(err, user.ErrInvalidStatus):
		return "invalid_auth_request"
	case errors.Is(err, user.ErrUserExists):
		return "user_exists_try_signin"
	case errors.Is(err, user.ErrUserNotFound):
		return "user_not_found_try_signup"
	case errors.Is(err, user.ErrUserDisabled):
		return "user_disabled"
	case errors.Is(err, user.ErrTenantNotFound):
		return "tenant_not_found"
	case errors.Is(err, user.ErrSessionTokenInvalid), errors.Is(err, user.ErrSessionTokenExpired):
		return "session_invalid_or_expired"
	case errors.Is(err, user.ErrForbidden), strings.Contains(err.Error(), "forbidden"):
		return "permission_denied"
	default:
		return "internal_error"
	}
}
