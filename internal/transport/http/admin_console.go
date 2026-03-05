package http

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/derekwin/opcAPI/internal/billing"
	"github.com/derekwin/opcAPI/internal/promptapi"
	"github.com/derekwin/opcAPI/internal/upstream"
	"github.com/derekwin/opcAPI/internal/user"
)

type adminBaseView struct {
	Claims  user.SessionClaims
	Me      user.AuthResult
	Active  string
	Lang    string
	Path    string
	Message string
	Error   string
}

type adminUsersPageView struct {
	adminBaseView
	Rows        []adminUserRow
	Pagination  adminPagination
	TotalUsers  int
	ActiveUsers int
	AdminUsers  int
	PlanConfigs []billing.PlanConfig
	PlanMap     map[string]billing.PlanConfig
	FilterQ     string
}

type adminPromptsPageView struct {
	adminBaseView
	PromptAPIs   []promptapi.Definition
	DefaultAPIs  []promptapi.DefaultAPIConfig
	Pagination   adminPagination
	Published    int
	Unpublished  int
	FilterQ      string
	TotalPrompts int
	ModelOptions []string
}

type adminOrdersPageView struct {
	adminBaseView
	Orders      []billing.Order
	Pagination  adminPagination
	TotalOrders int
	FilterID    string
	FilterUser  string
	FilterType  string
	FilterChan  string
	FilterStat  string
}

type adminUpstreamsPageView struct {
	adminBaseView
	Upstreams          []upstream.Upstream
	UpstreamOptions    []upstream.Upstream
	UpstreamModelMap   map[string][]string
	Routes             []upstream.ModelRoute
	RuntimeStats       []upstream.RuntimeStat
	UpstreamPagination adminPagination
	RoutePagination    adminPagination
	RuntimePagination  adminPagination
	FilterUpQ          string
	FilterRouteQ       string
	FilterRunQ         string
}

type adminUserRow struct {
	User       user.TenantUser
	Membership billing.Membership
	Policy     billing.UsagePolicy
}

type adminPagination struct {
	Page       int
	PageSize   int
	Total      int
	TotalPages int
	HasPrev    bool
	HasNext    bool
	PrevURL    string
	NextURL    string
}

var adminConsoleTemplateFuncs = template.FuncMap{
	"ui":   adminUI,
	"enum": adminEnum,
	"uiOr": func(lang, key, fallback string) string {
		v := adminUI(lang, key)
		if v == key {
			return fallback
		}
		return v
	},
	"t": func(lang, zhText, enText string) string {
		if isChineseLang(lang) {
			return zhText
		}
		return enText
	},
	"langCode": func(lang string) string {
		if isChineseLang(lang) {
			return languageZhCN
		}
		return languageEN
	},
	"withLang": withLangPath,
	"routeTargets": func(items []upstream.RouteTarget) string {
		out := make([]string, 0, len(items))
		for _, item := range items {
			out = append(out, fmt.Sprintf("%s:%s:w%d:p%d:en=%t", item.UpstreamID, item.Model, item.Weight, item.Priority, item.Enabled))
		}
		return strings.Join(out, ", ")
	},
	"routeTargetsForm": func(items []upstream.RouteTarget) string {
		out := make([]string, 0, len(items))
		for _, item := range items {
			out = append(out, fmt.Sprintf("%s:%s:%d:%d", item.UpstreamID, item.Model, item.Weight, item.Priority))
		}
		return strings.Join(out, ",")
	},
	"defaultAPIMappingsText": func(items []promptapi.DefaultAPIModelMapping) string {
		if len(items) == 0 {
			return ""
		}
		lines := make([]string, 0, len(items))
		for _, item := range items {
			modelID := strings.TrimSpace(item.ModelID)
			model := strings.TrimSpace(item.Model)
			if modelID == "" || model == "" {
				continue
			}
			lines = append(lines, modelID+"="+model)
		}
		return strings.Join(lines, "\n")
	},
	"targetRows": func(total int) []int {
		if total <= 0 {
			total = 1
		}
		rows := make([]int, 0, total)
		for i := 1; i <= total; i++ {
			rows = append(rows, i)
		}
		return rows
	},
	"routeTargetFormItem": func(items []upstream.RouteTarget, pos int) upstream.RouteTarget {
		if pos <= 0 {
			pos = 1
		}
		if pos <= len(items) {
			item := items[pos-1]
			if item.Weight <= 0 {
				item.Weight = 100
			}
			if item.Priority <= 0 {
				item.Priority = pos
			}
			return item
		}
		return upstream.RouteTarget{
			Weight:   100,
			Priority: pos,
			Enabled:  true,
		}
	},
	"statusTone": func(raw string) string {
		v := strings.ToLower(strings.TrimSpace(raw))
		switch {
		case v == "":
			return "tone-neutral"
		case strings.Contains(v, "disable"), strings.Contains(v, "error"), strings.Contains(v, "fail"), strings.Contains(v, "forbidden"), strings.Contains(v, "deny"):
			return "tone-danger"
		case v == "admin", v == "active", v == "enabled", v == "success", v == "healthy", v == "published", v == "paid":
			return "tone-good"
		case v == "member", v == "draft", v == "pending", v == "fallback", v == "weighted":
			return "tone-warn"
		default:
			return "tone-neutral"
		}
	},
	"boolLabel": func(v bool) string {
		if v {
			return "true"
		}
		return "false"
	},
	"boolText": func(lang string, v bool) string {
		if isChineseLang(lang) {
			if v {
				return "是"
			}
			return "否"
		}
		if v {
			return "true"
		}
		return "false"
	},
	"fmtTimeInput": func(ts time.Time) string {
		if ts.IsZero() {
			return ""
		}
		return ts.In(time.Local).Format("2006-01-02T15:04")
	},
	"hasTime": func(ts time.Time) bool {
		return !ts.IsZero()
	},
	"limitText": func(limit int, unlimited bool) string {
		if unlimited || limit <= 0 {
			return "∞"
		}
		return strconv.Itoa(limit)
	},
	"countActiveUsers": func(users []user.TenantUser) int {
		n := 0
		for _, item := range users {
			if strings.EqualFold(item.User.Status, "active") {
				n++
			}
		}
		return n
	},
	"countAdminUsers": func(users []user.TenantUser) int {
		n := 0
		for _, item := range users {
			if strings.EqualFold(item.Role, "admin") {
				n++
			}
		}
		return n
	},
	"countPublishedPrompts": func(items []promptapi.Definition) int {
		n := 0
		for _, item := range items {
			if strings.EqualFold(item.Status, "published") {
				n++
			}
		}
		return n
	},
	"countUnpublishedPrompts": func(items []promptapi.Definition) int {
		n := 0
		for _, item := range items {
			if !strings.EqualFold(item.Status, "published") {
				n++
			}
		}
		return n
	},
	"countHealthyUpstreams": func(items []upstream.Upstream) int {
		n := 0
		for _, item := range items {
			if strings.EqualFold(item.LastHealthStatus, "healthy") {
				n++
			}
		}
		return n
	},
	"countEnabledRoutes": func(items []upstream.ModelRoute) int {
		n := 0
		for _, item := range items {
			if item.Enabled {
				n++
			}
		}
		return n
	},
	"fmtAmount": func(v float64) string {
		return strconv.FormatFloat(v, 'f', 2, 64)
	},
	"fmtLatency": func(v int64) string {
		if v <= 0 {
			return "-"
		}
		return fmt.Sprintf("%dms", v)
	},
	"fmtTime": func(v time.Time) string {
		if v.IsZero() {
			return "-"
		}
		return v.Format("2006-01-02 15:04:05")
	},
	"csv": func(items []string) string {
		return strings.Join(items, ",")
	},
	"toJSON": func(v any) string {
		raw, err := json.Marshal(v)
		if err != nil {
			return "{}"
		}
		return string(raw)
	},
	"short": func(raw string, max int) string {
		s := strings.TrimSpace(raw)
		if max <= 0 || len(s) <= max {
			return s
		}
		return s[:max] + "..."
	},
}

const adminConsoleCommonCSS = `
:root {
  --app-sidebar-width: 286px;
  --app-bg: #f4f6fa;
  --app-panel: #ffffff;
  --app-border: #dbe2ef;
  --app-ink: #0f172a;
  --app-muted: #6b7280;
  --app-brand: #2563eb;
  --app-brand-soft: #dbeafe;
}
* { box-sizing: border-box; }
body {
  margin: 0;
  color: var(--app-ink);
  font-family: "IBM Plex Sans", "Noto Sans SC", "PingFang SC", "Segoe UI", sans-serif;
  background:
    radial-gradient(1200px 600px at -10% -10%, #dbeafe 0%, transparent 68%),
    radial-gradient(900px 400px at 120% 110%, #e0e7ff 0%, transparent 66%),
    var(--app-bg);
}
.app-shell {
  min-height: 100vh;
  display: flex;
}
.app-sidebar {
  width: var(--app-sidebar-width);
  border-right: 1px solid #1f2a44;
  background: linear-gradient(180deg, #111827 0%, #0b1220 100%);
  color: #cbd5e1;
  padding: 18px 14px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.sidebar-brand {
  padding: 6px 8px;
}
.eyebrow {
  margin: 0 0 4px;
  letter-spacing: .11em;
  font-weight: 600;
  font-size: 11px;
  opacity: .76;
}
.sidebar-title {
  margin: 0;
  font-size: 20px;
  font-weight: 700;
  color: #f8fafc;
}
.sidebar-sub {
  margin: 6px 0 0;
  font-size: 12px;
  opacity: .8;
}
.sidebar-nav {
  display: grid;
  gap: 8px;
}
.sidebar-nav .nav-link {
  border-radius: 10px;
  color: #cbd5e1;
  border: 1px solid transparent;
  font-size: 14px;
  padding: 9px 12px;
}
.sidebar-nav .nav-link:hover {
  color: #fff;
  background: rgba(148, 163, 184, .12);
  border-color: rgba(148, 163, 184, .24);
}
.sidebar-nav .nav-link.active {
  color: #eff6ff;
  background: rgba(37, 99, 235, .35);
  border-color: rgba(59, 130, 246, .6);
}
.sidebar-footer {
  margin-top: auto;
  display: grid;
  gap: 10px;
}
.meta-pills {
  display: grid;
  gap: 6px;
}
.meta-pill {
  border: 1px solid rgba(148, 163, 184, .26);
  border-radius: 999px;
  padding: 5px 10px;
  font-size: 12px;
  color: #e2e8f0;
  text-decoration: none;
  width: fit-content;
  background: rgba(15, 23, 42, .45);
}
.meta-pill:hover {
  color: #fff;
  border-color: rgba(147, 197, 253, .7);
}
.app-main {
  flex: 1;
  min-width: 0;
  padding: 16px;
}
.page-shell {
  width: min(1450px, 100%);
  margin: 0 auto;
  display: grid;
  gap: 12px;
}
.panel {
  border: 1px solid var(--app-border);
  background: var(--app-panel);
  border-radius: 14px;
  box-shadow: 0 10px 24px rgba(15, 23, 42, .06);
}
.topbar {
  padding: 16px 18px;
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 14px;
}
.page-title {
  margin: 0;
  font-size: 28px;
  letter-spacing: -.01em;
}
.page-subtitle {
  margin: 6px 0 0;
  color: var(--app-muted);
  font-size: 14px;
}
.topbar-meta {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: flex-end;
}
.topbar-meta .meta-pill {
  color: #334155;
  border-color: var(--app-border);
  background: #f8fafc;
}
.notice {
  margin: 0;
  border-radius: 10px;
  padding: 10px 12px;
  border: 1px solid;
  font-size: 14px;
}
.notice.success {
  color: #166534;
  background: #ecfdf3;
  border-color: #bbf7d0;
}
.notice.error {
  color: #991b1b;
  background: #fef2f2;
  border-color: #fecaca;
}
.d-flex { display: flex; }
.gap-2 { gap: 8px; }
.w-100 { width: 100%; }
.opc-field {
  width: 100%;
  border: 1px solid var(--app-border);
  border-radius: 10px;
  background: #fff;
  color: var(--app-ink);
  font-size: 13px;
  padding: 8px 10px;
  line-height: 1.35;
}
textarea.opc-field {
  min-height: 88px;
  resize: vertical;
}
.opc-field:focus {
  outline: none;
  border-color: #93c5fd;
  box-shadow: 0 0 0 3px rgba(147, 197, 253, .28);
}
.opc-field:disabled {
  background: #f8fafc;
  color: #64748b;
}
.opc-table {
  width: 100%;
  border-collapse: separate;
  border-spacing: 0;
}
.opc-table th, .opc-table td {
  border-bottom: 1px solid #e2e8f0;
  padding: 9px 10px;
  vertical-align: middle;
  font-size: 13px;
}
.opc-table tbody tr:hover {
  background: #f8fbff;
}
.opc-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  border-radius: 10px;
  border: 1px solid transparent;
  font-size: 13px;
  line-height: 1.2;
  font-weight: 600;
  text-decoration: none;
  cursor: pointer;
  padding: 8px 12px;
  transition: .14s ease-in-out;
}
.opc-btn:disabled {
  opacity: .65;
  cursor: not-allowed;
}
.opc-btn-primary {
  background: #2563eb;
  color: #fff;
  border-color: #1d4ed8;
}
.opc-btn-primary:hover {
  background: #1d4ed8;
  border-color: #1e40af;
}
.opc-btn-secondary {
  background: #fff;
  color: #334155;
  border-color: #cbd5e1;
}
.opc-btn-secondary:hover {
  background: #f8fafc;
  border-color: #94a3b8;
}
.opc-btn-ghost {
  background: #f8fafc;
  color: #334155;
  border-color: #dbe2ef;
}
.opc-btn-ghost:hover {
  background: #f1f5f9;
  border-color: #cbd5e1;
}
.opc-btn-mini {
  font-size: 12px;
  padding: 6px 10px;
}
.form-check {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.form-check-input {
  appearance: none;
  width: 36px;
  height: 20px;
  border-radius: 999px;
  border: 1px solid #94a3b8;
  background: #cbd5e1;
  position: relative;
  transition: .15s ease-in-out;
  cursor: pointer;
  margin: 0;
}
.form-check-input::before {
  content: "";
  position: absolute;
  top: 1px;
  left: 1px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: #fff;
  box-shadow: 0 1px 2px rgba(15, 23, 42, .25);
  transition: .15s ease-in-out;
}
.form-check-input:checked {
  background: #2563eb;
  border-color: #1d4ed8;
}
.form-check-input:checked::before {
  transform: translateX(16px);
}
.form-check-label {
  font-size: 13px;
  color: #334155;
}
.kpi-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}
.kpi {
  padding: 14px;
  background: linear-gradient(180deg, #ffffff 0%, #f8fafc 100%);
}
.kpi-label {
  margin: 0;
  font-size: 12px;
  color: var(--app-muted);
}
.kpi-value {
  margin: 6px 0 2px;
  font-size: 29px;
  font-weight: 700;
}
.kpi-note {
  margin: 0;
  font-size: 12px;
  color: var(--app-muted);
}
.grid {
  display: grid;
  grid-template-columns: repeat(12, minmax(0, 1fr));
  gap: 12px;
}
.span-12 { grid-column: span 12; }
.span-8 { grid-column: span 8; }
.span-7 { grid-column: span 7; }
.span-6 { grid-column: span 6; }
.span-5 { grid-column: span 5; }
.span-4 { grid-column: span 4; }
.section {
  padding: 14px;
  display: grid;
  gap: 10px;
}
.section-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 10px;
}
.section-title {
  margin: 0;
  font-size: 18px;
}
.section-desc {
  margin: 2px 0 0;
  color: var(--app-muted);
  font-size: 13px;
}
.search {
  width: min(360px, 100%);
}
.table-tools {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: nowrap;
}
.table-tools > form.table-tools {
  flex: 1;
  min-width: 0;
}
.table-tools .search {
  width: auto;
  max-width: none;
  min-width: 260px;
  flex: 1;
}
.table-tools .page-size {
  width: 120px;
  flex: 0 0 auto;
}
.table-wrap {
  border: 1px solid var(--app-border);
  border-radius: 12px;
  overflow: auto;
  max-height: 420px;
}
.table-check {
  width: 34px;
  text-align: center;
}
.table-check input {
  width: auto;
  margin: 0;
}
table th {
  position: sticky;
  top: 0;
  z-index: 1;
  background: #f8fafc !important;
}
.chip {
  display: inline-flex;
  border-radius: 999px;
  border: 1px solid;
  padding: 2px 8px;
  font-size: 12px;
  font-weight: 600;
}
.tone-good {
  color: #166534;
  background: #ecfdf3;
  border-color: #bbf7d0;
}
.tone-warn {
  color: #92400e;
  background: #fffbeb;
  border-color: #fde68a;
}
.tone-danger {
  color: #991b1b;
  background: #fef2f2;
  border-color: #fecaca;
}
.tone-neutral {
  color: #475569;
  background: #f8fafc;
  border-color: #e2e8f0;
}
form.stack {
  display: grid;
  gap: 8px;
}
.stack-block {
  border: 1px solid var(--app-border);
  border-radius: 12px;
  padding: 12px;
  background: #fbfdff;
}
.compact-card {
  gap: 6px;
  padding: 10px;
}
.user-update-layout {
  grid-template-columns: minmax(280px, 340px) minmax(0, 1fr);
  align-items: start;
}
.user-role-status-stack {
  display: grid;
  gap: 8px;
}
.switch-field {
  border: 1px solid var(--app-border);
  border-radius: 10px;
  padding: 10px 12px;
  background: #f8fafc;
  display: grid;
  gap: 6px;
  align-content: center;
}
.switch-field-title {
  margin: 0;
  font-size: 12px;
  color: var(--app-muted);
}
.stack-title {
  margin: 0 0 8px;
  font-size: 13px;
  color: #334155;
}
.f2 {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}
.f3 {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
}
.target-row {
  display: grid;
  grid-template-columns: 1.2fr 1fr .7fr .7fr;
  gap: 8px;
}
.target-row-box {
  border: 1px dashed #cbd5e1;
  border-radius: 10px;
  padding: 10px;
  background: #f8fafc;
  display: grid;
  gap: 8px;
}
.target-row-box.is-hidden {
  display: none;
}
.target-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  flex-wrap: wrap;
}
.prompt-create-guide {
  border: 1px solid #dbeafe;
  background: #eff6ff;
}
.prompt-endpoint {
  font-size: 12px;
  color: #1e3a8a;
  margin: 0;
}
.prompt-preview-panel {
  border: 1px solid #dbeafe;
  background: #f8fbff;
}
.prompt-preview-result {
  margin: 0;
  padding: 10px 12px;
  border-radius: 10px;
  border: 1px solid #d1e3ff;
  background: #fff;
  color: #1e293b;
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-word;
  min-height: 88px;
}
.row-details {
  border: 1px solid #dbe2ef;
  border-radius: 10px;
  padding: 6px 8px;
  background: #fff;
}
.row-details summary {
  cursor: pointer;
  color: #1d4ed8;
  font-size: 12px;
  font-weight: 600;
}
.row-details[open] {
  background: #f8fafc;
}
.row-details-body {
  margin-top: 8px;
  display: grid;
  gap: 6px;
}
.row-details-line {
  font-size: 12px;
  color: #334155;
}
.row-details-line strong {
  color: #0f172a;
}
label {
  display: grid;
  gap: 4px;
  font-size: 12px;
  color: #64748b;
}
.check {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}
.check input {
  width: auto;
  margin: 0;
}
.hint {
  margin: 0;
  color: #64748b;
  font-size: 12px;
}
.split {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}
.inline-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.pager {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-top: 10px;
}
.pager-info {
  color: var(--app-muted);
  font-size: 12px;
}
.page-jump {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.jump-input {
  width: 88px;
}
.opc-modal {
  position: fixed;
  inset: 0;
  z-index: 1200;
  display: none;
  align-items: center;
  justify-content: center;
  padding: 24px 12px;
}
.opc-modal.open {
  display: flex;
}
.opc-modal-backdrop {
  position: absolute;
  inset: 0;
  background: rgba(15, 23, 42, .45);
}
.opc-modal-card {
  position: relative;
  width: min(920px, 100%);
  max-height: calc(100vh - 48px);
  overflow: auto;
  border: 1px solid var(--app-border);
  border-radius: 14px;
  background: #fff;
  box-shadow: 0 24px 48px rgba(2, 6, 23, .28);
  padding: 14px;
}
.modal-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 8px;
}
.modal-title {
  margin: 0;
  font-size: 18px;
}
.mono {
  font-family: "JetBrains Mono", "SFMono-Regular", "Menlo", monospace;
  font-size: 12px;
}
@media (max-width: 1200px) {
  .span-8, .span-7, .span-6, .span-5, .span-4 { grid-column: span 12; }
  .kpi-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (max-width: 992px) {
  .app-shell { flex-direction: column; }
  .app-sidebar {
    width: 100%;
    border-right: none;
    border-bottom: 1px solid #1f2a44;
  }
  .sidebar-nav {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
  .topbar {
    flex-direction: column;
    align-items: stretch;
  }
  .topbar-meta {
    justify-content: flex-start;
  }
}
@media (max-width: 700px) {
  .f3, .f2, .split, .target-row, .user-update-layout { grid-template-columns: 1fr; }
  .kpi-grid { grid-template-columns: 1fr; }
  .section-head { flex-direction: column; align-items: stretch; }
  .table-tools { flex-wrap: wrap; }
  .table-tools .search { min-width: 0; width: 100%; }
  .table-tools .page-size { width: 100%; }
  .table-wrap { max-height: none; }
}
`

const adminConsoleCommonJS = `
(() => {
  const zh = (document.documentElement.lang || '').toLowerCase().startsWith('zh');

  const enhanceBootstrapLook = () => {
    document.querySelectorAll('input:not([type="checkbox"]):not([type="hidden"]), textarea, select').forEach((el) => {
      el.classList.add('opc-field');
    });
    document.querySelectorAll('.search').forEach((el) => {
      el.classList.add('opc-field');
    });
    document.querySelectorAll('table').forEach((el) => {
      el.classList.add('opc-table');
    });
    document.querySelectorAll('.btn').forEach((btn) => {
      btn.classList.add('opc-btn');
      if (btn.classList.contains('ghost')) {
        btn.classList.add('opc-btn-ghost');
      } else if (btn.classList.contains('secondary')) {
        btn.classList.add('opc-btn-secondary');
      } else {
        btn.classList.add('opc-btn-primary');
      }
      if (btn.classList.contains('mini')) {
        btn.classList.add('opc-btn-mini');
      }
    });
    document.querySelectorAll('.check input[type="checkbox"], .table-check input[type="checkbox"]').forEach((el) => {
      el.classList.add('form-check-input');
    });
  };

  const ensurePositiveNumbers = (form) => {
    const fields = Array.from(form.querySelectorAll('[data-positive-number]'));
    for (const field of fields) {
      const raw = (field.value || '').trim();
      if (raw === '') continue;
      const num = Number(raw);
      if (!Number.isFinite(num) || num <= 0) {
        const msg = field.getAttribute('data-positive-number') || (zh ? '请输入大于 0 的数值' : 'Please input a value greater than 0');
        alert(msg);
        field.focus();
        return false;
      }
    }
    return true;
  };

  const ensureRouteTargets = (form) => {
    if (form.getAttribute('data-require-targets') !== '1') return true;
    const legacy = form.querySelector('textarea[name="targets"]');
    if (legacy && (legacy.value || '').trim() !== '') return true;
    const rows = Array.from(form.querySelectorAll('[data-route-target-row]'));
    if (rows.length > 0) {
      for (const row of rows) {
        if (row.classList.contains('is-hidden')) continue;
        const field = row.querySelector('[name^="target_upstream_id_"]');
        if (field && (field.value || '').trim() !== '') return true;
      }
    } else {
      for (let i = 1; i <= 8; i++) {
        const field = form.querySelector('[name="target_upstream_id_' + i + '"]');
        if (field && (field.value || '').trim() !== '') return true;
      }
    }
    const msg = form.getAttribute('data-route-targets-required-text') || (zh ? '请至少填写一个路由目标。' : 'Please provide at least one route target.');
    alert(msg);
    return false;
  };

  const bindFormEnhancements = () => {
    document.querySelectorAll('form').forEach((form) => {
      form.addEventListener('submit', (event) => {
        const confirmText = form.getAttribute('data-confirm');
        if (confirmText && !window.confirm(confirmText)) {
          event.preventDefault();
          return;
        }
        if (!ensurePositiveNumbers(form)) {
          event.preventDefault();
          return;
        }
        if (!ensureRouteTargets(form)) {
          event.preventDefault();
          return;
        }
        if (form.getAttribute('data-submit-lock') !== '1') return;
        if (form.dataset.submitting === '1') {
          event.preventDefault();
          return;
        }
        form.dataset.submitting = '1';
        const loadingText = form.getAttribute('data-loading-text') || (zh ? '提交中...' : 'Submitting...');
        form.querySelectorAll('button[type="submit"]').forEach((btn) => {
          if (!btn.dataset.originalText) btn.dataset.originalText = btn.textContent || '';
          btn.disabled = true;
          btn.textContent = loadingText;
        });
      });
    });
  };

  const bindTableFilter = () => {
    document.querySelectorAll('[data-filter-input]').forEach((input) => {
      const tableId = input.getAttribute('data-filter-input');
      if (!tableId) return;
      const table = document.getElementById(tableId);
      if (!table) return;
      const rows = Array.from(table.querySelectorAll('tbody tr'));
      input.addEventListener('input', () => {
        const q = (input.value || '').toLowerCase().trim();
        rows.forEach((row) => {
          const hit = q === '' || row.innerText.toLowerCase().includes(q);
          row.style.display = hit ? '' : 'none';
        });
      });
    });
  };

  const bindUserSelection = () => {
    const selectAll = document.querySelector('[data-users-select-all]');
    const rowChecks = () => Array.from(document.querySelectorAll('[data-user-select]'));
    const updateSelectAllState = () => {
      if (!selectAll) return;
      const checks = rowChecks();
      const checked = checks.filter((item) => item.checked).length;
      if (checked === 0) {
        selectAll.checked = false;
        selectAll.indeterminate = false;
        return;
      }
      if (checked === checks.length) {
        selectAll.checked = true;
        selectAll.indeterminate = false;
        return;
      }
      selectAll.checked = false;
      selectAll.indeterminate = true;
    };

    if (selectAll) {
      selectAll.addEventListener('change', () => {
        rowChecks().forEach((item) => {
          item.checked = selectAll.checked;
        });
        updateSelectAllState();
      });
    }
    rowChecks().forEach((item) => item.addEventListener('change', updateSelectAllState));
    updateSelectAllState();

    document.querySelectorAll('[data-fill-selected-target]').forEach((btn) => {
      btn.addEventListener('click', () => {
        const targetID = btn.getAttribute('data-fill-selected-target');
        if (!targetID) return;
        const target = document.getElementById(targetID);
        if (!target) return;
        const ids = rowChecks().filter((item) => item.checked).map((item) => item.value).filter((item) => item);
        if (ids.length === 0) {
          const msg = btn.getAttribute('data-no-selection-text') || (zh ? '请先选择至少一个用户' : 'Please select at least one user');
          alert(msg);
          return;
        }
        target.value = ids.join(',');
        target.focus();
      });
    });
  };

  const bindRouteTargetRows = () => {
    const parseModelMap = (form) => {
      const raw = form.getAttribute('data-upstream-model-map') || '{}';
      try {
        const parsed = JSON.parse(raw);
        if (parsed && typeof parsed === 'object') return parsed;
      } catch (_) {}
      return {};
    };

    const clearRow = (row) => {
      row.querySelectorAll('select, input').forEach((field) => {
        if (!(field instanceof HTMLInputElement || field instanceof HTMLSelectElement)) return;
        const name = field.getAttribute('name') || '';
        if (name.startsWith('target_weight_')) {
          field.value = field.getAttribute('data-default-value') || '100';
          return;
        }
        if (name.startsWith('target_priority_')) {
          field.value = field.getAttribute('data-default-value') || '';
          return;
        }
        if (field instanceof HTMLSelectElement) {
          field.selectedIndex = 0;
          return;
        }
        field.value = '';
      });
    };

    document.querySelectorAll('[data-route-targets-form]').forEach((form) => {
      const modelMap = parseModelMap(form);
      const rows = () => Array.from(form.querySelectorAll('[data-route-target-row]'));
      const addBtn = form.querySelector('[data-route-target-add]');
      const fillModelSelect = (row, forceCurrentValue) => {
        const upstreamSelect = row.querySelector('select[name^="target_upstream_id_"]');
        const modelSelect = row.querySelector('select[name^="target_model_"]');
        if (!(upstreamSelect instanceof HTMLSelectElement) || !(modelSelect instanceof HTMLSelectElement)) return;

        const upstreamID = (upstreamSelect.value || '').trim();
        const emptyLabel = modelSelect.getAttribute('data-empty-label') || (zh ? '留空则使用路由别名模型' : 'Use route alias model when empty');
        const current = typeof forceCurrentValue === 'string' ? forceCurrentValue : (modelSelect.value || '');
        const models = Array.isArray(modelMap[upstreamID]) ? modelMap[upstreamID] : [];

        const frag = document.createDocumentFragment();
        const emptyOpt = document.createElement('option');
        emptyOpt.value = '';
        emptyOpt.textContent = emptyLabel;
        frag.appendChild(emptyOpt);

        models.forEach((model) => {
          const v = String(model || '').trim();
          if (!v) return;
          const opt = document.createElement('option');
          opt.value = v;
          opt.textContent = v;
          frag.appendChild(opt);
        });

        if (current && !models.includes(current)) {
          const legacyOpt = document.createElement('option');
          legacyOpt.value = current;
          legacyOpt.textContent = current + (zh ? '（当前值）' : ' (current)');
          frag.appendChild(legacyOpt);
        }

        modelSelect.innerHTML = '';
        modelSelect.appendChild(frag);
        modelSelect.value = current;
        modelSelect.dataset.currentModel = modelSelect.value || '';
      };

      const sync = () => {
        const visible = rows().filter((row) => !row.classList.contains('is-hidden'));
        if (addBtn) {
          addBtn.disabled = visible.length >= rows().length;
        }
      };

      rows().forEach((row) => {
        const upstreamSelect = row.querySelector('select[name^="target_upstream_id_"]');
        const modelSelect = row.querySelector('select[name^="target_model_"]');
        const initialModel = modelSelect ? (modelSelect.getAttribute('data-current-model') || modelSelect.value || '') : '';
        fillModelSelect(row, initialModel);
        if (upstreamSelect instanceof HTMLSelectElement) {
          upstreamSelect.addEventListener('change', () => fillModelSelect(row, ''));
        }

        row.querySelectorAll('[data-route-target-remove]').forEach((btn) => {
          btn.addEventListener('click', () => {
            const visible = rows().filter((item) => !item.classList.contains('is-hidden'));
            if (visible.length <= 1) {
              clearRow(row);
              sync();
              return;
            }
            clearRow(row);
            row.classList.add('is-hidden');
            sync();
          });
        });
      });

      if (addBtn) {
        addBtn.addEventListener('click', () => {
          const next = rows().find((row) => row.classList.contains('is-hidden'));
          if (!next) return;
          next.classList.remove('is-hidden');
          const priority = next.querySelector('input[name^="target_priority_"]');
          if (priority && (priority.value || '').trim() === '') {
            const visible = rows().filter((row) => !row.classList.contains('is-hidden')).length;
            priority.value = String(visible);
          }
          fillModelSelect(next, '');
          sync();
        });
      }
      sync();
    });
  };

  const bindPromptEndpointPreview = () => {
    document.querySelectorAll('[data-prompt-slug-input]').forEach((input) => {
      const targetID = input.getAttribute('data-endpoint-target');
      if (!targetID) return;
      const endpointNode = document.getElementById(targetID);
      if (!endpointNode) return;
      const update = () => {
        const raw = (input.value || '').trim();
        const slug = raw === '' ? '{slug}' : raw;
        endpointNode.textContent = '/v1/prompt-apis/' + slug;
      };
      input.addEventListener('input', update);
      update();
    });
  };

  const bindPromptPreview = () => {
    const toJSONString = (obj) => {
      try {
        return JSON.stringify(obj, null, 2);
      } catch (_) {
        return String(obj);
      }
    };
    const parseError = async (resp) => {
      let text = '';
      try {
        const data = await resp.json();
        if (data && data.error && data.error.message) return data.error.message;
        if (data && data.error) return typeof data.error === 'string' ? data.error : toJSONString(data.error);
        return toJSONString(data);
      } catch (_) {
        try { text = await resp.text(); } catch (_) {}
      }
      return text || (zh ? '预览失败' : 'Preview failed');
    };

    document.querySelectorAll('[data-prompt-preview-form]').forEach((form) => {
      const btn = form.querySelector('[data-prompt-preview-btn]');
      const result = form.querySelector('[data-prompt-preview-result]');
      const endpoint = form.getAttribute('data-prompt-preview-url') || '';
      if (!btn || !result || !endpoint) return;
      btn.addEventListener('click', async () => {
        if (!(result instanceof HTMLElement)) return;
        if (btn instanceof HTMLButtonElement) btn.disabled = true;
        const loading = btn.getAttribute('data-loading-text') || (zh ? '试运行中...' : 'Running preview...');
        const original = btn.textContent || '';
        btn.textContent = loading;
        result.textContent = zh ? '正在调用上游模型，请稍候...' : 'Calling upstream model...';
        try {
          const fd = new FormData(form);
          const body = new URLSearchParams();
          for (const [k, v] of fd.entries()) {
            body.append(k, String(v));
          }
          const resp = await fetch(endpoint, {
            method: 'POST',
            headers: { 'Accept': 'application/json' },
            body,
          });
          if (!resp.ok) {
            result.textContent = await parseError(resp);
            return;
          }
          const data = await resp.json();
          const preview = data.preview || {};
          const lines = [
            (zh ? '模型: ' : 'Model: ') + (preview.provider_model || ''),
            (zh ? '渲染后的用户提示词:' : 'Rendered user prompt:'),
            String(preview.rendered_user || ''),
            '',
            (zh ? '模型返回:' : 'Assistant reply:'),
            String(preview.assistant_reply || ''),
          ];
          result.textContent = lines.join('\n');
        } catch (err) {
          result.textContent = (err && err.message) ? err.message : (zh ? '预览失败' : 'Preview failed');
        } finally {
          if (btn instanceof HTMLButtonElement) btn.disabled = false;
          btn.textContent = original;
        }
      });
    });
  };

  const bindUpstreamDiagnose = () => {
    const formatHealthBlock = (health) => {
      if (!health || typeof health !== 'object') {
        return zh ? '连通性诊断结果缺失' : 'Reachability result is missing';
      }
      const raw = String(health.raw_response || '');
      const rawLimit = 3000;
      const rawText = raw.length > rawLimit ? (raw.slice(0, rawLimit) + '\n... (truncated)') : raw;
      const lines = [
        (zh ? '健康状态: ' : 'Health: ') + (health.healthy ? (zh ? '可访问' : 'reachable') : (zh ? '不可访问' : 'unreachable')),
        (zh ? '上游: ' : 'Upstream: ') + (health.upstream_name || health.upstream_id || ''),
        (zh ? '类型: ' : 'Type: ') + (health.provider_type || ''),
        (zh ? 'HTTP 状态: ' : 'HTTP status: ') + String(health.http_status || ''),
        (zh ? '端点: ' : 'Endpoint: ') + String(health.endpoint || ''),
        (zh ? '延迟: ' : 'Latency: ') + String(health.latency_ms || 0) + 'ms',
        (zh ? '检查时间: ' : 'Checked at: ') + String(health.checked_at || ''),
      ];
      if (health.error) {
        lines.push((zh ? '错误: ' : 'Error: ') + String(health.error));
      }
      if (rawText) {
        lines.push('');
        lines.push(zh ? '连通性诊断原始返回:' : 'Reachability raw response:');
        lines.push(rawText);
      }
      return lines.join('\n');
    };

    const formatProbeBlock = (probe, probeError) => {
      const raw = String(probe.raw_response || '');
      const rawLimit = 6000;
      const rawText = raw.length > rawLimit ? (raw.slice(0, rawLimit) + '\n... (truncated)') : raw;
      const lines = [
        (zh ? '上游: ' : 'Upstream: ') + (probe.upstream_name || probe.upstream_id || ''),
        (zh ? '类型: ' : 'Type: ') + (probe.provider_type || ''),
        (zh ? 'HTTP 状态: ' : 'HTTP status: ') + String(probe.http_status || ''),
        (zh ? '端点: ' : 'Endpoint: ') + String(probe.endpoint || ''),
        (zh ? '请求模型: ' : 'Requested model: ') + (probe.requested_model || probe.model || ''),
        (zh ? '返回模型: ' : 'Provider model: ') + (probe.provider_model || ''),
        (zh ? '延迟: ' : 'Latency: ') + String(probe.latency_ms || 0) + 'ms',
        (zh ? 'Tokens: ' : 'Tokens: ') + String(probe.total_tokens || 0) + ' (prompt=' + String(probe.prompt_tokens || 0) + ', completion=' + String(probe.completion_tokens || 0) + ')',
        '',
        (zh ? '模型返回:' : 'Assistant reply:'),
        String(probe.assistant_reply || ''),
      ];
      if (probeError) {
        lines.push('');
        lines.push((zh ? '响应测试错误: ' : 'Probe error: ') + String(probeError));
      }
      if (rawText) {
        lines.push('');
        lines.push(zh ? '原始返回:' : 'Raw response:');
        lines.push(rawText);
      }
      return lines.join('\n');
    };

    const parseError = async (resp) => {
      try {
        const data = await resp.json();
        if (data && (data.health || data.probe || data.probe_error)) {
          const blocks = [];
          if (data.health) {
            blocks.push((zh ? '【连通性诊断】' : '[Reachability Diagnose]'));
            blocks.push(formatHealthBlock(data.health));
          }
          if (data.probe || data.probe_error) {
            blocks.push((zh ? '【响应测试】' : '[Response Probe]'));
            blocks.push(formatProbeBlock(data.probe || {}, data.probe_error));
          }
          return blocks.join('\n\n');
        }
        if (data && data.error) return typeof data.error === 'string' ? data.error : JSON.stringify(data.error);
      } catch (_) {}
      try {
        const text = await resp.text();
        if (text) return text;
      } catch (_) {}
      return zh ? '诊断失败' : 'Diagnose failed';
    };

    document.querySelectorAll('[data-upstream-diagnose-form]').forEach((form) => {
      const btn = form.querySelector('[data-upstream-diagnose-btn]');
      const result = form.querySelector('[data-upstream-diagnose-result]');
      const endpoint = form.getAttribute('data-upstream-diagnose-url') || '';
      if (!btn || !result || !endpoint) return;

      btn.addEventListener('click', async () => {
        if (!(result instanceof HTMLElement) || !(btn instanceof HTMLButtonElement)) return;
        btn.disabled = true;
        const original = btn.textContent || '';
        btn.textContent = btn.getAttribute('data-loading-text') || (zh ? '诊断中...' : 'Diagnosing...');
        result.textContent = zh ? '正在执行连通性诊断与响应测试，请稍候...' : 'Running reachability diagnose and response probe...';
        try {
          const fd = new FormData(form);
          const body = new URLSearchParams();
          for (const [k, v] of fd.entries()) {
            body.append(k, String(v));
          }
          const resp = await fetch(endpoint, {
            method: 'POST',
            headers: { 'Accept': 'application/json' },
            body,
          });
          if (!resp.ok) {
            result.textContent = await parseError(resp);
            return;
          }
          const payload = await resp.json();
          const blocks = [];
          if (payload.health) {
            blocks.push(zh ? '【连通性诊断】' : '[Reachability Diagnose]');
            blocks.push(formatHealthBlock(payload.health));
          }
          if (payload.probe || payload.probe_error) {
            blocks.push(zh ? '【响应测试】' : '[Response Probe]');
            blocks.push(formatProbeBlock(payload.probe || {}, payload.probe_error));
          }
          result.textContent = blocks.join('\n\n');
        } catch (err) {
          result.textContent = (err && err.message) ? err.message : (zh ? '诊断失败' : 'Diagnose failed');
        } finally {
          btn.disabled = false;
          btn.textContent = original;
        }
      });
    });
  };

  const bindModals = () => {
    const open = (id) => {
      if (!id) return;
      const modal = document.getElementById(id);
      if (!modal) return;
      modal.classList.add('open');
      document.body.style.overflow = 'hidden';
    };
    const close = (id) => {
      if (!id) return;
      const modal = document.getElementById(id);
      if (!modal) return;
      modal.classList.remove('open');
      if (!document.querySelector('.opc-modal.open')) {
        document.body.style.overflow = '';
      }
    };

    document.querySelectorAll('[data-open-modal]').forEach((btn) => {
      btn.addEventListener('click', () => open(btn.getAttribute('data-open-modal') || ''));
    });
    document.querySelectorAll('[data-close-modal]').forEach((btn) => {
      btn.addEventListener('click', () => close(btn.getAttribute('data-close-modal') || ''));
    });
    document.querySelectorAll('.opc-modal').forEach((modal) => {
      modal.addEventListener('click', (event) => {
        if (!(event.target instanceof Element)) return;
        if (!event.target.classList.contains('opc-modal-backdrop')) return;
        close(modal.id || '');
      });
    });
    document.addEventListener('keydown', (event) => {
      if (event.key !== 'Escape') return;
      const opened = document.querySelector('.opc-modal.open');
      if (!opened) return;
      close(opened.id || '');
    });
  };

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => {
      enhanceBootstrapLook();
      bindTableFilter();
      bindFormEnhancements();
      bindUserSelection();
      bindRouteTargetRows();
      bindPromptEndpointPreview();
      bindPromptPreview();
      bindUpstreamDiagnose();
      bindModals();
    });
  } else {
    enhanceBootstrapLook();
    bindTableFilter();
    bindFormEnhancements();
    bindUserSelection();
    bindRouteTargetRows();
    bindPromptEndpointPreview();
    bindPromptPreview();
    bindUpstreamDiagnose();
    bindModals();
  }
})();
`

var adminConsoleLayoutTemplate = template.Must(template.New("admin-console-layout").Funcs(adminConsoleTemplateFuncs).Parse(`<!doctype html>
<html lang="{{langCode .Lang}}">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>{{template "page_title" .}}</title>
  <style>` + adminConsoleCommonCSS + `</style>
</head>
<body>
  <div class="app-shell">
    <aside class="app-sidebar">
      <div class="sidebar-brand">
        <p class="eyebrow">{{uiOr .Lang "management" "OPCAPI MANAGEMENT"}}</p>
        <h1 class="sidebar-title">opcAPI Admin</h1>
        <p class="sidebar-sub">{{t .Lang "单体管理控制台视图" "Monolithic admin console view"}}</p>
      </div>
      <nav class="sidebar-nav" aria-label="Admin navigation">
        <a class="nav-link {{if eq .Active "users"}}active{{end}}" href="{{withLang "/admin/users" .Lang}}">{{ui .Lang "users"}}</a>
        <a class="nav-link {{if eq .Active "orders"}}active{{end}}" href="{{withLang "/admin/orders" .Lang}}">{{uiOr .Lang "order_management" "Order Management"}}</a>
        <a class="nav-link {{if eq .Active "prompts"}}active{{end}}" href="{{withLang "/admin/prompts" .Lang}}">{{ui .Lang "prompt_apis"}}</a>
        <a class="nav-link {{if eq .Active "upstreams"}}active{{end}}" href="{{withLang "/admin/upstreams" .Lang}}">{{t .Lang "上游配置" "Upstream Config"}}</a>
        <a class="nav-link {{if eq .Active "routes"}}active{{end}}" href="{{withLang "/admin/routes" .Lang}}">{{ui .Lang "model_routes"}}</a>
      </nav>
      <div class="sidebar-footer">
        <div class="meta-pills">
          <span class="meta-pill">{{.Me.User.Email}}</span>
          <div class="d-flex gap-2">
            <a class="meta-pill" href="{{withLang .Path "en"}}">EN</a>
            <a class="meta-pill" href="{{withLang .Path "zh-CN"}}">中文</a>
          </div>
        </div>
        <form method="post" action="{{withLang "/admin/logout" .Lang}}">
          <button class="btn ghost w-100" type="submit">{{ui .Lang "sign_out"}}</button>
        </form>
      </div>
    </aside>

    <main class="app-main">
      <div class="page-shell">
        <header class="topbar panel">
          <section>
            <p class="eyebrow">{{uiOr .Lang "management" "OPCAPI MANAGEMENT"}}</p>
            <h2 class="page-title">{{template "page_heading" .}}</h2>
            <p class="page-subtitle">{{template "page_subtitle" .}}</p>
          </section>
        </header>

        {{template "hero" .}}

        {{if .Message}}<p class="notice success">{{.Message}}</p>{{end}}
        {{if .Error}}<p class="notice error">{{.Error}}</p>{{end}}

        <section class="grid">
          {{template "content" .}}
        </section>
      </div>
    </main>
  </div>
  <script>` + adminConsoleCommonJS + `</script>
</body>
</html>`))

var adminUsersPageTemplate = template.Must(template.Must(adminConsoleLayoutTemplate.Clone()).Parse(`
{{define "page_title"}}opcAPI Admin - Users{{end}}
{{define "page_heading"}}{{ui .Lang "users"}}{{end}}
{{define "page_subtitle"}}{{t .Lang "统一管理用户角色、状态与会员信息，支持行内更新与分页浏览。" "Unified user role, status, and membership management with inline edit and pagination."}}{{end}}
{{define "hero"}}
<section class="kpi-grid">
  <article class="panel kpi">
    <p class="kpi-label">{{t .Lang "用户总数" "Total users"}}</p>
    <p class="kpi-value">{{.TotalUsers}}</p>
    <p class="kpi-note">{{t .Lang "分页列表已启用" "Pagination enabled"}}</p>
  </article>
  <article class="panel kpi">
    <p class="kpi-label">{{t .Lang "当前页用户" "Users in page"}}</p>
    <p class="kpi-value">{{len .Rows}}</p>
    <p class="kpi-note">{{t .Lang "本页可直接编辑" "Editable in this page"}}</p>
  </article>
  <article class="panel kpi">
    <p class="kpi-label">{{t .Lang "激活用户" "Active users"}}</p>
    <p class="kpi-value">{{.ActiveUsers}}</p>
    <p class="kpi-note">{{t .Lang "全量用户中状态 active" "Active status in all users"}}</p>
  </article>
  <article class="panel kpi">
    <p class="kpi-label">{{t .Lang "每页条数" "Page size"}}</p>
    <p class="kpi-value">{{.Pagination.PageSize}}</p>
    <p class="kpi-note">{{t .Lang "可通过 page_size 调整" "Adjustable via page_size"}}</p>
  </article>
</section>
{{end}}
{{define "content"}}
<article class="panel section span-12">
  <div class="section-head">
    <div>
      <h2 class="section-title">{{t .Lang "用户列表" "User Directory"}}</h2>
      <p class="section-desc">{{t .Lang "用户角色、状态与会员等级在同一张表管理。" "Role, status, and membership tier are managed in one table."}}</p>
    </div>
  </div>
  <div class="table-tools">
    <form class="table-tools" method="get" action="{{withLang "/admin/users" .Lang}}">
      <input class="search" name="q" value="{{.FilterQ}}" data-filter-input="users-table" placeholder="{{t .Lang "按 ID、邮箱、角色、状态、会员搜索" "Search by id, email, role, status, or membership"}}" />
      <select class="page-size" name="page_size">
        <option value="10" {{if eq .Pagination.PageSize 10}}selected{{end}}>10 / page</option>
        <option value="20" {{if eq .Pagination.PageSize 20}}selected{{end}}>20 / page</option>
        <option value="50" {{if eq .Pagination.PageSize 50}}selected{{end}}>50 / page</option>
        <option value="100" {{if eq .Pagination.PageSize 100}}selected{{end}}>100 / page</option>
      </select>
      <button class="btn secondary" type="submit">{{t .Lang "查询" "Search"}}</button>
    </form>
    <button class="btn secondary" type="button" data-open-modal="plan-config-modal">{{t .Lang "会员套餐配置" "Plan Configs"}}</button>
    <button class="btn" type="button" data-open-modal="create-user-modal">{{ui .Lang "create_user"}}</button>
  </div>
  <div class="table-wrap">
    <table id="users-table">
      <thead>
        <tr>
          <th>{{ui .Lang "id"}}</th>
          <th>{{ui .Lang "email"}}</th>
          <th>{{ui .Lang "role"}}</th>
          <th>{{ui .Lang "status"}}</th>
          <th>{{t .Lang "会员等级" "Membership Tier"}}</th>
          <th>{{t .Lang "有效期至" "Valid Until"}}</th>
          <th>{{t .Lang "5小时 Token 限额" "5h Token Limit"}}</th>
          <th>{{t .Lang "月 Token 限额" "Monthly Token Limit"}}</th>
          <th>{{ui .Lang "action"}}</th>
        </tr>
      </thead>
      <tbody>
        {{range $idx, $row := .Rows}}
        <tr>
          <td><span class="mono">{{$row.User.User.ID}}</span></td>
          <td>{{$row.User.User.Email}}</td>
          <td><span class="chip {{statusTone $row.User.Role}}">{{enum $.Lang "role" $row.User.Role}}</span></td>
          <td><span class="chip {{statusTone $row.User.User.Status}}">{{enum $.Lang "status" $row.User.User.Status}}</span></td>
          <td><span class="chip {{statusTone $row.Membership.PlanID}}">{{$row.Membership.PlanID}}</span></td>
          <td>{{if and (eq $row.Membership.Status "active") (hasTime $row.Membership.PeriodEnd)}}{{fmtTime $row.Membership.PeriodEnd}}{{else}}-{{end}}</td>
          <td>{{limitText $row.Policy.FiveHourTokenLimit $row.Policy.Unlimited}}</td>
          <td>{{limitText $row.Policy.MonthlyTokenLimit $row.Policy.Unlimited}}</td>
          <td>
            <div class="inline-actions">
              <button class="btn secondary mini" type="button" data-open-modal="user-update-modal-{{$idx}}">{{t $.Lang "更新" "Update"}}</button>
              {{if ne $row.User.User.ID $.Claims.UserID}}
              <form method="post" action="{{withLang "/admin/users/delete" $.Lang}}" data-submit-lock="1" data-loading-text="{{ui $.Lang "deleting"}}" data-confirm="{{t $.Lang "确认删除该用户？" "Delete this user?"}}">
                <input type="hidden" name="return_to" value="{{withLang "/admin/users" $.Lang}}" />
                <input type="hidden" name="user_id" value="{{$row.User.User.ID}}" />
                <button class="btn secondary mini" type="submit">{{ui $.Lang "delete"}}</button>
              </form>
              {{else}}
              <button class="btn secondary mini" type="button" disabled title="{{t $.Lang "管理员不能删除自己" "Admin cannot delete themselves"}}">{{ui $.Lang "delete"}}</button>
              {{end}}
            </div>
          </td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>
  <div class="pager">
    <p class="pager-info">{{t .Lang "第" "Page"}} {{.Pagination.Page}} / {{.Pagination.TotalPages}} , {{t .Lang "共" "Total"}} {{.Pagination.Total}} {{t .Lang "条" "rows"}}</p>
    <div class="inline-actions">
      {{if .Pagination.HasPrev}}<a class="btn secondary" href="{{.Pagination.PrevURL}}">{{t .Lang "上一页" "Previous"}}</a>{{end}}
      {{if .Pagination.HasNext}}<a class="btn secondary" href="{{.Pagination.NextURL}}">{{t .Lang "下一页" "Next"}}</a>{{end}}
      <form class="page-jump" method="get" action="{{withLang "/admin/users" .Lang}}">
        <input type="hidden" name="q" value="{{.FilterQ}}" />
        <input type="hidden" name="page_size" value="{{.Pagination.PageSize}}" />
        <input class="jump-input" type="number" min="1" max="{{.Pagination.TotalPages}}" name="page" value="{{.Pagination.Page}}" />
        <button class="btn secondary" type="submit">{{t .Lang "跳转" "Go"}}</button>
      </form>
    </div>
  </div>
</article>
<div id="plan-config-modal" class="opc-modal" data-modal>
  <div class="opc-modal-backdrop" data-close-modal="plan-config-modal"></div>
  <div class="opc-modal-card">
    <div class="modal-head">
      <h3 class="modal-title">{{t .Lang "会员套餐配置" "Membership Plan Configs"}}</h3>
      <button class="btn ghost mini" type="button" data-close-modal="plan-config-modal">{{t .Lang "关闭" "Close"}}</button>
    </div>
    <form class="stack" method="post" action="{{withLang "/admin/billing/plans" .Lang}}" data-submit-lock="1" data-loading-text="{{ui .Lang "saving"}}">
      <input type="hidden" name="return_to" value="{{withLang "/admin/users" .Lang}}" />
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>{{t .Lang "套餐" "Plan"}}</th>
              <th>{{t .Lang "价格(CNY/月)" "Price (CNY/month)"}}</th>
              <th>{{t .Lang "RPM" "RPM"}}</th>
              <th>{{t .Lang "5小时 Token 限额" "5h Token Limit"}}</th>
              <th>{{t .Lang "月 Token 限额" "Monthly Token Limit"}}</th>
              <th>{{t .Lang "不限量" "Unlimited"}}</th>
            </tr>
          </thead>
          <tbody>
            {{$free := index .PlanMap "free"}}
            <tr>
              <td>free</td>
              <td><input name="price_free" type="number" step="0.01" min="0" value="{{fmtAmount $free.PriceCNY}}" /></td>
              <td><input name="rpm_free" type="number" min="0" value="{{$free.RPMLimit}}" /></td>
              <td><input name="limit5h_free" type="number" min="0" value="{{$free.FiveHourTokenLimit}}" /></td>
              <td><input name="limitm_free" type="number" min="0" value="{{$free.MonthlyTokenLimit}}" /></td>
              <td><input type="checkbox" name="unlimited_free" {{if $free.Unlimited}}checked{{end}} /></td>
            </tr>
            {{$starter := index .PlanMap "starter"}}
            <tr>
              <td>starter</td>
              <td><input name="price_starter" type="number" step="0.01" min="0" value="{{fmtAmount $starter.PriceCNY}}" /></td>
              <td><input name="rpm_starter" type="number" min="0" value="{{$starter.RPMLimit}}" /></td>
              <td><input name="limit5h_starter" type="number" min="0" value="{{$starter.FiveHourTokenLimit}}" /></td>
              <td><input name="limitm_starter" type="number" min="0" value="{{$starter.MonthlyTokenLimit}}" /></td>
              <td><input type="checkbox" name="unlimited_starter" {{if $starter.Unlimited}}checked{{end}} /></td>
            </tr>
            {{$pro := index .PlanMap "pro"}}
            <tr>
              <td>pro</td>
              <td><input name="price_pro" type="number" step="0.01" min="0" value="{{fmtAmount $pro.PriceCNY}}" /></td>
              <td><input name="rpm_pro" type="number" min="0" value="{{$pro.RPMLimit}}" /></td>
              <td><input name="limit5h_pro" type="number" min="0" value="{{$pro.FiveHourTokenLimit}}" /></td>
              <td><input name="limitm_pro" type="number" min="0" value="{{$pro.MonthlyTokenLimit}}" /></td>
              <td><input type="checkbox" name="unlimited_pro" {{if $pro.Unlimited}}checked{{end}} /></td>
            </tr>
            {{$business := index .PlanMap "business"}}
            <tr>
              <td>business</td>
              <td><input name="price_business" type="number" step="0.01" min="0" value="{{fmtAmount $business.PriceCNY}}" /></td>
              <td><input name="rpm_business" type="number" min="0" value="{{$business.RPMLimit}}" /></td>
              <td><input name="limit5h_business" type="number" min="0" value="{{$business.FiveHourTokenLimit}}" /></td>
              <td><input name="limitm_business" type="number" min="0" value="{{$business.MonthlyTokenLimit}}" /></td>
              <td><input type="checkbox" name="unlimited_business" {{if $business.Unlimited}}checked{{end}} /></td>
            </tr>
          </tbody>
        </table>
      </div>
      <p class="hint">{{t .Lang "勾选“不限量”后会自动忽略 RPM/Token 限额；价格用于用户订阅下单金额。" "When unlimited is checked, RPM/token limits are ignored. Price is used for subscription orders."}}</p>
      <div class="inline-actions">
        <button class="btn" type="submit">{{t .Lang "保存配置" "Save Configs"}}</button>
        <button class="btn ghost" type="button" data-close-modal="plan-config-modal">{{t .Lang "关闭" "Close"}}</button>
      </div>
    </form>
  </div>
</div>
<div id="create-user-modal" class="opc-modal" data-modal>
  <div class="opc-modal-backdrop" data-close-modal="create-user-modal"></div>
  <div class="opc-modal-card">
    <div class="modal-head">
      <h3 class="modal-title">{{ui .Lang "create_user"}}</h3>
      <button class="btn ghost mini" type="button" data-close-modal="create-user-modal">{{t .Lang "关闭" "Close"}}</button>
    </div>
    <form class="stack" method="post" action="{{withLang "/admin/users/new" .Lang}}" data-submit-lock="1" data-loading-text="{{ui .Lang "submitting"}}">
      <input type="hidden" name="return_to" value="{{withLang "/admin/users" .Lang}}" />
      <div class="f3">
        <label>{{ui .Lang "email"}}<input name="email" type="email" required /></label>
        <label>{{ui .Lang "password"}}<input name="password" type="password" required /></label>
        <label>{{ui .Lang "role"}}
          <select name="role"><option value="member">{{enum .Lang "role" "member"}}</option><option value="admin">{{enum .Lang "role" "admin"}}</option></select>
        </label>
      </div>
      <div class="inline-actions">
        <button class="btn" type="submit">{{ui .Lang "create_user"}}</button>
        <button class="btn ghost" type="button" data-close-modal="create-user-modal">{{t .Lang "关闭" "Close"}}</button>
      </div>
    </form>
  </div>
</div>
{{range $idx, $row := .Rows}}
<div id="user-update-modal-{{$idx}}" class="opc-modal" data-modal>
  <div class="opc-modal-backdrop" data-close-modal="user-update-modal-{{$idx}}"></div>
  <div class="opc-modal-card">
    <div class="modal-head">
      <h3 class="modal-title">{{t $.Lang "更新用户" "Update User"}} {{$row.User.User.Email}}</h3>
      <button class="btn ghost mini" type="button" data-close-modal="user-update-modal-{{$idx}}">{{t $.Lang "关闭" "Close"}}</button>
    </div>
    <div class="user-role-status-stack">
      {{if ne $row.User.User.ID $.Claims.UserID}}
      <form class="stack stack-block compact-card" method="post" action="{{withLang "/admin/users/role" $.Lang}}" data-submit-lock="1" data-loading-text="{{ui $.Lang "submitting"}}">
        <input type="hidden" name="return_to" value="{{withLang "/admin/users" $.Lang}}" />
        <input type="hidden" name="user_id" value="{{$row.User.User.ID}}" />
        <h3 class="stack-title">{{ui $.Lang "update_role"}}</h3>
        <div class="switch-field">
          <p class="switch-field-title">{{ui $.Lang "update_role"}}</p>
          <div class="form-check form-switch">
            <input class="form-check-input" id="user-role-admin-{{$idx}}" type="checkbox" name="is_admin" {{if eq $row.User.Role "admin"}}checked{{end}} />
            <label class="form-check-label" for="user-role-admin-{{$idx}}">{{t $.Lang "管理员" "Admin"}}</label>
          </div>
        </div>
        <p class="hint">{{t $.Lang "关闭为普通成员，开启为管理员。" "Off = member, on = admin."}}</p>
        <button class="btn secondary mini" type="submit">{{ui $.Lang "update_role"}}</button>
      </form>
      <form class="stack stack-block compact-card" method="post" action="{{withLang "/admin/users/status" $.Lang}}" data-submit-lock="1" data-loading-text="{{ui $.Lang "submitting"}}">
        <input type="hidden" name="return_to" value="{{withLang "/admin/users" $.Lang}}" />
        <input type="hidden" name="user_id" value="{{$row.User.User.ID}}" />
        <h3 class="stack-title">{{ui $.Lang "update_status"}}</h3>
        <div class="switch-field">
          <p class="switch-field-title">{{ui $.Lang "update_status"}}</p>
          <div class="form-check form-switch">
            <input class="form-check-input" id="user-status-active-{{$idx}}" type="checkbox" name="is_active" {{if eq $row.User.User.Status "active"}}checked{{end}} />
            <label class="form-check-label" for="user-status-active-{{$idx}}">{{t $.Lang "激活" "Active"}}</label>
          </div>
        </div>
        <p class="hint">{{t $.Lang "关闭后该用户将无法登录与调用接口。" "When off, this user cannot sign in or call APIs."}}</p>
        <button class="btn secondary mini" type="submit">{{ui $.Lang "update_status"}}</button>
      </form>
      {{else}}
      <article class="stack stack-block compact-card">
        <h3 class="stack-title">{{t $.Lang "敏感状态保护" "Sensitive status protection"}}</h3>
        <p class="hint">{{t $.Lang "当前管理员账号不允许修改自己的角色/状态，也不允许删除自己。" "Current admin account cannot update its own role/status or delete itself."}}</p>
      </article>
      {{end}}
      <form class="stack stack-block compact-card" method="post" action="{{withLang "/admin/users/membership" $.Lang}}" data-submit-lock="1" data-loading-text="{{ui $.Lang "saving"}}">
        <input type="hidden" name="return_to" value="{{withLang "/admin/users" $.Lang}}" />
        <input type="hidden" name="user_id" value="{{$row.User.User.ID}}" />
        <h3 class="stack-title">{{t $.Lang "更新会员" "Update Membership"}}</h3>
        <div class="f2">
          <label>{{t $.Lang "会员等级" "Membership Tier"}}
            <select name="plan_id">
              <option value="free" {{if eq $row.Membership.PlanID "free"}}selected{{end}}>free</option>
              <option value="starter" {{if eq $row.Membership.PlanID "starter"}}selected{{end}}>starter</option>
              <option value="pro" {{if eq $row.Membership.PlanID "pro"}}selected{{end}}>pro</option>
              <option value="business" {{if eq $row.Membership.PlanID "business"}}selected{{end}}>business</option>
            </select>
          </label>
          <label>{{t $.Lang "有效期至" "Valid Until"}}
            <input name="period_end" type="datetime-local" value="{{fmtTimeInput $row.Membership.PeriodEnd}}" />
          </label>
        </div>
        <p class="hint">{{t $.Lang "free 可留空有效期；付费会员需要填写到期时间。business 为无限量套餐。" "free can leave valid-until empty; paid tiers require a valid-until time. business tier is unlimited."}}</p>
        <button class="btn secondary mini" type="submit">{{t $.Lang "更新会员" "Update Membership"}}</button>
      </form>
      <div class="inline-actions">
        <button class="btn ghost mini" type="button" data-close-modal="user-update-modal-{{$idx}}">{{t $.Lang "关闭" "Close"}}</button>
      </div>
    </div>
  </div>
</div>
{{end}}
{{end}}
`))

var adminPromptsPageTemplate = template.Must(template.Must(adminConsoleLayoutTemplate.Clone()).Parse(`
{{define "page_title"}}opcAPI Admin - APIs{{end}}
{{define "page_heading"}}{{ui .Lang "prompt_apis"}}{{end}}
{{define "page_subtitle"}}{{t .Lang "统一管理 Prompt APIs 与默认 OpenAI 兼容接口策略。" "Manage Prompt APIs and default OpenAI-compatible API behavior in one place."}}{{end}}
{{define "hero"}}
<section class="kpi-grid">
  <article class="panel kpi">
    <p class="kpi-label">{{ui .Lang "prompt_apis"}}</p>
    <p class="kpi-value">{{.TotalPrompts}}</p>
    <p class="kpi-note">{{t .Lang "筛选后总定义数" "Total filtered definitions"}}</p>
  </article>
  <article class="panel kpi">
    <p class="kpi-label">{{t .Lang "已发布" "Published"}}</p>
    <p class="kpi-value">{{.Published}}</p>
    <p class="kpi-note">{{t .Lang "筛选范围内可调用" "Published in filtered scope"}}</p>
  </article>
  <article class="panel kpi">
    <p class="kpi-label">{{t .Lang "草稿 / 其他" "Draft / Others"}}</p>
    <p class="kpi-value">{{.Unpublished}}</p>
    <p class="kpi-note">{{t .Lang "筛选范围内未发布" "Unpublished in filtered scope"}}</p>
  </article>
  <article class="panel kpi">
    <p class="kpi-label">{{t .Lang "发布比例" "Publish ratio"}}</p>
    <p class="kpi-value">{{.Published}} / {{.TotalPrompts}}</p>
    <p class="kpi-note">{{t .Lang "已发布 / 筛选总数" "Published versus filtered total"}}</p>
  </article>
</section>
{{end}}
{{define "content"}}
<article class="panel section span-12">
  <div class="section-head">
    <div>
      <h2 class="section-title">{{t .Lang "Prompt API 管理" "Prompt API Management"}}</h2>
      <p class="section-desc">{{t .Lang "支持新建、行内更新、删除，便于和用户管理保持一致。" "Create, inline update, and delete in a user-list style workflow."}}</p>
    </div>
  </div>
  <div class="table-tools">
    <form class="table-tools" method="get" action="{{withLang "/admin/prompts" .Lang}}">
      <input class="search" name="q" value="{{.FilterQ}}" data-filter-input="prompts-table" placeholder="{{t .Lang "按 slug、模型、状态、名称搜索" "Search slug, model, status or name"}}" />
      <select class="page-size" name="page_size">
        <option value="10" {{if eq .Pagination.PageSize 10}}selected{{end}}>10 / page</option>
        <option value="20" {{if eq .Pagination.PageSize 20}}selected{{end}}>20 / page</option>
        <option value="50" {{if eq .Pagination.PageSize 50}}selected{{end}}>50 / page</option>
        <option value="100" {{if eq .Pagination.PageSize 100}}selected{{end}}>100 / page</option>
      </select>
      <button class="btn secondary" type="submit">{{t .Lang "查询" "Search"}}</button>
    </form>
    <button class="btn" type="button" data-open-modal="prompt-create-modal">{{t .Lang "新建 Prompt API" "New Prompt API"}}</button>
  </div>
  <div class="table-wrap">
    <table id="prompts-table">
      <thead><tr><th>{{ui .Lang "slug"}}</th><th>{{ui .Lang "name"}}</th><th>{{ui .Lang "status"}}</th><th>{{ui .Lang "model"}}</th><th>{{ui .Lang "version"}}</th><th>{{t .Lang "详情" "Details"}}</th><th>{{ui .Lang "action"}}</th></tr></thead>
      <tbody>
        {{range $idx, $item := .PromptAPIs}}
        <tr>
          <td><span class="mono">{{$item.Slug}}</span></td>
          <td>{{$item.Name}}</td>
          <td><span class="chip {{statusTone $item.Status}}">{{enum $.Lang "status" $item.Status}}</span></td>
          <td>{{$item.Model}}</td>
          <td>{{$item.Version}}</td>
          <td>
            <details class="row-details">
              <summary>{{t $.Lang "查看详情" "View details"}}</summary>
              <div class="row-details-body">
                <p class="row-details-line"><strong>{{ui $.Lang "description"}}:</strong> {{if $item.Description}}{{short $item.Description 120}}{{else}}-{{end}}</p>
                <p class="row-details-line"><strong>{{t $.Lang "变量" "Variables"}}:</strong> {{if $item.Variables}}<span class="mono">{{range $i, $v := $item.Variables}}{{if $i}}, {{end}}{{$v}}{{end}}</span>{{else}}-{{end}}</p>
                <p class="row-details-line"><strong>{{ui $.Lang "system_prompt"}}:</strong> {{if $item.SystemPrompt}}{{short $item.SystemPrompt 120}}{{else}}-{{end}}</p>
                <p class="row-details-line"><strong>{{ui $.Lang "user_template"}}:</strong> {{if $item.UserTemplate}}<span class="mono">{{short $item.UserTemplate 120}}</span>{{else}}-{{end}}</p>
                <p class="row-details-line"><strong>{{t $.Lang "输入字段说明" "Input schema"}}:</strong> {{if $item.InputSchema}}<span class="mono">{{short $item.InputSchema 120}}</span>{{else}}-{{end}}</p>
                <p class="row-details-line"><strong>{{t $.Lang "输出格式约束" "Output schema"}}:</strong> {{if $item.OutputSchema}}<span class="mono">{{short $item.OutputSchema 120}}</span>{{else}}-{{end}}</p>
              </div>
            </details>
          </td>
          <td>
            <div class="inline-actions">
              <button class="btn secondary mini" type="button" data-open-modal="prompt-update-modal-{{$idx}}">{{t $.Lang "更新" "Update"}}</button>
              <form method="post" action="{{withLang "/admin/prompt-apis/delete" $.Lang}}" data-submit-lock="1" data-loading-text="{{ui $.Lang "deleting"}}" data-confirm="{{t $.Lang "确认删除该 Prompt API？" "Delete this prompt api?"}}">
                <input type="hidden" name="return_to" value="{{withLang "/admin/prompts" $.Lang}}" />
                <input type="hidden" name="slug" value="{{$item.Slug}}" />
                <button class="btn secondary mini" type="submit">{{ui $.Lang "delete"}}</button>
              </form>
            </div>
          </td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>
  <div class="pager">
    <p class="pager-info">{{t .Lang "第" "Page"}} {{.Pagination.Page}} / {{.Pagination.TotalPages}} , {{t .Lang "共" "Total"}} {{.Pagination.Total}} {{t .Lang "条" "rows"}}</p>
    <div class="inline-actions">
      {{if .Pagination.HasPrev}}<a class="btn secondary" href="{{.Pagination.PrevURL}}">{{t .Lang "上一页" "Previous"}}</a>{{end}}
      {{if .Pagination.HasNext}}<a class="btn secondary" href="{{.Pagination.NextURL}}">{{t .Lang "下一页" "Next"}}</a>{{end}}
      <form class="page-jump" method="get" action="{{withLang "/admin/prompts" .Lang}}">
        <input type="hidden" name="q" value="{{.FilterQ}}" />
        <input type="hidden" name="page_size" value="{{.Pagination.PageSize}}" />
        <input class="jump-input" type="number" min="1" max="{{.Pagination.TotalPages}}" name="page" value="{{.Pagination.Page}}" />
        <button class="btn secondary" type="submit">{{t .Lang "跳转" "Go"}}</button>
      </form>
    </div>
  </div>
</article>
<article class="panel section span-12">
  <div class="section-head">
    <div>
      <h2 class="section-title">{{t .Lang "默认 API 管理" "Default API Management"}}</h2>
      <p class="section-desc">{{t .Lang "配置默认 OpenAI 兼容接口的默认模型、允许模型与 model id 映射。" "Configure default model, allowed models, and model-id mapping for OpenAI-compatible APIs."}}</p>
    </div>
  </div>
  <div class="table-wrap">
    <table id="default-apis-table">
      <thead><tr><th>{{t .Lang "接口" "API"}}</th><th>{{ui .Lang "status"}}</th><th>{{t .Lang "默认模型" "Default Model"}}</th><th>{{t .Lang "允许模型" "Allowed Models"}}</th><th>{{t .Lang "model id 映射" "Model ID Mappings"}}</th><th>{{ui .Lang "action"}}</th></tr></thead>
      <tbody>
        {{range $idx, $item := .DefaultAPIs}}
        <tr>
          <td>
            <p><strong>{{$item.Name}}</strong></p>
            <p class="mono">{{$item.APIPath}}</p>
          </td>
          <td><span class="chip {{if $item.Enabled}}tone-good{{else}}tone-danger{{end}}">{{if $item.Enabled}}{{t $.Lang "启用" "Enabled"}}{{else}}{{t $.Lang "停用" "Disabled"}}{{end}}</span></td>
          <td>{{if $item.DefaultModel}}<span class="mono">{{$item.DefaultModel}}</span>{{else}}-{{end}}</td>
          <td>{{if $item.AllowedModels}}<span class="mono">{{range $i, $v := $item.AllowedModels}}{{if $i}}, {{end}}{{$v}}{{end}}</span>{{else}}{{t $.Lang "全部（不限制）" "All (unrestricted)"}}{{end}}</td>
          <td>{{if $item.ModelIDMappings}}<span class="mono">{{range $i, $m := $item.ModelIDMappings}}{{if $i}}; {{end}}{{$m.ModelID}}=&gt;{{$m.Model}}{{end}}</span>{{else}}-{{end}}</td>
          <td>
            <button class="btn secondary mini" type="button" data-open-modal="default-api-update-modal-{{$idx}}">{{t $.Lang "更新" "Update"}}</button>
          </td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>
</article>
<div id="prompt-create-modal" class="opc-modal" data-modal>
  <div class="opc-modal-backdrop" data-close-modal="prompt-create-modal"></div>
  <div class="opc-modal-card">
    <div class="modal-head">
      <h3 class="modal-title">{{t .Lang "新建 Prompt API" "New Prompt API"}}</h3>
      <button class="btn ghost mini" type="button" data-close-modal="prompt-create-modal">{{t .Lang "关闭" "Close"}}</button>
    </div>
    <form class="stack" method="post" action="{{withLang "/admin/prompt-apis/new" .Lang}}" data-submit-lock="1" data-loading-text="{{ui .Lang "saving"}}" data-prompt-preview-form data-prompt-preview-url="{{withLang "/admin/prompt-apis/preview" .Lang}}">
      <input type="hidden" name="return_to" value="{{withLang "/admin/prompts" .Lang}}" />
      <div class="stack-block prompt-create-guide">
        <h3 class="stack-title">{{t .Lang "创建说明" "Creation Guide"}}</h3>
        <p class="hint">{{t .Lang "极简模式：只填标识、名称、模型、任务说明即可创建；其余字段系统自动补全。" "Simple mode: only slug, name, model and task instruction are required; other fields are auto-filled."}}</p>
        <p class="prompt-endpoint">{{t .Lang "生成调用地址：" "Generated endpoint:"}} <span class="mono" id="prompt-create-endpoint-preview">/v1/prompt-apis/{slug}</span></p>
        <p class="hint">{{t .Lang "默认请求体：{\"input\":\"...\"}；也支持请求头 X-Prompt-Var-<key> 自动补充变量。" "Default request body: {\"input\":\"...\"}; headers X-Prompt-Var-<key> are also supported."}}</p>
      </div>
      <div class="f3">
        <label>{{ui .Lang "slug"}}<input name="slug" required data-prompt-slug-input="1" data-endpoint-target="prompt-create-endpoint-preview" /></label>
        <label>{{ui .Lang "name"}}<input name="name" required /></label>
        <label>{{ui .Lang "model"}}
          <select name="model" required>
            <option value="">{{t .Lang "请选择系统可用模型" "Select an available model"}}</option>
            {{range .ModelOptions}}
            <option value="{{.}}">{{.}}</option>
            {{end}}
          </select>
        </label>
      </div>
      <div class="switch-field">
        <p class="switch-field-title">{{ui .Lang "publish"}}</p>
        <div class="form-check form-switch">
          <input class="form-check-input" id="prompt-create-publish" type="checkbox" name="publish" checked />
          <label class="form-check-label" for="prompt-create-publish">{{ui .Lang "publish"}}</label>
        </div>
      </div>
      <div class="f2">
        <label>{{t .Lang "任务说明（必填）" "Task instruction (required)"}}
          <textarea name="task_instruction" placeholder="{{t .Lang "例如：你是一名专业翻译助手，输入任意文本后返回高质量中文翻译。" "For example: You are a professional translator. Return high-quality Chinese translation for any input text."}}" required></textarea>
        </label>
        <label>{{t .Lang "输入字段名" "Input field key"}}
          <input name="input_key" value="input" placeholder="input" />
          <span class="hint">{{t .Lang "默认 input，会自动生成 User Template = {{.input}}" "Default is input, auto-generates User Template = {{.input}}"}}</span>
        </label>
      </div>
      <label>{{ui .Lang "description"}}<input name="description" /></label>
      <details class="stack-block">
        <summary class="stack-title">{{t .Lang "高级设置（可选）" "Advanced settings (optional)"}}</summary>
        <div class="stack">
          <label>{{ui .Lang "variables_comma"}}<input name="variables" placeholder="input" /></label>
          <label>{{ui .Lang "system_prompt"}}<textarea name="system_prompt"></textarea></label>
          <label>{{ui .Lang "user_template"}}<textarea name="user_template" placeholder="{{t .Lang "留空自动生成，例如 {{.input}}" "Leave empty to auto-generate, e.g. {{.input}}"}}"></textarea></label>
          <label>{{t .Lang "输入字段说明" "Input schema"}}<textarea name="input_schema" placeholder='{"input":"string"}'></textarea></label>
          <label>{{t .Lang "输出格式约束" "Output schema / format"}}<textarea name="output_schema" placeholder='{"output":"string"}'></textarea></label>
          <div class="f2">
            <label>{{t .Lang "示例输入（JSON）" "Example input (JSON)"}}<textarea name="example_input" placeholder='{"input":"hello"}'></textarea></label>
            <label>{{t .Lang "示例输出" "Example output"}}<textarea name="example_output" placeholder='{"output":"..."}'></textarea></label>
          </div>
        </div>
      </details>
      <div class="stack-block prompt-preview-panel">
        <h3 class="stack-title">{{t .Lang "即时试运行" "Live preview"}}</h3>
        <p class="hint">{{t .Lang "填写预览输入 JSON，点击试运行即可看到模型返回；确认后再保存。" "Provide preview input JSON and run preview before saving."}}</p>
        <label>{{t .Lang "预览输入（JSON）" "Preview input (JSON)"}}<textarea name="preview_input" placeholder='{"input":"请翻译这段文本"}'></textarea></label>
        <div class="inline-actions">
          <button class="btn secondary mini" type="button" data-prompt-preview-btn data-loading-text="{{t .Lang "试运行中..." "Running..."}}">{{t .Lang "试运行" "Run Preview"}}</button>
        </div>
        <pre class="prompt-preview-result" data-prompt-preview-result>{{t .Lang "这里显示渲染后的提示词与模型返回结果。" "Rendered prompt and model output will appear here."}}</pre>
      </div>
      <div class="inline-actions">
        <button class="btn" type="submit">{{ui .Lang "save_prompt_api"}}</button>
        <button class="btn ghost" type="button" data-close-modal="prompt-create-modal">{{t .Lang "关闭" "Close"}}</button>
      </div>
    </form>
  </div>
</div>
{{range $idx, $item := .PromptAPIs}}
<div id="prompt-update-modal-{{$idx}}" class="opc-modal" data-modal>
  <div class="opc-modal-backdrop" data-close-modal="prompt-update-modal-{{$idx}}"></div>
  <div class="opc-modal-card">
    <div class="modal-head">
      <h3 class="modal-title">{{t $.Lang "更新 Prompt API" "Update Prompt API"}} {{$item.Slug}}</h3>
      <button class="btn ghost mini" type="button" data-close-modal="prompt-update-modal-{{$idx}}">{{t $.Lang "关闭" "Close"}}</button>
    </div>
    <form class="stack" method="post" action="{{withLang "/admin/prompt-apis/new" $.Lang}}" data-submit-lock="1" data-loading-text="{{ui $.Lang "saving"}}" data-prompt-preview-form data-prompt-preview-url="{{withLang "/admin/prompt-apis/preview" $.Lang}}">
      <input type="hidden" name="return_to" value="{{withLang "/admin/prompts" $.Lang}}" />
      <div class="stack-block prompt-create-guide">
        <h3 class="stack-title">{{t $.Lang "调用地址" "Endpoint"}}</h3>
        <p class="prompt-endpoint"><span class="mono" id="prompt-update-endpoint-preview-{{$idx}}">/v1/prompt-apis/{{$item.Slug}}</span></p>
        <p class="hint">{{t $.Lang "变量 = input 字段键；可用请求头 X-Prompt-Var-<key> 自动补充。" "Variables map to input keys; headers X-Prompt-Var-<key> can auto-fill values."}}</p>
      </div>
      <div class="f3">
        <label>{{ui $.Lang "slug"}}<input name="slug" value="{{$item.Slug}}" required readonly data-prompt-slug-input="1" data-endpoint-target="prompt-update-endpoint-preview-{{$idx}}" /></label>
        <label>{{ui $.Lang "name"}}<input name="name" value="{{$item.Name}}" required /></label>
        <label>{{ui $.Lang "model"}}
          <select name="model" required>
            <option value="">{{t $.Lang "请选择系统可用模型" "Select an available model"}}</option>
            {{range $.ModelOptions}}
            <option value="{{.}}" {{if eq . $item.Model}}selected{{end}}>{{.}}</option>
            {{end}}
          </select>
        </label>
      </div>
      <div class="switch-field">
        <p class="switch-field-title">{{ui $.Lang "publish"}}</p>
        <div class="form-check form-switch">
          <input class="form-check-input" id="prompt-update-publish-{{$idx}}" type="checkbox" name="publish" {{if eq $item.Status "published"}}checked{{end}} />
          <label class="form-check-label" for="prompt-update-publish-{{$idx}}">{{ui $.Lang "publish"}}</label>
        </div>
      </div>
      <div class="f2">
        <label>{{t $.Lang "任务说明（推荐）" "Task instruction (recommended)"}}
          <textarea name="task_instruction" placeholder="{{t $.Lang "例如：你是一名专业翻译助手，输入文本后返回高质量中文翻译。" "For example: You are a professional translator. Return high-quality Chinese translation for the input text."}}">{{$item.SystemPrompt}}</textarea>
        </label>
        <label>{{t $.Lang "输入字段名" "Input field key"}}
          <input name="input_key" value="{{if gt (len $item.Variables) 0}}{{index $item.Variables 0}}{{else}}input{{end}}" placeholder="input" />
          <span class="hint">{{t $.Lang "默认按此字段生成模板变量（可在高级设置中覆盖）。" "Default template variable will use this key (override in advanced settings)."}}</span>
        </label>
      </div>
      <label>{{ui $.Lang "description"}}<input name="description" value="{{$item.Description}}" /></label>
      <details class="stack-block">
        <summary class="stack-title">{{t $.Lang "高级设置（可选）" "Advanced settings (optional)"}}</summary>
        <div class="stack">
          <label>{{ui $.Lang "variables_comma"}}<input name="variables" value="{{range $i, $v := $item.Variables}}{{if $i}},{{end}}{{$v}}{{end}}" /></label>
          <label>{{ui $.Lang "system_prompt"}}<textarea name="system_prompt">{{$item.SystemPrompt}}</textarea></label>
          <label>{{ui $.Lang "user_template"}}<textarea name="user_template">{{$item.UserTemplate}}</textarea></label>
          <label>{{t $.Lang "输入字段说明" "Input schema"}}<textarea name="input_schema">{{$item.InputSchema}}</textarea></label>
          <label>{{t $.Lang "输出格式约束" "Output schema / format"}}<textarea name="output_schema">{{$item.OutputSchema}}</textarea></label>
          <div class="f2">
            <label>{{t $.Lang "示例输入（JSON）" "Example input (JSON)"}}<textarea name="example_input">{{$item.ExampleInput}}</textarea></label>
            <label>{{t $.Lang "示例输出" "Example output"}}<textarea name="example_output">{{$item.ExampleOutput}}</textarea></label>
          </div>
        </div>
      </details>
      <div class="stack-block prompt-preview-panel">
        <h3 class="stack-title">{{t $.Lang "即时试运行" "Live preview"}}</h3>
        <label>{{t $.Lang "预览输入（JSON）" "Preview input (JSON)"}}<textarea name="preview_input">{{if $item.ExampleInput}}{{$item.ExampleInput}}{{else}}{}{{end}}</textarea></label>
        <div class="inline-actions">
          <button class="btn secondary mini" type="button" data-prompt-preview-btn data-loading-text="{{t $.Lang "试运行中..." "Running..."}}">{{t $.Lang "试运行" "Run Preview"}}</button>
        </div>
        <pre class="prompt-preview-result" data-prompt-preview-result>{{if $item.ExampleOutput}}{{$item.ExampleOutput}}{{else}}{{t $.Lang "这里显示渲染后的提示词与模型返回结果。" "Rendered prompt and model output will appear here."}}{{end}}</pre>
      </div>
      <div class="inline-actions">
        <button class="btn" type="submit">{{ui $.Lang "save_prompt_api"}}</button>
        <button class="btn ghost mini" type="button" data-close-modal="prompt-update-modal-{{$idx}}">{{t $.Lang "关闭" "Close"}}</button>
      </div>
    </form>
  </div>
</div>
{{end}}
{{range $idx, $item := .DefaultAPIs}}
<div id="default-api-update-modal-{{$idx}}" class="opc-modal" data-modal>
  <div class="opc-modal-backdrop" data-close-modal="default-api-update-modal-{{$idx}}"></div>
  <div class="opc-modal-card">
    <div class="modal-head">
      <h3 class="modal-title">{{t $.Lang "更新默认 API" "Update Default API"}} {{$item.APIPath}}</h3>
      <button class="btn ghost mini" type="button" data-close-modal="default-api-update-modal-{{$idx}}">{{t $.Lang "关闭" "Close"}}</button>
    </div>
    <form class="stack" method="post" action="{{withLang "/admin/default-apis/update" $.Lang}}" data-submit-lock="1" data-loading-text="{{ui $.Lang "saving"}}">
      <input type="hidden" name="return_to" value="{{withLang "/admin/prompts" $.Lang}}" />
      <input type="hidden" name="api_key" value="{{$item.APIKey}}" />
      <div class="switch-field">
        <p class="switch-field-title">{{ui $.Lang "status"}}</p>
        <div class="form-check form-switch">
          <input class="form-check-input" id="default-api-enabled-{{$idx}}" type="checkbox" name="enabled" {{if $item.Enabled}}checked{{end}} />
          <label class="form-check-label" for="default-api-enabled-{{$idx}}">{{t $.Lang "启用接口策略" "Enable API policy"}}</label>
        </div>
      </div>
      {{if eq $item.APIKey "chat_completions"}}
      <div class="f2">
        <label>{{t $.Lang "默认模型（调用方不传 model 时生效）" "Default model (when caller omits model)"}}
          <select name="default_model">
            <option value="">{{t $.Lang "不设置（走系统路由默认）" "Not set (fallback to system routing default)"}}</option>
            {{range $.ModelOptions}}
            <option value="{{.}}" {{if eq . $item.DefaultModel}}selected{{end}}>{{.}}</option>
            {{end}}
          </select>
          <span class="hint">{{t $.Lang "未设置时，沿用系统路由默认模型。" "If unset, system route default will be used."}}</span>
        </label>
      </div>
      <label>{{t $.Lang "输入 model id 映射（每行 model_id=内部模型）" "Input model-id mapping (one per line: model_id=internal_model)"}}
        <textarea name="model_mappings" placeholder="gpt-4o=route-default&#10;gpt-4o-mini=qwen-plus">{{defaultAPIMappingsText $item.ModelIDMappings}}</textarea>
        <span class="hint">{{t $.Lang "允许模型将自动按映射目标推导；调用方 model 只接受这里定义的 model id。若留空则不限制调用方 model（默认模型仍可生效）。" "Allowed models are auto-derived from mapping targets; caller model only accepts mapped model ids. Leave empty for unrestricted caller models (default model still works)."}}</span>
      </label>
      {{else}}
      <p class="hint">{{t $.Lang "该接口暂无模型相关配置。" "This API currently has no model-level options."}}</p>
      {{end}}
      <div class="inline-actions">
        <button class="btn" type="submit">{{t $.Lang "保存默认 API 配置" "Save Default API Config"}}</button>
        <button class="btn ghost mini" type="button" data-close-modal="default-api-update-modal-{{$idx}}">{{t $.Lang "关闭" "Close"}}</button>
      </div>
    </form>
  </div>
</div>
{{end}}
{{end}}
`))

var adminOrdersPageTemplate = template.Must(template.Must(adminConsoleLayoutTemplate.Clone()).Parse(`
{{define "page_title"}}opcAPI Admin - Orders{{end}}
{{define "page_heading"}}{{uiOr .Lang "order_management" "Order Management"}}{{end}}
{{define "page_subtitle"}}{{t .Lang "订单查询与订单清理统一管理，订单记录会员购买行为。" "Unified order query and cleanup; orders represent membership purchases."}}{{end}}
{{define "hero"}}
<section class="kpi-grid">
  <article class="panel kpi">
    <p class="kpi-label">{{t .Lang "订单总数" "Total orders"}}</p>
    <p class="kpi-value">{{.TotalOrders}}</p>
    <p class="kpi-note">{{t .Lang "系统内全部订单" "Total orders in system"}}</p>
  </article>
  <article class="panel kpi">
    <p class="kpi-label">{{t .Lang "当前页条数" "Rows in page"}}</p>
    <p class="kpi-value">{{len .Orders}}</p>
    <p class="kpi-note">{{t .Lang "支持分页浏览" "Paginated"}}</p>
  </article>
  <article class="panel kpi">
    <p class="kpi-label">{{t .Lang "页码" "Page"}}</p>
    <p class="kpi-value">{{.Pagination.Page}} / {{.Pagination.TotalPages}}</p>
    <p class="kpi-note">{{t .Lang "可按页跳转" "Navigate by page"}}</p>
  </article>
  <article class="panel kpi">
    <p class="kpi-label">{{t .Lang "每页条数" "Page size"}}</p>
    <p class="kpi-value">{{.Pagination.PageSize}}</p>
    <p class="kpi-note">{{t .Lang "通过 page_size 调整" "Adjust via page_size"}}</p>
  </article>
</section>
{{end}}
{{define "content"}}
<article class="panel section span-12">
  <div class="section-head">
    <div>
      <h2 class="section-title">{{uiOr .Lang "order_management" "Order Management"}}</h2>
      <p class="section-desc">{{t .Lang "支持查询和删除订单；订单仅记录会员订阅。" "Supports query and delete; orders only record membership subscriptions."}}</p>
    </div>
  </div>

  <form class="stack stack-block" method="get" action="{{withLang "/admin/orders" .Lang}}">
    <div class="f3">
      <label>{{ui .Lang "id"}}<input name="id" value="{{.FilterID}}" placeholder="ord_xxx" /></label>
      <label>{{ui .Lang "user_id"}}<input name="user_id" value="{{.FilterUser}}" placeholder="usr_xxx" /></label>
      <label>{{ui .Lang "type"}}
        <select name="type">
          <option value="">{{t .Lang "全部" "All"}}</option>
          <option value="subscription" {{if eq .FilterType "subscription"}}selected{{end}}>subscription</option>
        </select>
      </label>
    </div>
    <div class="f3">
      <label>{{ui .Lang "channel"}}
        <select name="channel">
          <option value="">{{t .Lang "全部" "All"}}</option>
          <option value="manual" {{if eq .FilterChan "manual"}}selected{{end}}>manual</option>
          <option value="stripe" {{if eq .FilterChan "stripe"}}selected{{end}}>stripe</option>
          <option value="yipay" {{if eq .FilterChan "yipay"}}selected{{end}}>yipay</option>
        </select>
      </label>
      <label>{{ui .Lang "status"}}
        <select name="status">
          <option value="">{{t .Lang "全部" "All"}}</option>
          <option value="paid" {{if eq .FilterStat "paid"}}selected{{end}}>paid</option>
          <option value="pending" {{if eq .FilterStat "pending"}}selected{{end}}>pending</option>
          <option value="failed" {{if eq .FilterStat "failed"}}selected{{end}}>failed</option>
          <option value="canceled" {{if eq .FilterStat "canceled"}}selected{{end}}>canceled</option>
          <option value="refunded" {{if eq .FilterStat "refunded"}}selected{{end}}>refunded</option>
        </select>
      </label>
      <label>{{t .Lang "每页条数" "Page size"}}
        <select name="page_size">
          <option value="10" {{if eq .Pagination.PageSize 10}}selected{{end}}>10 / page</option>
          <option value="20" {{if eq .Pagination.PageSize 20}}selected{{end}}>20 / page</option>
          <option value="50" {{if eq .Pagination.PageSize 50}}selected{{end}}>50 / page</option>
          <option value="100" {{if eq .Pagination.PageSize 100}}selected{{end}}>100 / page</option>
        </select>
      </label>
    </div>
    <div class="inline-actions">
      <button class="btn" type="submit">{{t .Lang "查询" "Query"}}</button>
      <a class="btn secondary" href="{{withLang "/admin/orders" .Lang}}">{{t .Lang "重置" "Reset"}}</a>
    </div>
  </form>

  <div class="table-wrap">
    <table id="orders-table">
      <thead><tr><th>{{ui .Lang "id"}}</th><th>{{ui .Lang "user"}}</th><th>{{ui .Lang "type"}}</th><th>{{t .Lang "套餐" "Plan"}}</th><th>{{ui .Lang "channel"}}</th><th>{{ui .Lang "amount"}}</th><th>{{ui .Lang "currency"}}</th><th>{{ui .Lang "status"}}</th><th>{{ui .Lang "created"}}</th><th>{{ui .Lang "action"}}</th></tr></thead>
      <tbody>
        {{range .Orders}}
        <tr>
          <td><span class="mono">{{.ID}}</span></td>
          <td><span class="mono">{{.UserID}}</span></td>
          <td>{{enum $.Lang "order_type" .Type}}</td>
          <td>{{if .PlanID}}{{.PlanID}}{{else}}-{{end}}</td>
          <td>{{enum $.Lang "channel" .Channel}}</td>
          <td>{{fmtAmount .Amount}}</td>
          <td>{{.Currency}}</td>
          <td><span class="chip {{statusTone .Status}}">{{enum $.Lang "order_status" .Status}}</span></td>
          <td>{{fmtTime .CreatedAt}}</td>
          <td>
            <form method="post" action="{{withLang "/admin/orders/delete" $.Lang}}" data-submit-lock="1" data-loading-text="{{ui $.Lang "deleting"}}" data-confirm="{{t $.Lang "确认删除该订单？" "Delete this order?"}}">
              <input type="hidden" name="return_to" value="{{withLang "/admin/orders" $.Lang}}" />
              <input type="hidden" name="order_id" value="{{.ID}}" />
              <button class="btn secondary mini" type="submit">{{ui $.Lang "delete"}}</button>
            </form>
          </td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>

  <div class="pager">
    <p class="pager-info">{{t .Lang "第" "Page"}} {{.Pagination.Page}} / {{.Pagination.TotalPages}} , {{t .Lang "共" "Total"}} {{.Pagination.Total}} {{t .Lang "条" "rows"}}</p>
    <div class="inline-actions">
      {{if .Pagination.HasPrev}}<a class="btn secondary" href="{{.Pagination.PrevURL}}">{{t .Lang "上一页" "Previous"}}</a>{{end}}
      {{if .Pagination.HasNext}}<a class="btn secondary" href="{{.Pagination.NextURL}}">{{t .Lang "下一页" "Next"}}</a>{{end}}
      <form class="page-jump" method="get" action="{{withLang "/admin/orders" .Lang}}">
        <input type="hidden" name="id" value="{{.FilterID}}" />
        <input type="hidden" name="user_id" value="{{.FilterUser}}" />
        <input type="hidden" name="type" value="{{.FilterType}}" />
        <input type="hidden" name="channel" value="{{.FilterChan}}" />
        <input type="hidden" name="status" value="{{.FilterStat}}" />
        <input type="hidden" name="page_size" value="{{.Pagination.PageSize}}" />
        <input class="jump-input" type="number" min="1" max="{{.Pagination.TotalPages}}" name="page" value="{{.Pagination.Page}}" />
        <button class="btn secondary" type="submit">{{t .Lang "跳转" "Go"}}</button>
      </form>
    </div>
  </div>
</article>
{{end}}
`))

var adminUpstreamsPageTemplate = template.Must(template.Must(adminConsoleLayoutTemplate.Clone()).Parse(`
{{define "page_title"}}opcAPI Admin - Upstreams & Routing{{end}}
{{define "page_heading"}}{{if eq .Active "routes"}}{{t .Lang "模型路由管理" "Model Routing"}}{{else}}{{t .Lang "上游配置管理" "Upstream Configuration"}}{{end}}{{end}}
{{define "page_subtitle"}}{{if eq .Active "routes"}}{{t .Lang "按模型别名管理主/副目标与权重策略。" "Manage primary/fallback targets and weights by model alias."}}{{else}}{{t .Lang "管理上游服务连接、状态与可用性。" "Manage upstream connectivity, status, and availability."}}{{end}}{{end}}
{{define "hero"}}
<section class="kpi-grid">
  <article class="panel kpi">
    <p class="kpi-label">{{t .Lang "上游数量" "Upstreams"}}</p>
    <p class="kpi-value">{{len .Upstreams}}</p>
    <p class="kpi-note">{{t .Lang "已配置服务" "Configured providers"}}</p>
  </article>
  <article class="panel kpi">
    <p class="kpi-label">{{t .Lang "健康上游" "Healthy"}}</p>
    <p class="kpi-value">{{countHealthyUpstreams .Upstreams}}</p>
    <p class="kpi-note">{{t .Lang "最近一次连通性诊断" "Latest reachability diagnose status"}}</p>
  </article>
  <article class="panel kpi">
    <p class="kpi-label">{{t .Lang "路由数量" "Routes"}}</p>
    <p class="kpi-value">{{len .Routes}}</p>
    <p class="kpi-note">{{countEnabledRoutes .Routes}} {{t .Lang "启用中" "enabled"}}</p>
  </article>
  <article class="panel kpi">
    <p class="kpi-label">{{t .Lang "运行统计行" "Runtime Stat Rows"}}</p>
    <p class="kpi-value">{{len .RuntimeStats}}</p>
    <p class="kpi-note">{{t .Lang "用于路由诊断" "Used for routing diagnostics"}}</p>
  </article>
</section>
{{end}}
{{define "content"}}
<article class="panel section span-12">
  {{if eq .Active "upstreams"}}
  <section class="stack">
    <div class="section-head">
      <div>
        <h2 class="section-title">{{ui .Lang "upstream_providers"}}</h2>
        <p class="section-desc">{{t .Lang "新建、更新、删除与诊断统一在表格动作中。" "Create, update, delete, and diagnose from table actions."}}</p>
      </div>
    </div>
    <div class="table-tools">
      <form class="table-tools" method="get" action="{{withLang .Path .Lang}}">
        <input type="hidden" name="q_route" value="{{.FilterRouteQ}}" />
        <input type="hidden" name="q_runtime" value="{{.FilterRunQ}}" />
        <input type="hidden" name="route_page_size" value="{{.RoutePagination.PageSize}}" />
        <input type="hidden" name="run_page_size" value="{{.RuntimePagination.PageSize}}" />
        <input class="search" name="q_upstream" value="{{.FilterUpQ}}" data-filter-input="upstreams-table" placeholder="{{t .Lang "按 ID、名称、连通性、URL 搜索" "Search id, name, reachability, base url"}}" />
        <select class="page-size" name="up_page_size">
          <option value="10" {{if eq .UpstreamPagination.PageSize 10}}selected{{end}}>10 / page</option>
          <option value="12" {{if eq .UpstreamPagination.PageSize 12}}selected{{end}}>12 / page</option>
          <option value="20" {{if eq .UpstreamPagination.PageSize 20}}selected{{end}}>20 / page</option>
          <option value="50" {{if eq .UpstreamPagination.PageSize 50}}selected{{end}}>50 / page</option>
          <option value="100" {{if eq .UpstreamPagination.PageSize 100}}selected{{end}}>100 / page</option>
        </select>
        <button class="btn secondary" type="submit">{{t .Lang "查询" "Search"}}</button>
      </form>
      <button class="btn" type="button" data-open-modal="upstream-create-modal">{{t .Lang "新建上游" "New Upstream"}}</button>
    </div>
    <div class="table-wrap">
      <table id="upstreams-table">
        <thead><tr><th>{{ui .Lang "id"}}</th><th>{{ui .Lang "name"}}</th><th>{{ui .Lang "type"}}</th><th>{{ui .Lang "base_url"}}</th><th>{{t .Lang "上游模型" "Upstream Models"}}</th><th>{{ui .Lang "enabled"}}</th><th>{{ui .Lang "health"}}</th><th>{{t .Lang "HTTP" "HTTP"}}</th><th>{{ui .Lang "latency"}}</th><th>{{ui .Lang "checked_at"}}</th><th>{{ui .Lang "action"}}</th></tr></thead>
        <tbody>
          {{range $idx, $item := .Upstreams}}
          <tr>
            <td><span class="mono">{{$item.ID}}</span></td>
            <td>{{$item.Name}}</td>
            <td><span class="chip {{statusTone $item.Type}}">{{$item.Type}}</span></td>
            <td>{{$item.BaseURL}}</td>
            <td>{{if $item.Models}}<span class="mono">{{csv $item.Models}}</span>{{else}}-{{end}}</td>
            <td><span class="chip {{statusTone (boolLabel $item.Enabled)}}">{{boolText $.Lang $item.Enabled}}</span></td>
            <td><span class="chip {{statusTone $item.LastHealthStatus}}" {{if $item.LastHealthError}}title="{{$item.LastHealthError}}"{{end}}>{{enum $.Lang "status" $item.LastHealthStatus}}</span></td>
            <td>{{if gt $item.LastHealthHTTPCode 0}}{{$item.LastHealthHTTPCode}}{{else}}-{{end}}</td>
            <td>{{fmtLatency $item.LastHealthLatency}}</td>
            <td>{{fmtTime $item.LastHealthCheckedAt}}</td>
            <td>
              <div class="inline-actions">
                <button class="btn secondary mini" type="button" data-open-modal="upstream-update-modal-{{$idx}}">{{t $.Lang "更新" "Update"}}</button>
                <button class="btn secondary mini" type="button" data-open-modal="upstream-diagnose-modal-{{$idx}}">{{t $.Lang "诊断" "Diagnose"}}</button>
                <form method="post" action="{{withLang "/admin/upstreams/delete" $.Lang}}" data-submit-lock="1" data-loading-text="{{ui $.Lang "deleting"}}" data-confirm="{{ui $.Lang "delete_this_upstream"}}">
                  <input type="hidden" name="return_to" value="{{withLang $.Path $.Lang}}" />
                  <input type="hidden" name="id" value="{{$item.ID}}" />
                  <button class="btn secondary mini" type="submit">{{ui $.Lang "delete"}}</button>
                </form>
              </div>
            </td>
          </tr>
          {{end}}
        </tbody>
      </table>
    </div>
    <div class="pager">
      <p class="pager-info">{{t .Lang "第" "Page"}} {{.UpstreamPagination.Page}} / {{.UpstreamPagination.TotalPages}} , {{t .Lang "共" "Total"}} {{.UpstreamPagination.Total}} {{t .Lang "条" "rows"}}</p>
      <div class="inline-actions">
        {{if .UpstreamPagination.HasPrev}}<a class="btn secondary" href="{{.UpstreamPagination.PrevURL}}">{{t .Lang "上一页" "Previous"}}</a>{{end}}
        {{if .UpstreamPagination.HasNext}}<a class="btn secondary" href="{{.UpstreamPagination.NextURL}}">{{t .Lang "下一页" "Next"}}</a>{{end}}
        <form class="page-jump" method="get" action="{{withLang .Path .Lang}}">
          <input type="hidden" name="q_upstream" value="{{.FilterUpQ}}" />
          <input type="hidden" name="q_route" value="{{.FilterRouteQ}}" />
          <input type="hidden" name="q_runtime" value="{{.FilterRunQ}}" />
          <input type="hidden" name="route_page_size" value="{{.RoutePagination.PageSize}}" />
          <input type="hidden" name="run_page_size" value="{{.RuntimePagination.PageSize}}" />
          <input type="hidden" name="up_page_size" value="{{.UpstreamPagination.PageSize}}" />
          <input class="jump-input" type="number" min="1" max="{{.UpstreamPagination.TotalPages}}" name="up_page" value="{{.UpstreamPagination.Page}}" />
          <button class="btn secondary" type="submit">{{t .Lang "跳转" "Go"}}</button>
        </form>
      </div>
    </div>
  </section>
  <div id="upstream-create-modal" class="opc-modal" data-modal>
    <div class="opc-modal-backdrop" data-close-modal="upstream-create-modal"></div>
    <div class="opc-modal-card">
      <div class="modal-head">
        <h3 class="modal-title">{{t .Lang "新建上游" "New Upstream"}}</h3>
        <button class="btn ghost mini" type="button" data-close-modal="upstream-create-modal">{{t .Lang "关闭" "Close"}}</button>
      </div>
      <form class="stack" method="post" action="{{withLang "/admin/upstreams/new" .Lang}}" data-submit-lock="1" data-loading-text="{{ui .Lang "saving"}}">
        <input type="hidden" name="return_to" value="{{withLang .Path .Lang}}" />
        <div class="f3">
          <label>{{ui .Lang "name"}}<input name="name" required /></label>
          <label>{{ui .Lang "type"}}
            <select name="type">
              <option value="openai_compat">openai_compat</option>
              <option value="anthropic">anthropic</option>
              <option value="gemini">gemini</option>
              <option value="ollama">ollama</option>
            </select>
          </label>
          <div class="switch-field">
            <p class="switch-field-title">{{ui .Lang "enabled"}}</p>
            <div class="form-check form-switch">
              <input class="form-check-input" id="upstream-create-enabled" type="checkbox" name="enabled" checked />
              <label class="form-check-label" for="upstream-create-enabled">{{ui .Lang "enabled"}}</label>
            </div>
          </div>
        </div>
        <label>{{ui .Lang "base_url"}}<input name="base_url" placeholder="https://api.openai.com" required /></label>
        <label>{{t .Lang "上游模型（逗号分隔）" "Upstream models (comma separated)"}}<input name="models" placeholder="gpt-4o-mini,gpt-4.1-mini" /></label>
        <label>{{t .Lang "API Key（openai_compat / anthropic / gemini 使用）" "API Key (for openai_compat / anthropic / gemini)"}}<input name="api_key" placeholder="sk-..." /></label>
        <div class="inline-actions">
          <button class="btn" type="submit">{{ui .Lang "save_upstream"}}</button>
          <button class="btn ghost" type="button" data-close-modal="upstream-create-modal">{{t .Lang "关闭" "Close"}}</button>
        </div>
      </form>
    </div>
  </div>
  {{range $idx, $item := .Upstreams}}
  <div id="upstream-update-modal-{{$idx}}" class="opc-modal" data-modal>
    <div class="opc-modal-backdrop" data-close-modal="upstream-update-modal-{{$idx}}"></div>
    <div class="opc-modal-card">
      <div class="modal-head">
        <h3 class="modal-title">{{t $.Lang "更新上游" "Update upstream"}} {{$item.Name}}</h3>
        <button class="btn ghost mini" type="button" data-close-modal="upstream-update-modal-{{$idx}}">{{t $.Lang "关闭" "Close"}}</button>
      </div>
      <form class="stack" method="post" action="{{withLang "/admin/upstreams/new" $.Lang}}" data-submit-lock="1" data-loading-text="{{ui $.Lang "saving"}}">
        <input type="hidden" name="return_to" value="{{withLang $.Path $.Lang}}" />
        <input type="hidden" name="id" value="{{$item.ID}}" />
        <div class="f3">
          <label>{{ui $.Lang "name"}}<input name="name" value="{{$item.Name}}" required /></label>
          <label>{{ui $.Lang "type"}}
            <select name="type">
              <option value="openai_compat" {{if eq $item.Type "openai_compat"}}selected{{end}}>openai_compat</option>
              <option value="anthropic" {{if eq $item.Type "anthropic"}}selected{{end}}>anthropic</option>
              <option value="gemini" {{if eq $item.Type "gemini"}}selected{{end}}>gemini</option>
              <option value="ollama" {{if eq $item.Type "ollama"}}selected{{end}}>ollama</option>
            </select>
          </label>
          <div class="switch-field">
            <p class="switch-field-title">{{ui $.Lang "enabled"}}</p>
            <div class="form-check form-switch">
              <input class="form-check-input" id="upstream-enabled-{{$idx}}" type="checkbox" name="enabled" {{if $item.Enabled}}checked{{end}} />
              <label class="form-check-label" for="upstream-enabled-{{$idx}}">{{ui $.Lang "enabled"}}</label>
            </div>
          </div>
        </div>
        <label>{{ui $.Lang "base_url"}}<input name="base_url" value="{{$item.BaseURL}}" required /></label>
        <label>{{t $.Lang "上游模型（逗号分隔）" "Upstream models (comma separated)"}}<input name="models" value="{{csv $item.Models}}" placeholder="gpt-4o-mini,gpt-4.1-mini" /></label>
        <label>{{t $.Lang "API Key（留空保持不变）" "API Key (leave empty to keep unchanged)"}}<input name="api_key" placeholder="sk-..." /></label>
        <div class="inline-actions">
          <button class="btn" type="submit">{{ui $.Lang "save_upstream"}}</button>
          <button class="btn ghost mini" type="button" data-close-modal="upstream-update-modal-{{$idx}}">{{t $.Lang "关闭" "Close"}}</button>
        </div>
      </form>
    </div>
  </div>
  <div id="upstream-diagnose-modal-{{$idx}}" class="opc-modal" data-modal>
    <div class="opc-modal-backdrop" data-close-modal="upstream-diagnose-modal-{{$idx}}"></div>
    <div class="opc-modal-card">
      <div class="modal-head">
        <h3 class="modal-title">{{t $.Lang "统一诊断中心" "Unified Diagnose Center"}} {{$item.Name}}</h3>
        <button class="btn ghost mini" type="button" data-close-modal="upstream-diagnose-modal-{{$idx}}">{{t $.Lang "关闭" "Close"}}</button>
      </div>
      <form class="stack stack-block" data-upstream-diagnose-form data-upstream-diagnose-url="{{withLang "/admin/upstreams/diagnose" $.Lang}}">
        <h3 class="stack-title">{{t $.Lang "连通性 + 响应测试" "Connectivity + response diagnose"}}</h3>
        <input type="hidden" name="id" value="{{$item.ID}}" />
        <div class="f2">
          <label>{{t $.Lang "目标模型" "Target model"}}
            <input name="model" list="upstream-model-list-{{$idx}}" {{if gt (len $item.Models) 0}}value="{{index $item.Models 0}}"{{end}} placeholder="qwen-plus" />
            <datalist id="upstream-model-list-{{$idx}}">
              {{range $item.Models}}
              <option value="{{.}}"></option>
              {{end}}
            </datalist>
          </label>
          <label>{{ui $.Lang "enabled"}}<input value="{{boolText $.Lang $item.Enabled}}" disabled /></label>
        </div>
        <label>{{t $.Lang "系统提示词（可选）" "System prompt (optional)"}}<textarea name="system_prompt" placeholder="{{t $.Lang "你是一个简洁的助手。" "You are a concise assistant."}}"></textarea></label>
        <label>{{t $.Lang "用户输入" "User prompt"}}<textarea name="user_prompt" required>请回复“pong”，并说明你当前的模型名。</textarea></label>
        <div class="inline-actions">
          <button class="btn secondary mini" type="button" data-upstream-diagnose-btn data-loading-text="{{t $.Lang "诊断中..." "Diagnosing..."}}">{{t $.Lang "执行诊断" "Run diagnose"}}</button>
          <button class="btn ghost mini" type="button" data-close-modal="upstream-diagnose-modal-{{$idx}}">{{t $.Lang "关闭" "Close"}}</button>
        </div>
        <pre class="prompt-preview-result" data-upstream-diagnose-result>{{t $.Lang "这里会同时显示连通性检查与模型响应测试结果。" "Connectivity and response diagnose output will be shown together."}}</pre>
      </form>
    </div>
  </div>
  {{end}}
  {{end}}

  {{if eq .Active "routes"}}
  <section class="stack">
    <div class="section-head">
      <div>
        <h2 class="section-title">{{ui .Lang "model_routes"}}</h2>
        <p class="section-desc">{{t .Lang "自定义模型路由配置，支持新建/更新/删除。" "Custom model routing with create, update, and delete."}}</p>
      </div>
    </div>
    <div class="table-tools">
      <form class="table-tools" method="get" action="{{withLang .Path .Lang}}">
        <input type="hidden" name="q_upstream" value="{{.FilterUpQ}}" />
        <input type="hidden" name="q_runtime" value="{{.FilterRunQ}}" />
        <input type="hidden" name="up_page_size" value="{{.UpstreamPagination.PageSize}}" />
        <input type="hidden" name="run_page_size" value="{{.RuntimePagination.PageSize}}" />
        <input class="search" name="q_route" value="{{.FilterRouteQ}}" data-filter-input="routes-table" placeholder="{{t .Lang "按别名、策略、目标内容搜索" "Search alias, strategy, target content"}}" />
        <select class="page-size" name="route_page_size">
          <option value="10" {{if eq .RoutePagination.PageSize 10}}selected{{end}}>10 / page</option>
          <option value="12" {{if eq .RoutePagination.PageSize 12}}selected{{end}}>12 / page</option>
          <option value="20" {{if eq .RoutePagination.PageSize 20}}selected{{end}}>20 / page</option>
          <option value="50" {{if eq .RoutePagination.PageSize 50}}selected{{end}}>50 / page</option>
          <option value="100" {{if eq .RoutePagination.PageSize 100}}selected{{end}}>100 / page</option>
        </select>
        <button class="btn secondary" type="submit">{{t .Lang "查询" "Search"}}</button>
      </form>
      <button class="btn" type="button" data-open-modal="route-create-modal">{{t .Lang "新建路由" "New Route"}}</button>
    </div>
    <div class="table-wrap">
      <table id="routes-table">
        <thead><tr><th>{{ui .Lang "model_alias"}}</th><th>{{ui .Lang "strategy"}}</th><th>{{ui .Lang "enabled"}}</th><th>{{ui .Lang "targets"}}</th><th>{{ui .Lang "action"}}</th></tr></thead>
        <tbody>
          {{range $idx, $route := .Routes}}
          <tr>
            <td><span class="mono">{{$route.Alias}}</span></td>
            <td><span class="chip {{statusTone $route.Strategy}}">{{$route.Strategy}}</span></td>
            <td><span class="chip {{statusTone (boolLabel $route.Enabled)}}">{{boolText $.Lang $route.Enabled}}</span></td>
            <td>{{routeTargets $route.Targets}}</td>
            <td>
              <div class="inline-actions">
                <button class="btn secondary mini" type="button" data-open-modal="route-update-modal-{{$idx}}">{{t $.Lang "更新" "Update"}}</button>
                <form method="post" action="{{withLang "/admin/routes/delete" $.Lang}}" data-submit-lock="1" data-loading-text="{{ui $.Lang "deleting"}}" data-confirm="{{ui $.Lang "delete_this_route"}}">
                  <input type="hidden" name="return_to" value="{{withLang $.Path $.Lang}}" />
                  <input type="hidden" name="alias" value="{{$route.Alias}}" />
                  <button class="btn secondary mini" type="submit">{{ui $.Lang "delete"}}</button>
                </form>
              </div>
            </td>
          </tr>
          {{end}}
        </tbody>
      </table>
    </div>
    <div class="pager">
      <p class="pager-info">{{t .Lang "第" "Page"}} {{.RoutePagination.Page}} / {{.RoutePagination.TotalPages}} , {{t .Lang "共" "Total"}} {{.RoutePagination.Total}} {{t .Lang "条" "rows"}}</p>
      <div class="inline-actions">
        {{if .RoutePagination.HasPrev}}<a class="btn secondary" href="{{.RoutePagination.PrevURL}}">{{t .Lang "上一页" "Previous"}}</a>{{end}}
        {{if .RoutePagination.HasNext}}<a class="btn secondary" href="{{.RoutePagination.NextURL}}">{{t .Lang "下一页" "Next"}}</a>{{end}}
        <form class="page-jump" method="get" action="{{withLang .Path .Lang}}">
          <input type="hidden" name="q_upstream" value="{{.FilterUpQ}}" />
          <input type="hidden" name="q_route" value="{{.FilterRouteQ}}" />
          <input type="hidden" name="q_runtime" value="{{.FilterRunQ}}" />
          <input type="hidden" name="up_page_size" value="{{.UpstreamPagination.PageSize}}" />
          <input type="hidden" name="run_page_size" value="{{.RuntimePagination.PageSize}}" />
          <input type="hidden" name="route_page_size" value="{{.RoutePagination.PageSize}}" />
          <input class="jump-input" type="number" min="1" max="{{.RoutePagination.TotalPages}}" name="route_page" value="{{.RoutePagination.Page}}" />
          <button class="btn secondary" type="submit">{{t .Lang "跳转" "Go"}}</button>
        </form>
      </div>
    </div>

    <div class="section-head">
      <div>
        <h3 class="stack-title">{{ui .Lang "routing_runtime_stats"}}</h3>
      </div>
    </div>
    <form class="table-tools" method="get" action="{{withLang .Path .Lang}}">
      <input type="hidden" name="q_upstream" value="{{.FilterUpQ}}" />
      <input type="hidden" name="q_route" value="{{.FilterRouteQ}}" />
      <input type="hidden" name="up_page_size" value="{{.UpstreamPagination.PageSize}}" />
      <input type="hidden" name="route_page_size" value="{{.RoutePagination.PageSize}}" />
      <input class="search" name="q_runtime" value="{{.FilterRunQ}}" data-filter-input="runtime-table" placeholder="{{t .Lang "按模型别名、上游 ID、错误内容搜索" "Search model alias, upstream id or errors"}}" />
      <select class="page-size" name="run_page_size">
        <option value="10" {{if eq .RuntimePagination.PageSize 10}}selected{{end}}>10 / page</option>
        <option value="12" {{if eq .RuntimePagination.PageSize 12}}selected{{end}}>12 / page</option>
        <option value="20" {{if eq .RuntimePagination.PageSize 20}}selected{{end}}>20 / page</option>
        <option value="50" {{if eq .RuntimePagination.PageSize 50}}selected{{end}}>50 / page</option>
        <option value="100" {{if eq .RuntimePagination.PageSize 100}}selected{{end}}>100 / page</option>
      </select>
      <button class="btn secondary" type="submit">{{t .Lang "查询" "Search"}}</button>
    </form>
    <div class="table-wrap">
      <table id="runtime-table">
        <thead><tr><th>{{ui .Lang "model_alias"}}</th><th>{{ui .Lang "upstream_id"}}</th><th>{{t .Lang "成功" "Success"}}</th><th>{{t .Lang "失败" "Failure"}}</th><th>{{t .Lang "最近延迟" "Last Latency"}}</th><th>{{ui .Lang "last_error"}}</th><th>{{ui .Lang "last_seen"}}</th></tr></thead>
        <tbody>
          {{range .RuntimeStats}}
          <tr>
            <td><span class="mono">{{.ModelAlias}}</span></td>
            <td><span class="mono">{{.UpstreamID}}</span></td>
            <td>{{.SuccessCount}}</td>
            <td>{{.FailureCount}}</td>
            <td>{{fmtLatency .LastLatencyMS}}</td>
            <td>{{.LastError}}</td>
            <td>{{fmtTime .LastSeenAt}}</td>
          </tr>
          {{end}}
        </tbody>
      </table>
    </div>
    <div class="pager">
      <p class="pager-info">{{t .Lang "第" "Page"}} {{.RuntimePagination.Page}} / {{.RuntimePagination.TotalPages}} , {{t .Lang "共" "Total"}} {{.RuntimePagination.Total}} {{t .Lang "条" "rows"}}</p>
      <div class="inline-actions">
        {{if .RuntimePagination.HasPrev}}<a class="btn secondary" href="{{.RuntimePagination.PrevURL}}">{{t .Lang "上一页" "Previous"}}</a>{{end}}
        {{if .RuntimePagination.HasNext}}<a class="btn secondary" href="{{.RuntimePagination.NextURL}}">{{t .Lang "下一页" "Next"}}</a>{{end}}
        <form class="page-jump" method="get" action="{{withLang .Path .Lang}}">
          <input type="hidden" name="q_upstream" value="{{.FilterUpQ}}" />
          <input type="hidden" name="q_route" value="{{.FilterRouteQ}}" />
          <input type="hidden" name="q_runtime" value="{{.FilterRunQ}}" />
          <input type="hidden" name="up_page_size" value="{{.UpstreamPagination.PageSize}}" />
          <input type="hidden" name="route_page_size" value="{{.RoutePagination.PageSize}}" />
          <input type="hidden" name="run_page_size" value="{{.RuntimePagination.PageSize}}" />
          <input class="jump-input" type="number" min="1" max="{{.RuntimePagination.TotalPages}}" name="run_page" value="{{.RuntimePagination.Page}}" />
          <button class="btn secondary" type="submit">{{t .Lang "跳转" "Go"}}</button>
        </form>
      </div>
    </div>
  </section>
  <div id="route-create-modal" class="opc-modal" data-modal>
    <div class="opc-modal-backdrop" data-close-modal="route-create-modal"></div>
    <div class="opc-modal-card">
      <div class="modal-head">
        <h3 class="modal-title">{{t .Lang "新建路由" "New Route"}}</h3>
        <button class="btn ghost mini" type="button" data-close-modal="route-create-modal">{{t .Lang "关闭" "Close"}}</button>
      </div>
      <form class="stack" method="post" action="{{withLang "/admin/routes/new" .Lang}}" data-submit-lock="1" data-loading-text="{{ui .Lang "saving"}}" data-require-targets="1" data-route-targets-required-text="{{ui .Lang "at_least_one_route_target"}}" data-route-targets-form="1" data-upstream-model-map='{{toJSON $.UpstreamModelMap}}'>
        <input type="hidden" name="return_to" value="{{withLang .Path .Lang}}" />
        <div class="f3">
          <label>{{ui .Lang "model_alias"}}<input name="alias" placeholder="gpt-4o-mini" required /></label>
          <label>{{ui .Lang "strategy"}}
            <select name="strategy"><option value="fallback">{{t .Lang "故障回退" "fallback"}}</option><option value="weighted">{{t .Lang "权重分流" "weighted"}}</option></select>
          </label>
          <div class="switch-field">
            <p class="switch-field-title">{{ui .Lang "enabled"}}</p>
            <div class="form-check form-switch">
              <input class="form-check-input" id="route-create-enabled" type="checkbox" name="enabled" checked />
              <label class="form-check-label" for="route-create-enabled">{{t .Lang "启用该路由" "Enable this route"}}</label>
            </div>
          </div>
        </div>
        <div class="stack-block">
          <h3 class="stack-title">{{t .Lang "模型目标（主/副模型）" "Route targets (primary/fallback)"}}</h3>
          {{range $pos := targetRows 8}}
          <div class="target-row-box {{if gt $pos 1}}is-hidden{{end}}" data-route-target-row data-route-target-index="{{$pos}}">
            <div class="target-row">
              <label>{{if eq $pos 1}}{{t $.Lang "主目标上游" "Primary upstream"}}{{else if eq $pos 2}}{{t $.Lang "副目标上游" "Fallback upstream"}}{{else}}{{t $.Lang "额外目标上游" "Extra upstream"}}{{end}}
                <select name="target_upstream_id_{{$pos}}">
                  <option value="">{{t $.Lang "请选择上游" "Select upstream"}}</option>
                  {{range $.UpstreamOptions}}
                  <option value="{{.ID}}">{{.Name}} ({{.ID}})</option>
                  {{end}}
                </select>
              </label>
              <label>{{t $.Lang "目标模型" "Target model"}}
                <select name="target_model_{{$pos}}" data-empty-label="{{t $.Lang "留空则使用路由别名模型" "Use route alias model when empty"}}">
                  <option value="">{{t $.Lang "留空则使用路由别名模型" "Use route alias model when empty"}}</option>
                </select>
              </label>
              <label>{{t $.Lang "权重" "Weight"}}<input name="target_weight_{{$pos}}" value="100" data-default-value="100" /></label>
              <label>{{t $.Lang "优先级" "Priority"}}<input name="target_priority_{{$pos}}" value="{{$pos}}" data-default-value="{{$pos}}" /></label>
            </div>
            <div class="inline-actions">
              <button class="btn ghost mini" type="button" data-route-target-remove>{{t $.Lang "移除该目标" "Remove target"}}</button>
            </div>
          </div>
          {{end}}
          <div class="target-toolbar">
            <button class="btn secondary mini" type="button" data-route-target-add>{{t .Lang "新增模型目标" "Add route target"}}</button>
            <p class="hint">{{t .Lang "默认仅 1 条，可按需新增；至少保留 1 条有效目标。" "Start with one row, add more when needed; keep at least one valid target."}}</p>
          </div>
        </div>
        <div class="inline-actions">
          <button class="btn" type="submit">{{ui .Lang "save_route"}}</button>
          <button class="btn ghost" type="button" data-close-modal="route-create-modal">{{t .Lang "关闭" "Close"}}</button>
        </div>
      </form>
    </div>
  </div>
  {{range $idx, $route := .Routes}}
  <div id="route-update-modal-{{$idx}}" class="opc-modal" data-modal>
    <div class="opc-modal-backdrop" data-close-modal="route-update-modal-{{$idx}}"></div>
    <div class="opc-modal-card">
      <div class="modal-head">
        <h3 class="modal-title">{{t $.Lang "更新路由" "Update route"}} {{$route.Alias}}</h3>
        <button class="btn ghost mini" type="button" data-close-modal="route-update-modal-{{$idx}}">{{t $.Lang "关闭" "Close"}}</button>
      </div>
      <form class="stack" method="post" action="{{withLang "/admin/routes/new" $.Lang}}" data-submit-lock="1" data-loading-text="{{ui $.Lang "saving"}}" data-require-targets="1" data-route-targets-required-text="{{ui $.Lang "at_least_one_route_target"}}" data-route-targets-form="1" data-upstream-model-map='{{toJSON $.UpstreamModelMap}}'>
        <input type="hidden" name="return_to" value="{{withLang $.Path $.Lang}}" />
        <div class="f3">
          <label>{{ui $.Lang "model_alias"}}<input name="alias" value="{{$route.Alias}}" required /></label>
          <label>{{ui $.Lang "strategy"}}
            <select name="strategy"><option value="fallback" {{if eq $route.Strategy "fallback"}}selected{{end}}>{{t $.Lang "故障回退" "fallback"}}</option><option value="weighted" {{if eq $route.Strategy "weighted"}}selected{{end}}>{{t $.Lang "权重分流" "weighted"}}</option></select>
          </label>
          <div class="switch-field">
            <p class="switch-field-title">{{ui $.Lang "enabled"}}</p>
            <div class="form-check form-switch">
              <input class="form-check-input" id="route-enabled-{{$idx}}" type="checkbox" name="enabled" {{if $route.Enabled}}checked{{end}} />
              <label class="form-check-label" for="route-enabled-{{$idx}}">{{t $.Lang "启用该路由" "Enable this route"}}</label>
            </div>
          </div>
        </div>
        <div class="stack-block">
          <h3 class="stack-title">{{t $.Lang "模型目标（主/副模型）" "Route targets (primary/fallback)"}}</h3>
          {{range $pos := targetRows 8}}
          {{$target := routeTargetFormItem $route.Targets $pos}}
          <div class="target-row-box {{if and (gt $pos 1) (gt $pos (len $route.Targets))}}is-hidden{{end}}" data-route-target-row data-route-target-index="{{$pos}}">
            <div class="target-row">
              <label>{{if eq $pos 1}}{{t $.Lang "主目标上游" "Primary upstream"}}{{else if eq $pos 2}}{{t $.Lang "副目标上游" "Fallback upstream"}}{{else}}{{t $.Lang "额外目标上游" "Extra upstream"}}{{end}}
                <select name="target_upstream_id_{{$pos}}">
                  <option value="">{{t $.Lang "请选择上游" "Select upstream"}}</option>
                  {{range $.UpstreamOptions}}
                  <option value="{{.ID}}" {{if eq .ID $target.UpstreamID}}selected{{end}}>{{.Name}} ({{.ID}})</option>
                  {{end}}
                </select>
              </label>
              <label>{{t $.Lang "目标模型" "Target model"}}
                <select name="target_model_{{$pos}}" data-empty-label="{{t $.Lang "留空则使用路由别名模型" "Use route alias model when empty"}}" data-current-model="{{$target.Model}}">
                  <option value="">{{t $.Lang "留空则使用路由别名模型" "Use route alias model when empty"}}</option>
                  {{if ne $target.Model ""}}<option value="{{$target.Model}}" selected>{{$target.Model}}</option>{{end}}
                </select>
              </label>
              <label>{{t $.Lang "权重" "Weight"}}<input name="target_weight_{{$pos}}" value="{{$target.Weight}}" data-default-value="100" /></label>
              <label>{{t $.Lang "优先级" "Priority"}}<input name="target_priority_{{$pos}}" value="{{$target.Priority}}" data-default-value="{{$pos}}" /></label>
            </div>
            <div class="inline-actions">
              <button class="btn ghost mini" type="button" data-route-target-remove>{{t $.Lang "移除该目标" "Remove target"}}</button>
            </div>
          </div>
          {{end}}
          <div class="target-toolbar">
            <button class="btn secondary mini" type="button" data-route-target-add>{{t $.Lang "新增模型目标" "Add route target"}}</button>
            <p class="hint">{{t $.Lang "默认展示已配置目标，可继续追加。" "Existing targets are shown by default; add more if needed."}}</p>
          </div>
        </div>
        <div class="inline-actions">
          <button class="btn" type="submit">{{ui $.Lang "save_route"}}</button>
          <button class="btn ghost mini" type="button" data-close-modal="route-update-modal-{{$idx}}">{{t $.Lang "关闭" "Close"}}</button>
        </div>
      </form>
    </div>
  </div>
  {{end}}
  {{end}}
</article>
{{end}}
`))

func (h *handler) adminHome(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	target := withLangPath("/admin/users", strings.TrimSpace(q.Get("lang")))
	sep := "?"
	if strings.Contains(target, "?") {
		sep = "&"
	}
	if msg := strings.TrimSpace(q.Get("message")); msg != "" {
		target += sep + "message=" + url.QueryEscape(msg)
	} else if err := strings.TrimSpace(q.Get("error")); err != "" {
		target += sep + "error=" + url.QueryEscape(err)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (h *handler) adminUsersPage(w http.ResponseWriter, r *http.Request) {
	base, ok := h.adminBasePageView(w, r, "users")
	if !ok {
		return
	}
	page, pageSize := parseAdminPage(r, 20, 100)
	view := adminUsersPageView{
		adminBaseView: base,
		FilterQ:       strings.TrimSpace(r.URL.Query().Get("q")),
		PlanMap:       map[string]billing.PlanConfig{},
	}
	allUsers := make([]user.TenantUser, 0)
	if users, err := h.userService.ListUsers(r.Context(), base.Claims); err == nil {
		allUsers = users
	}
	view.TotalUsers = len(allUsers)
	for _, item := range allUsers {
		if strings.EqualFold(item.User.Status, "active") {
			view.ActiveUsers++
		}
		if strings.EqualFold(item.Role, "admin") {
			view.AdminUsers++
		}
	}

	membershipByUser := make(map[string]billing.Membership)
	policyByUser := make(map[string]billing.UsagePolicy)
	if h.billing != nil {
		view.PlanMap = map[string]billing.PlanConfig{}
		if plans, err := h.billing.ListPlanConfigs(r.Context()); err == nil {
			view.PlanConfigs = plans
			for _, cfg := range plans {
				view.PlanMap[strings.TrimSpace(cfg.PlanID)] = cfg
			}
		}
		if memberships, err := h.billing.ListMemberships(r.Context()); err == nil {
			for _, m := range memberships {
				userID := strings.TrimSpace(m.UserID)
				if userID == "" {
					continue
				}
				membershipByUser[userID] = m
			}
		}
		for _, item := range allUsers {
			userID := strings.TrimSpace(item.User.ID)
			if userID == "" {
				continue
			}
			policy, err := h.billing.ResolveUsagePolicy(r.Context(), userID)
			if err != nil {
				policyByUser[userID] = billing.FreeUsagePolicy()
				continue
			}
			policyByUser[userID] = policy
			if _, ok := membershipByUser[userID]; !ok {
				membershipByUser[userID] = billing.Membership{
					UserID:    userID,
					PlanID:    policy.PlanID,
					Status:    "free",
					UpdatedAt: time.Now().UTC(),
				}
			}
		}
	}

	filteredUsers := allUsers
	if q := strings.ToLower(view.FilterQ); q != "" {
		filteredUsers = make([]user.TenantUser, 0, len(allUsers))
		for _, item := range allUsers {
			membership := membershipByUser[item.User.ID]
			policy := policyByUser[item.User.ID]
			if adminUserMatchesQuery(item, membership, policy, q) {
				filteredUsers = append(filteredUsers, item)
			}
		}
	}

	total := len(filteredUsers)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	pagedUsers := filteredUsers[start:end]
	view.Rows = make([]adminUserRow, 0, len(pagedUsers))
	for _, item := range pagedUsers {
		row := adminUserRow{User: item}
		row.Membership = membershipByUser[item.User.ID]
		row.Policy = policyByUser[item.User.ID]
		if strings.TrimSpace(row.Membership.PlanID) == "" {
			row.Membership = billing.Membership{UserID: item.User.ID, PlanID: "free", Status: "free"}
		}
		if strings.TrimSpace(row.Policy.PlanID) == "" {
			row.Policy = billing.FreeUsagePolicy()
		}
		view.Rows = append(view.Rows, row)
	}
	view.Pagination = buildAdminPagination("/admin/users", base.Lang, r.URL.Query(), page, pageSize, total)
	_ = adminUsersPageTemplate.Execute(w, view)
}

func (h *handler) adminOrdersPage(w http.ResponseWriter, r *http.Request) {
	base, ok := h.adminBasePageView(w, r, "orders")
	if !ok {
		return
	}
	page, pageSize := parseAdminPage(r, 20, 100)
	view := adminOrdersPageView{
		adminBaseView: base,
		FilterID:      strings.TrimSpace(r.URL.Query().Get("id")),
		FilterUser:    strings.TrimSpace(r.URL.Query().Get("user_id")),
		FilterType:    strings.TrimSpace(r.URL.Query().Get("type")),
		FilterChan:    strings.TrimSpace(r.URL.Query().Get("channel")),
		FilterStat:    strings.TrimSpace(r.URL.Query().Get("status")),
	}
	total := 0
	if h.billing != nil {
		if all, err := h.billing.ListOrdersAdmin(r.Context(), billing.OrderQuery{Limit: 1, Offset: 0}); err == nil {
			view.TotalOrders = all.Total
		}
		res, err := h.billing.ListOrdersAdmin(r.Context(), billing.OrderQuery{
			ID:      view.FilterID,
			UserID:  view.FilterUser,
			Type:    view.FilterType,
			Status:  view.FilterStat,
			Channel: view.FilterChan,
			Limit:   pageSize,
			Offset:  (page - 1) * pageSize,
		})
		if err == nil {
			view.Orders = res.Data
			total = res.Total
		}
	}
	view.Pagination = buildAdminPagination("/admin/orders", base.Lang, r.URL.Query(), page, pageSize, total)
	_ = adminOrdersPageTemplate.Execute(w, view)
}

func (h *handler) adminPromptsPage(w http.ResponseWriter, r *http.Request) {
	base, ok := h.adminBasePageView(w, r, "prompts")
	if !ok {
		return
	}
	page, pageSize := parseAdminPageByKeys(r, "page", "page_size", 20, 100)
	view := adminPromptsPageView{
		adminBaseView: base,
		FilterQ:       strings.TrimSpace(r.URL.Query().Get("q")),
		ModelOptions:  []string{},
	}
	view.Pagination = buildAdminPaginationByKeys("/admin/prompts", base.Lang, r.URL.Query(), "page", "page_size", page, pageSize, 0)
	if h.upstream != nil {
		if ups, err := h.upstream.ListUpstreams(); err == nil {
			view.ModelOptions = mergeModelOptions(view.ModelOptions, upstreamModelsOnly(ups))
		}
		if routes, err := h.upstream.ListRoutes(); err == nil {
			view.ModelOptions = mergeModelOptions(view.ModelOptions, routeAliasOnly(routes))
		}
	}
	if h.promptAPI != nil {
		if defaultAPIs, err := h.promptAPI.ListDefaultAPIs(r.Context()); err == nil {
			view.DefaultAPIs = defaultAPIs
			for _, cfg := range defaultAPIs {
				view.ModelOptions = mergeModelOptions(view.ModelOptions, cfg.AllowedModels)
				if strings.TrimSpace(cfg.DefaultModel) != "" {
					view.ModelOptions = mergeModelOptions(view.ModelOptions, []string{cfg.DefaultModel})
				}
				for _, m := range cfg.ModelIDMappings {
					if strings.TrimSpace(m.Model) != "" {
						view.ModelOptions = mergeModelOptions(view.ModelOptions, []string{m.Model})
					}
				}
			}
		}
		items := make([]promptapi.Definition, 0, 200)
		offset := 0
		for {
			page, err := h.promptAPI.List(r.Context(), promptapi.ListQuery{Limit: 200, Offset: offset})
			if err != nil {
				break
			}
			items = append(items, page.Data...)
			offset += len(page.Data)
			if len(page.Data) == 0 || offset >= page.Total {
				break
			}
		}
		if q := strings.ToLower(view.FilterQ); q != "" {
			filtered := make([]promptapi.Definition, 0, len(items))
			for _, item := range items {
				if adminPromptMatchesQuery(item, q) {
					filtered = append(filtered, item)
				}
			}
			items = filtered
		}
		view.TotalPrompts = len(items)
		for _, item := range items {
			if strings.EqualFold(item.Status, "published") {
				view.Published++
			} else {
				view.Unpublished++
			}
		}
		start := (page - 1) * pageSize
		if start > len(items) {
			start = len(items)
		}
		end := start + pageSize
		if end > len(items) {
			end = len(items)
		}
		view.PromptAPIs = items[start:end]
		view.Pagination = buildAdminPaginationByKeys("/admin/prompts", base.Lang, r.URL.Query(), "page", "page_size", page, pageSize, len(items))
	}
	_ = adminPromptsPageTemplate.Execute(w, view)
}

func (h *handler) adminUpstreamsPage(w http.ResponseWriter, r *http.Request) {
	active := "upstreams"
	if strings.EqualFold(strings.TrimSpace(r.URL.Path), "/admin/routes") {
		active = "routes"
	}
	base, ok := h.adminBasePageView(w, r, active)
	if !ok {
		return
	}
	path := r.URL.Path
	upPage, upSize := parseAdminPageByKeys(r, "up_page", "up_page_size", 12, 100)
	routePage, routeSize := parseAdminPageByKeys(r, "route_page", "route_page_size", 12, 100)
	runPage, runSize := parseAdminPageByKeys(r, "run_page", "run_page_size", 12, 100)
	view := adminUpstreamsPageView{
		adminBaseView:    base,
		FilterUpQ:        strings.TrimSpace(r.URL.Query().Get("q_upstream")),
		FilterRouteQ:     strings.TrimSpace(r.URL.Query().Get("q_route")),
		FilterRunQ:       strings.TrimSpace(r.URL.Query().Get("q_runtime")),
		UpstreamModelMap: map[string][]string{},
	}
	view.UpstreamPagination = buildAdminPaginationByKeys(path, base.Lang, r.URL.Query(), "up_page", "up_page_size", upPage, upSize, 0)
	view.RoutePagination = buildAdminPaginationByKeys(path, base.Lang, r.URL.Query(), "route_page", "route_page_size", routePage, routeSize, 0)
	view.RuntimePagination = buildAdminPaginationByKeys(path, base.Lang, r.URL.Query(), "run_page", "run_page_size", runPage, runSize, 0)
	if h.upstream != nil {
		if items, err := h.upstream.ListUpstreams(); err == nil {
			view.UpstreamOptions = items
			view.UpstreamModelMap = upstreamModelsByID(items)
			if q := strings.ToLower(view.FilterUpQ); q != "" {
				filtered := make([]upstream.Upstream, 0, len(items))
				for _, item := range items {
					if adminUpstreamMatchesQuery(item, q) {
						filtered = append(filtered, item)
					}
				}
				items = filtered
			}
			total := len(items)
			start := (upPage - 1) * upSize
			if start > total {
				start = total
			}
			end := start + upSize
			if end > total {
				end = total
			}
			view.Upstreams = items[start:end]
			view.UpstreamPagination = buildAdminPaginationByKeys(path, base.Lang, r.URL.Query(), "up_page", "up_page_size", upPage, upSize, total)
		}
		if routes, err := h.upstream.ListRoutes(); err == nil {
			if q := strings.ToLower(view.FilterRouteQ); q != "" {
				filtered := make([]upstream.ModelRoute, 0, len(routes))
				for _, route := range routes {
					if adminRouteMatchesQuery(route, q) {
						filtered = append(filtered, route)
					}
				}
				routes = filtered
			}
			total := len(routes)
			start := (routePage - 1) * routeSize
			if start > total {
				start = total
			}
			end := start + routeSize
			if end > total {
				end = total
			}
			view.Routes = routes[start:end]
			view.RoutePagination = buildAdminPaginationByKeys(path, base.Lang, r.URL.Query(), "route_page", "route_page_size", routePage, routeSize, total)
		}
		stats := h.upstream.RuntimeStats()
		if q := strings.ToLower(view.FilterRunQ); q != "" {
			filtered := make([]upstream.RuntimeStat, 0, len(stats))
			for _, item := range stats {
				if adminRuntimeMatchesQuery(item, q) {
					filtered = append(filtered, item)
				}
			}
			stats = filtered
		}
		total := len(stats)
		start := (runPage - 1) * runSize
		if start > total {
			start = total
		}
		end := start + runSize
		if end > total {
			end = total
		}
		view.RuntimeStats = stats[start:end]
		view.RuntimePagination = buildAdminPaginationByKeys(path, base.Lang, r.URL.Query(), "run_page", "run_page_size", runPage, runSize, total)
	}
	_ = adminUpstreamsPageTemplate.Execute(w, view)
}

func mergeModelOptions(base, extras []string) []string {
	seen := make(map[string]struct{}, len(base)+len(extras))
	out := make([]string, 0, len(base)+len(extras))
	for _, item := range append(base, extras...) {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func upstreamModelsOnly(items []upstream.Upstream) []string {
	out := make([]string, 0, len(items)*2)
	for _, up := range items {
		for _, model := range up.Models {
			v := strings.TrimSpace(model)
			if v == "" {
				continue
			}
			out = append(out, v)
		}
	}
	return out
}

func upstreamModelsByID(items []upstream.Upstream) map[string][]string {
	out := make(map[string][]string, len(items))
	for _, up := range items {
		key := strings.TrimSpace(up.ID)
		if key == "" {
			continue
		}
		models := normalizeTextList(up.Models)
		out[key] = models
	}
	return out
}

func normalizeTextList(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		v := strings.TrimSpace(item)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func routeAliasOnly(items []upstream.ModelRoute) []string {
	out := make([]string, 0, len(items))
	for _, route := range items {
		alias := strings.TrimSpace(route.Alias)
		if alias == "" {
			continue
		}
		out = append(out, alias)
	}
	return out
}

func parseAdminPage(r *http.Request, defaultSize, maxSize int) (int, int) {
	return parseAdminPageByKeys(r, "page", "page_size", defaultSize, maxSize)
}

func parseAdminPageByKeys(r *http.Request, pageKey, pageSizeKey string, defaultSize, maxSize int) (int, int) {
	page := 1
	pageSize := defaultSize
	if r == nil {
		return page, pageSize
	}
	if v := strings.TrimSpace(r.URL.Query().Get(pageKey)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	if v := strings.TrimSpace(r.URL.Query().Get(pageSizeKey)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			pageSize = n
		}
	}
	if pageSize > maxSize {
		pageSize = maxSize
	}
	return page, pageSize
}

func buildAdminPagination(path, lang string, query url.Values, page, pageSize, total int) adminPagination {
	return buildAdminPaginationByKeys(path, lang, query, "page", "page_size", page, pageSize, total)
}

func buildAdminPaginationByKeys(path, lang string, query url.Values, pageKey, pageSizeKey string, page, pageSize, total int) adminPagination {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	totalPages := 1
	if total > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	if page > totalPages {
		page = totalPages
	}

	base := cloneURLValues(query)
	base.Del("lang")
	base.Set(pageSizeKey, strconv.Itoa(pageSize))

	p := adminPagination{
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: totalPages,
		HasPrev:    page > 1,
		HasNext:    page < totalPages,
	}
	if p.HasPrev {
		prev := cloneURLValues(base)
		prev.Set(pageKey, strconv.Itoa(page-1))
		p.PrevURL = withLangValues(path, lang, prev)
	}
	if p.HasNext {
		next := cloneURLValues(base)
		next.Set(pageKey, strconv.Itoa(page+1))
		p.NextURL = withLangValues(path, lang, next)
	}
	return p
}

func cloneURLValues(src url.Values) url.Values {
	out := make(url.Values, len(src))
	for k, vals := range src {
		cp := make([]string, len(vals))
		copy(cp, vals)
		out[k] = cp
	}
	return out
}

func withLangValues(path, lang string, values url.Values) string {
	target := withLangPath(path, lang)
	if len(values) == 0 {
		return target
	}
	encoded := values.Encode()
	if encoded == "" {
		return target
	}
	sep := "?"
	if strings.Contains(target, "?") {
		sep = "&"
	}
	return target + sep + encoded
}

func adminUserMatchesQuery(item user.TenantUser, membership billing.Membership, policy billing.UsagePolicy, q string) bool {
	if q == "" {
		return true
	}
	fields := []string{
		item.User.ID,
		item.User.Email,
		item.Role,
		item.User.Status,
		membership.PlanID,
		membership.Status,
		fmtTimeForSearch(membership.PeriodEnd),
		strconv.Itoa(policy.FiveHourTokenLimit),
		strconv.Itoa(policy.MonthlyTokenLimit),
	}
	return adminContainsAny(fields, q)
}

func fmtTimeForSearch(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339)
}

func adminPromptMatchesQuery(item promptapi.Definition, q string) bool {
	if q == "" {
		return true
	}
	fields := []string{
		item.Slug,
		item.Name,
		item.Description,
		item.Model,
		item.InputSchema,
		item.OutputSchema,
		item.ExampleInput,
		item.ExampleOutput,
		item.Status,
		strconv.Itoa(item.Version),
		item.SystemPrompt,
		item.UserTemplate,
		strings.Join(item.Variables, ","),
	}
	return adminContainsAny(fields, q)
}

func adminUpstreamMatchesQuery(item upstream.Upstream, q string) bool {
	if q == "" {
		return true
	}
	fields := []string{
		item.ID,
		item.Name,
		item.Type,
		item.BaseURL,
		strings.Join(item.Models, ","),
		item.LastHealthStatus,
		item.LastHealthError,
		strconv.Itoa(item.LastHealthHTTPCode),
		item.LastHealthEndpoint,
		item.LastHealthResponse,
	}
	return adminContainsAny(fields, q)
}

func adminRouteMatchesQuery(route upstream.ModelRoute, q string) bool {
	if q == "" {
		return true
	}
	fields := []string{
		route.Alias,
		route.Strategy,
		routeTargetsForSearch(route.Targets),
	}
	return adminContainsAny(fields, q)
}

func adminRuntimeMatchesQuery(item upstream.RuntimeStat, q string) bool {
	if q == "" {
		return true
	}
	fields := []string{
		item.ModelAlias,
		item.UpstreamID,
		item.LastError,
		strconv.FormatInt(item.SuccessCount, 10),
		strconv.FormatInt(item.FailureCount, 10),
	}
	return adminContainsAny(fields, q)
}

func routeTargetsForSearch(items []upstream.RouteTarget) string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, fmt.Sprintf("%s:%s:%d:%d:%t", item.UpstreamID, item.Model, item.Weight, item.Priority, item.Enabled))
	}
	return strings.Join(out, ",")
}

func adminContainsAny(fields []string, q string) bool {
	if q == "" {
		return true
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(strings.TrimSpace(field)), q) {
			return true
		}
	}
	return false
}

func (h *handler) adminUpstreamCreate(w http.ResponseWriter, r *http.Request) {
	if h.upstream == nil {
		h.redirectAdminWithError(w, r, "save_upstream_failed: upstream service unavailable")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.redirectAdminWithError(w, r, "invalid form data")
		return
	}
	enabled := r.FormValue("enabled") == "on"
	saved, err := h.upstream.UpsertUpstream(upstream.UpsertUpstreamRequest{
		ID:      strings.TrimSpace(r.FormValue("id")),
		Name:    strings.TrimSpace(r.FormValue("name")),
		Type:    strings.TrimSpace(r.FormValue("type")),
		BaseURL: strings.TrimSpace(r.FormValue("base_url")),
		APIKey:  strings.TrimSpace(r.FormValue("api_key")),
		Models:  splitCommaSeparated(r.FormValue("models")),
		Enabled: enabled,
	})
	if err != nil {
		h.redirectAdminWithError(w, r, "save_upstream_failed: "+err.Error())
		return
	}
	h.redirectAdminWithMessage(w, r, "upstream_saved:"+saved.ID)
}

func (h *handler) adminUpstreamDelete(w http.ResponseWriter, r *http.Request) {
	if h.upstream == nil {
		h.redirectAdminWithError(w, r, "delete_upstream_failed: upstream service unavailable")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.redirectAdminWithError(w, r, "invalid form data")
		return
	}
	ids := parseBatchValues(r.FormValue("id"))
	if len(ids) == 0 {
		h.redirectAdminWithError(w, r, "missing upstream id")
		return
	}
	for _, id := range ids {
		if err := h.upstream.DeleteUpstream(id); err != nil {
			h.redirectAdminWithError(w, r, "delete_upstream_failed: "+id+": "+err.Error())
			return
		}
	}
	if len(ids) == 1 {
		h.redirectAdminWithMessage(w, r, "upstream_deleted:"+ids[0])
		return
	}
	h.redirectAdminWithMessage(w, r, "upstream_deleted:"+strconv.Itoa(len(ids)))
}

func (h *handler) adminRouteCreate(w http.ResponseWriter, r *http.Request) {
	if h.upstream == nil {
		h.redirectAdminWithError(w, r, "save_route_failed: upstream service unavailable")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.redirectAdminWithError(w, r, "invalid form data")
		return
	}
	targets, err := parseRouteTargetsFromForm(r)
	if err != nil {
		h.redirectAdminWithError(w, r, "invalid targets: "+err.Error())
		return
	}
	enabled := r.FormValue("enabled") == "on"
	saved, err := h.upstream.UpsertRoute(upstream.UpsertRouteRequest{
		Alias:    strings.TrimSpace(r.FormValue("alias")),
		Strategy: strings.TrimSpace(r.FormValue("strategy")),
		Enabled:  enabled,
		Targets:  targets,
	})
	if err != nil {
		h.redirectAdminWithError(w, r, "save_route_failed: "+err.Error())
		return
	}
	h.redirectAdminWithMessage(w, r, "route_saved:"+saved.Alias)
}

func (h *handler) adminUpstreamToggleSubmit(w http.ResponseWriter, r *http.Request) {
	if h.upstream == nil {
		h.redirectAdminWithError(w, r, "toggle_upstream_failed: upstream service unavailable")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.redirectAdminWithError(w, r, "invalid form data")
		return
	}
	ids := parseBatchValues(r.FormValue("id"))
	if len(ids) == 0 {
		h.redirectAdminWithError(w, r, "missing upstream id")
		return
	}
	enabled := strings.EqualFold(strings.TrimSpace(r.FormValue("enabled")), "true") || r.FormValue("enabled") == "on"
	items, err := h.upstream.ListUpstreams()
	if err != nil {
		h.redirectAdminWithError(w, r, "toggle_upstream_failed: "+err.Error())
		return
	}
	currentMap := make(map[string]upstream.Upstream, len(items))
	for i := range items {
		currentMap[items[i].ID] = items[i]
	}
	changed := 0
	lastID := ""
	for _, id := range ids {
		current, ok := currentMap[id]
		if !ok {
			h.redirectAdminWithError(w, r, "toggle_upstream_failed: upstream not found: "+id)
			return
		}
		saved, err := h.upstream.UpsertUpstream(upstream.UpsertUpstreamRequest{
			ID:      current.ID,
			Name:    current.Name,
			Type:    current.Type,
			BaseURL: current.BaseURL,
			APIKey:  current.APIKey,
			Models:  current.Models,
			Enabled: enabled,
		})
		if err != nil {
			h.redirectAdminWithError(w, r, "toggle_upstream_failed: "+id+": "+err.Error())
			return
		}
		changed++
		lastID = saved.ID
	}
	state := "disabled"
	if enabled {
		state = "enabled"
	}
	if changed == 1 {
		h.redirectAdminWithMessage(w, r, "upstream_"+state+":"+lastID)
		return
	}
	h.redirectAdminWithMessage(w, r, "upstream_"+state+":"+strconv.Itoa(changed))
}

func (h *handler) adminRouteDelete(w http.ResponseWriter, r *http.Request) {
	if h.upstream == nil {
		h.redirectAdminWithError(w, r, "delete_route_failed: upstream service unavailable")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.redirectAdminWithError(w, r, "invalid form data")
		return
	}
	aliases := parseBatchValues(r.FormValue("alias"))
	if len(aliases) == 0 {
		h.redirectAdminWithError(w, r, "missing alias")
		return
	}
	for _, alias := range aliases {
		if err := h.upstream.DeleteRoute(alias); err != nil {
			h.redirectAdminWithError(w, r, "delete_route_failed: "+alias+": "+err.Error())
			return
		}
	}
	if len(aliases) == 1 {
		h.redirectAdminWithMessage(w, r, "route_deleted:"+aliases[0])
		return
	}
	h.redirectAdminWithMessage(w, r, "route_deleted:"+strconv.Itoa(len(aliases)))
}

func (h *handler) adminBasePageView(w http.ResponseWriter, r *http.Request, active string) (adminBaseView, bool) {
	claims := SessionClaimsFromContext(r.Context())
	if claims.UserID == "" {
		http.Redirect(w, r, "/admin/login?error=invalid_session", http.StatusSeeOther)
		return adminBaseView{}, false
	}
	if h.userService == nil {
		writeLocalizedPlainError(w, r, http.StatusServiceUnavailable, "user_service_unavailable", "user service unavailable")
		return adminBaseView{}, false
	}
	me, err := h.userService.GetMe(r.Context(), claims)
	if err != nil {
		http.Redirect(w, r, "/admin/login?error=invalid_session", http.StatusSeeOther)
		return adminBaseView{}, false
	}
	return adminBaseView{
		Claims:  claims,
		Me:      me,
		Active:  active,
		Lang:    detectLanguage(r),
		Path:    r.URL.Path,
		Message: adminFlashMessage(r, strings.TrimSpace(r.URL.Query().Get("message"))),
		Error:   adminErrorMessage(r, strings.TrimSpace(r.URL.Query().Get("error"))),
	}, true
}

func withLangPath(path, lang string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if !isChineseLang(lang) {
		return path
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "lang=" + url.QueryEscape(languageZhCN)
}

func withQuery(path, key, value string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "/"
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + key + "=" + url.QueryEscape(strings.TrimSpace(value))
}

func parseRouteTargetsFromText(raw string) ([]upstream.RouteTarget, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("targets required")
	}

	chunks := strings.Split(raw, ",")
	targets := make([]upstream.RouteTarget, 0, len(chunks))
	for _, chunk := range chunks {
		part := strings.TrimSpace(chunk)
		if part == "" {
			continue
		}
		fields := strings.Split(part, ":")
		if len(fields) < 4 {
			return nil, fmt.Errorf("invalid target format: %s", part)
		}
		weight, err := strconv.Atoi(strings.TrimSpace(fields[2]))
		if err != nil || weight < 0 {
			return nil, fmt.Errorf("invalid weight in %s", part)
		}
		priority, err := strconv.Atoi(strings.TrimSpace(fields[3]))
		if err != nil || priority < 0 {
			return nil, fmt.Errorf("invalid priority in %s", part)
		}
		targets = append(targets, upstream.RouteTarget{
			UpstreamID: strings.TrimSpace(fields[0]),
			Model:      strings.TrimSpace(fields[1]),
			Weight:     weight,
			Priority:   priority,
			Enabled:    true,
		})
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no valid targets")
	}
	return targets, nil
}

func parseRouteTargetsFromForm(r *http.Request) ([]upstream.RouteTarget, error) {
	if r == nil {
		return nil, fmt.Errorf("invalid form")
	}
	targets := make([]upstream.RouteTarget, 0, 8)
	for idx := 1; idx <= 8; idx++ {
		key := strconv.Itoa(idx)
		upstreamID := strings.TrimSpace(r.FormValue("target_upstream_id_" + key))
		model := strings.TrimSpace(r.FormValue("target_model_" + key))
		weightRaw := strings.TrimSpace(r.FormValue("target_weight_" + key))
		priorityRaw := strings.TrimSpace(r.FormValue("target_priority_" + key))
		if upstreamID == "" && model == "" {
			continue
		}
		if upstreamID == "" {
			return nil, fmt.Errorf("target %d missing upstream id", idx)
		}
		weight := 100
		if weightRaw != "" {
			v, err := strconv.Atoi(weightRaw)
			if err != nil || v < 0 {
				return nil, fmt.Errorf("target %d invalid weight", idx)
			}
			weight = v
		}
		priority := idx
		if priorityRaw != "" {
			v, err := strconv.Atoi(priorityRaw)
			if err != nil || v < 0 {
				return nil, fmt.Errorf("target %d invalid priority", idx)
			}
			priority = v
		}
		targets = append(targets, upstream.RouteTarget{
			UpstreamID: upstreamID,
			Model:      model,
			Weight:     weight,
			Priority:   priority,
			Enabled:    true,
		})
	}
	if len(targets) > 0 {
		return targets, nil
	}
	return parseRouteTargetsFromText(r.FormValue("targets"))
}

func parseBatchValues(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case ',', ';', '\n', '\r', '\t', ' ':
			return true
		default:
			return false
		}
	})
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
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
